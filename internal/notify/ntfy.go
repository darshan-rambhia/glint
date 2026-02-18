package notify

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
)

// NtfyProvider sends notifications via an ntfy server.
type NtfyProvider struct {
	url    string
	topic  string
	client *http.Client
}

// NewNtfy creates a new ntfy notification provider.
func NewNtfy(url, topic string) *NtfyProvider {
	return &NtfyProvider{
		url:    strings.TrimRight(url, "/"),
		topic:  topic,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *NtfyProvider) Name() string { return "ntfy" }

func (n *NtfyProvider) Send(ctx context.Context, notif model.Notification) error {
	endpoint := fmt.Sprintf("%s/%s", n.url, n.topic)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(notif.Message))
	if err != nil {
		return fmt.Errorf("ntfy: build request: %w", err)
	}

	req.Header.Set("Title", notif.Title)
	req.Header.Set("Priority", severityToNtfyPriority(notif.Severity))
	req.Header.Set("Tags", ntfyTags(notif))

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func severityToNtfyPriority(severity string) string {
	switch severity {
	case "critical":
		return "5"
	case "warning":
		return "3"
	case "info":
		return "2"
	default:
		return "3"
	}
}

func ntfyTags(n model.Notification) string {
	var tags []string
	switch n.Severity {
	case "critical":
		tags = append(tags, "rotating_light")
	case "warning":
		tags = append(tags, "warning")
	case "info":
		tags = append(tags, "information_source")
	}
	if n.AlertType != "" {
		tags = append(tags, n.AlertType)
	}
	if n.Resolved {
		tags = append(tags, "white_check_mark")
	}
	return strings.Join(tags, ",")
}
