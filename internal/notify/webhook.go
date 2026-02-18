package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
)

// WebhookProvider sends notifications as JSON to an HTTP endpoint.
type WebhookProvider struct {
	url     string
	method  string
	headers map[string]string
	client  *http.Client
}

// NewWebhook creates a new webhook notification provider.
func NewWebhook(url, method string, headers map[string]string) *WebhookProvider {
	if method == "" {
		method = http.MethodPost
	}
	return &WebhookProvider{
		url:     url,
		method:  method,
		headers: headers,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *WebhookProvider) Name() string { return "webhook" }

func (w *WebhookProvider) Send(ctx context.Context, n model.Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, w.method, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d", resp.StatusCode)
	}
	return nil
}
