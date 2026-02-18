package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookName(t *testing.T) {
	p := NewWebhook("http://localhost/hook", "", nil)
	assert.Equal(t, "webhook", p.Name())
}

func TestWebhookSendJSON(t *testing.T) {
	var gotBody model.Notification
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewWebhook(srv.URL+"/hook", "", nil)
	notif := model.Notification{
		AlertType: "node_down",
		Severity:  "critical",
		Title:     "Node Offline",
		Message:   "pve1 is unreachable",
		Instance:  "main",
		Subject:   "pve1",
		Timestamp: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		Metadata:  map[string]string{"region": "home"},
	}

	err := p.Send(context.Background(), notif)
	require.NoError(t, err)

	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "node_down", gotBody.AlertType)
	assert.Equal(t, "critical", gotBody.Severity)
	assert.Equal(t, "Node Offline", gotBody.Title)
	assert.Equal(t, "pve1 is unreachable", gotBody.Message)
	assert.Equal(t, "home", gotBody.Metadata["region"])
}

func TestWebhookCustomHeaders(t *testing.T) {
	var gotAuth string
	var gotCustom string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	headers := map[string]string{
		"Authorization": "Bearer tok123",
		"X-Custom":      "my-value",
	}
	p := NewWebhook(srv.URL, "", headers)
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "test",
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer tok123", gotAuth)
	assert.Equal(t, "my-value", gotCustom)
}

func TestWebhookMethodOverride(t *testing.T) {
	var gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewWebhook(srv.URL, http.MethodPut, nil)
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "test",
	})
	require.NoError(t, err)
	assert.Equal(t, "PUT", gotMethod)
}

func TestWebhookDefaultMethodIsPost(t *testing.T) {
	var gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewWebhook(srv.URL, "", nil)
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "test",
	})
	require.NoError(t, err)
	assert.Equal(t, "POST", gotMethod)
}

func TestWebhookServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	p := NewWebhook(srv.URL, "", nil)
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestWebhookSendCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewWebhook(srv.URL, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Send(ctx, model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "cancelled",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook: send:")
}

func TestWebhookSendBadURL(t *testing.T) {
	p := NewWebhook("://invalid", "", nil)
	err := p.Send(context.Background(), model.Notification{
		Severity: "info",
		Title:    "Test",
		Message:  "bad url",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook:")
}
