package agentauth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func signed(t *testing.T, body string, now time.Time) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/report", nil)
	require.NoError(t, Sign(req, "edge", testSecret, []byte(body), now))
	return req
}

func newVerifier(now time.Time) *Verifier {
	v := NewVerifier(map[string]string{"edge": testSecret})
	v.now = func() time.Time { return now }
	return v
}

func TestVerify_OK(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	agent, err := newVerifier(now).Verify(signed(t, `{"a":1}`, now), []byte(`{"a":1}`))
	require.NoError(t, err)
	assert.Equal(t, "edge", agent)
}

func TestVerify_Failures(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"a":1}`)

	tests := []struct {
		name   string
		mutate func(*http.Request) []byte
		want   error
	}{
		{"missing headers", func(r *http.Request) []byte { r.Header.Del(HeaderSignature); return body }, ErrMissingHeaders},
		{"oversized nonce", func(r *http.Request) []byte {
			r.Header.Set(HeaderNonce, string(make([]byte, 65)))
			return body
		}, ErrMissingHeaders},
		{"unknown agent", func(r *http.Request) []byte { r.Header.Set(HeaderAgent, "other"); return body }, ErrUnknownAgent},
		{"non-numeric timestamp", func(r *http.Request) []byte { r.Header.Set(HeaderTimestamp, "soon"); return body }, ErrBadTimestamp},
		{"too old", func(r *http.Request) []byte {
			r.Header.Set(HeaderTimestamp, strconv.FormatInt(now.Add(-MaxSkew-time.Second).Unix(), 10))
			return body
		}, ErrBadTimestamp},
		{"from the future", func(r *http.Request) []byte {
			r.Header.Set(HeaderTimestamp, strconv.FormatInt(now.Add(MaxSkew+time.Second).Unix(), 10))
			return body
		}, ErrBadTimestamp},
		{"tampered body", func(*http.Request) []byte { return []byte(`{"a":2}`) }, ErrBadSignature},
		{"other path", func(r *http.Request) []byte { r.URL.Path = "/api/v1/other"; return body }, ErrBadSignature},
		{"other method", func(r *http.Request) []byte { r.Method = http.MethodPut; return body }, ErrBadSignature},
		{"non-hex signature", func(r *http.Request) []byte { r.Header.Set(HeaderSignature, "zz"); return body }, ErrBadSignature},
		{"wrong secret", func(r *http.Request) []byte {
			require.NoError(t, Sign(r, "edge", "ffffffffffffffffffffffffffffffff", body, now))
			return body
		}, ErrBadSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := signed(t, string(body), now)
			b := tt.mutate(req)
			_, err := newVerifier(now).Verify(req, b)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestVerify_Replay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	v := newVerifier(now)
	req := signed(t, "x", now)

	_, err := v.Verify(req, []byte("x"))
	require.NoError(t, err)
	_, err = v.Verify(req, []byte("x"))
	assert.ErrorIs(t, err, ErrReplay)
}

func TestVerify_BadSignatureDoesNotConsumeNonce(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	v := newVerifier(now)
	req := signed(t, "x", now)

	_, err := v.Verify(req, []byte("tampered"))
	require.ErrorIs(t, err, ErrBadSignature)
	_, err = v.Verify(req, []byte("x"))
	assert.NoError(t, err, "the genuine request must still be accepted")
}

func TestVerify_ExpiredNoncesArePruned(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	v := newVerifier(now)
	_, err := v.Verify(signed(t, "x", now), []byte("x"))
	require.NoError(t, err)
	require.Len(t, v.nonces, 1)

	later := now.Add(3 * MaxSkew)
	v.now = func() time.Time { return later }
	_, err = v.Verify(signed(t, "y", later), []byte("y"))
	require.NoError(t, err)
	assert.Len(t, v.nonces, 1, "the first nonce should have been pruned")
}
