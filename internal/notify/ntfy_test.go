package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNtfyName(t *testing.T) {
	p := NewNtfy("http://localhost", "alerts")
	assert.Equal(t, "ntfy", p.Name())
}

func TestNtfySendCritical(t *testing.T) {
	var gotReq *http.Request
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "test-alerts")
	notif := model.Notification{
		AlertType: "disk_health",
		Severity:  "critical",
		Title:     "Disk Failure",
		Message:   "Disk wwn-123 SMART check failed",
		Instance:  "main",
		Timestamp: time.Now(),
	}

	err := p.Send(context.Background(), notif)
	require.NoError(t, err)

	assert.Equal(t, "/test-alerts", gotReq.URL.Path)
	assert.Equal(t, "Disk Failure", gotReq.Header.Get("Title"))
	assert.Equal(t, "5", gotReq.Header.Get("Priority"))
	assert.Contains(t, gotReq.Header.Get("Tags"), "rotating_light")
	assert.Contains(t, gotReq.Header.Get("Tags"), "disk_health")
	assert.Equal(t, "Disk wwn-123 SMART check failed", gotBody)
}

func TestNtfySendWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "3", r.Header.Get("Priority"))
		assert.Contains(t, r.Header.Get("Tags"), "warning")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "alerts")
	err := p.Send(context.Background(), model.Notification{
		Severity: "warning",
		Title:    "High CPU",
		Message:  "Node pve1 CPU at 95%",
	})
	require.NoError(t, err)
}

func TestNtfySendInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2", r.Header.Get("Priority"))
		assert.Contains(t, r.Header.Get("Tags"), "information_source")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "alerts")
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Backup complete",
		Message:  "Daily backup finished",
	})
	require.NoError(t, err)
}

func TestNtfySendResolved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Tags"), "white_check_mark")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "alerts")
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Resolved",
		Message:  "All clear",
		Resolved: true,
	})
	require.NoError(t, err)
}

func TestSeverityToNtfyPriority_UnknownSeverity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "3", r.Header.Get("Priority"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "alerts")
	err := p.Send(context.Background(), model.Notification{
		Severity: "unknown-severity",
		Title:    "Test",
		Message:  "Test unknown severity",
	})
	require.NoError(t, err)
}

func TestNtfySendServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "alerts")
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "Test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestNtfySendCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfy(srv.URL, "alerts")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Send(ctx, model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "Test cancelled",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ntfy: send:")
}

func TestNtfySendBadURL(t *testing.T) {
	p := NewNtfy("://invalid", "alerts")
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "bad url",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ntfy:")
}

func TestNtfyTrailingSlash(t *testing.T) {
	p := NewNtfy("http://example.com/", "alerts")
	assert.Equal(t, "http://example.com", p.url)
}
