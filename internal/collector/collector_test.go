package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// WorkerPool
// ---------------------------------------------------------------------------

func TestNewWorkerPool(t *testing.T) {
	pool := NewWorkerPool(4)
	require.NotNil(t, pool)
	assert.Equal(t, 4, cap(pool.sem))
}

func TestWorkerPool_Submit_ExecutesFunction(t *testing.T) {
	pool := NewWorkerPool(2)
	var called atomic.Bool

	done := make(chan struct{})
	err := pool.Submit(context.Background(), func() {
		called.Store(true)
		close(done)
	})
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("function was not executed in time")
	}
	assert.True(t, called.Load())
}

func TestWorkerPool_Submit_BlocksWhenFull(t *testing.T) {
	pool := NewWorkerPool(1)

	// Fill the single slot with a blocking function
	blocker := make(chan struct{})
	err := pool.Submit(context.Background(), func() {
		<-blocker
	})
	require.NoError(t, err)

	// Second submit should block until the first finishes or context cancels
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = pool.Submit(ctx, func() {})
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	close(blocker)
}

func TestWorkerPool_Submit_ErrorOnCancelledContext(t *testing.T) {
	pool := NewWorkerPool(1)

	// Fill the slot
	blocker := make(chan struct{})
	err := pool.Submit(context.Background(), func() {
		<-blocker
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = pool.Submit(ctx, func() {})
	assert.ErrorIs(t, err, context.Canceled)

	close(blocker)
}

// ---------------------------------------------------------------------------
// mockCollector for Run tests
// ---------------------------------------------------------------------------

type mockCollector struct {
	name     string
	interval time.Duration
	calls    atomic.Int32
	err      error
}

func (m *mockCollector) Name() string            { return m.name }
func (m *mockCollector) Interval() time.Duration { return m.interval }
func (m *mockCollector) Collect(_ context.Context) error {
	m.calls.Add(1)
	return m.err
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

func TestRun_ImmediateCollectThenInterval(t *testing.T) {
	mc := &mockCollector{
		name:     "test-collector",
		interval: 50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()

	err := Run(ctx, mc)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Should have collected immediately + at least 2 ticks (~50ms, ~100ms)
	got := int(mc.calls.Load())
	assert.GreaterOrEqual(t, got, 3, "expected at least 3 collections (immediate + 2 ticks), got %d", got)
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	mc := &mockCollector{
		name:     "cancel-collector",
		interval: 1 * time.Hour, // won't fire
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, mc)
	}()

	// Wait for the immediate collect
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}

	assert.Equal(t, int32(1), mc.calls.Load(), "should have collected exactly once (immediate)")
}

func TestRun_ContinuesOnCollectError(t *testing.T) {
	mc := &mockCollector{
		name:     "error-collector",
		interval: 30 * time.Millisecond,
		err:      errors.New("collection failed"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := Run(ctx, mc)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Despite errors, it should keep collecting
	assert.GreaterOrEqual(t, int(mc.calls.Load()), 2)
}

// ---------------------------------------------------------------------------
// RetryableError
// ---------------------------------------------------------------------------

func TestRetryableError_Error(t *testing.T) {
	inner := errors.New("timeout")
	re := NewRetryableError(inner)
	assert.Equal(t, "timeout", re.Error())
}

func TestRetryableError_Unwrap(t *testing.T) {
	inner := errors.New("timeout")
	re := NewRetryableError(inner)
	assert.ErrorIs(t, re, inner)
}

func TestRetryableError_IsRetryable(t *testing.T) {
	re := NewRetryableError(errors.New("x"))
	assert.True(t, re.IsRetryable())
}

// ---------------------------------------------------------------------------
// APIError
// ---------------------------------------------------------------------------

func TestAPIError_Error(t *testing.T) {
	ae := &APIError{StatusCode: 500, Body: "internal error", Endpoint: "/api2/json/nodes"}
	assert.Contains(t, ae.Error(), "500")
	assert.Contains(t, ae.Error(), "/api2/json/nodes")
	assert.Contains(t, ae.Error(), "internal error")
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"500 is retryable", 500, true},
		{"502 is retryable", 502, true},
		{"503 is retryable", 503, true},
		{"429 is retryable", 429, true},
		{"404 not retryable", 404, false},
		{"401 not retryable", 401, false},
		{"403 not retryable", 403, false},
		{"400 not retryable", 400, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ae := &APIError{StatusCode: tt.statusCode, Body: "test", Endpoint: "/test"}
			assert.Equal(t, tt.retryable, ae.IsRetryable())
		})
	}
}
