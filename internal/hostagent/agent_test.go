package hostagent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/agentauth"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "0123456789abcdef0123456789abcdef"

func testAgent(t *testing.T, serverURL string) *Agent {
	t.Helper()
	root, at := fakeHost(t)
	a := New(Options{
		ServerURL: serverURL,
		Name:      "oci-edge",
		Secret:    secret,
		Timeout:   5 * time.Second,
		Retries:   2,
		Backoff:   time.Millisecond,
	}, testCollector(root, at, aptUbuntu, nil))
	return a
}

func TestAgent_SendsVerifiableReport(t *testing.T) {
	v := agentauth.NewVerifier(map[string]string{"oci-edge": secret})
	var got model.HostReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, ReportPath, r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		agent, err := v.Verify(r, body)
		assert.NoError(t, err)
		assert.Equal(t, "oci-edge", agent)
		assert.NoError(t, json.Unmarshal(body, &got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	require.NoError(t, testAgent(t, srv.URL+"/").Run(context.Background()))
	assert.Equal(t, 2, got.Updates.Security)
}

func TestAgent_RetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	require.NoError(t, testAgent(t, srv.URL).Run(context.Background()))
	assert.Equal(t, int32(3), calls.Load())
}

func TestAgent_GivesUpAfterRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	err := testAgent(t, srv.URL).Run(context.Background())
	assert.ErrorContains(t, err, "HTTP 429")
	assert.Equal(t, int32(3), calls.Load(), "one attempt plus two retries")
}

func TestAgent_DoesNotRetryRejections(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := testAgent(t, srv.URL).Run(context.Background())
	assert.ErrorContains(t, err, "server rejected report: HTTP 401")
	assert.Equal(t, int32(1), calls.Load())
}

func TestAgent_ConnectionErrorAndCancelledRetry(t *testing.T) {
	a := testAgent(t, "http://127.0.0.1:1")
	a.opts.Backoff = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, a.Run(ctx), context.DeadlineExceeded)
}

func TestAgent_BadURL(t *testing.T) {
	a := testAgent(t, "http://bad host")
	a.opts.Retries = 0
	assert.ErrorContains(t, a.Run(context.Background()), "building request")
}

func TestAgent_DryRunPrintsJSON(t *testing.T) {
	a := testAgent(t, "")
	a.opts.DryRun = true
	var out bytes.Buffer
	a.out = &out

	require.NoError(t, a.Run(context.Background()))
	var r model.HostReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &r))
	assert.Equal(t, "oci-edge", r.Agent)
}

func TestAgent_IntervalLoopsUntilCancelled(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError) // failures are logged, not fatal
	}))
	defer srv.Close()

	a := testAgent(t, srv.URL)
	a.opts.Retries = 0
	a.opts.Interval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	assert.NoError(t, a.Run(ctx))
	assert.GreaterOrEqual(t, calls.Load(), int32(2))
}

func secretFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestMain_Validation(t *testing.T) {
	good := secretFile(t, secret+"\n")
	short := secretFile(t, "short")

	tests := []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"bad flag", []string{"--nope"}, 2, "flag provided but not defined"},
		{"no secret", []string{"--server", "https://g", "--secret-file", ""}, 2, "no secret"},
		{"unreadable secret", []string{"--server", "https://g", "--secret-file", "/nonexistent"}, 2, "reading secret"},
		{"no server", []string{"--secret-file", good}, 2, "--server is required"},
		{"bad scheme", []string{"--server", "ftp://g", "--secret-file", good}, 2, "must be an http(s) URL"},
		{"no name", []string{"--server", "https://g", "--name", "", "--secret-file", good}, 2, "--name is required"},
		{"short secret", []string{"--server", "https://g", "--secret-file", short}, 2, "at least 32"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := Main(context.Background(), tt.args, "test", &stderr)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, stderr.String(), tt.msg)
		})
	}
}

func TestMain_SendsAndUsesCredentialsDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "glint-agent-secret"), []byte(secret), 0o600))
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	t.Setenv("GLINT_AGENT_SERVER", srv.URL)
	t.Setenv("GLINT_AGENT_NAME", "oci-edge")

	var stderr bytes.Buffer
	code := Main(context.Background(), nil, "test", &stderr)
	assert.Equal(t, 1, code, "secret was found, the 401 is the failure")
	assert.Contains(t, stderr.String(), "HTTP 401")
}

func TestMain_DryRunNeedsNoSecret(t *testing.T) {
	var stderr bytes.Buffer
	assert.Equal(t, 0, Main(context.Background(), []string{"--dry-run"}, "test", &stderr), stderr.String())
}

func TestCredentialPath(t *testing.T) {
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	assert.Empty(t, credentialPath())
	t.Setenv("CREDENTIALS_DIRECTORY", "/run/credentials/x")
	assert.Equal(t, "/run/credentials/x/glint-agent-secret", credentialPath())
}
