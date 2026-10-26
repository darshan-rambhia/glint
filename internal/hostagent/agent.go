package hostagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/agentauth"
)

// ReportPath is the server endpoint agents post to.
const ReportPath = "/api/v1/agents/report"

// Options configures a single agent process.
type Options struct {
	ServerURL string        // Glint base URL, e.g. https://glint.example.com
	Name      string        // agent name, must match the server config
	Secret    string        // shared HMAC secret
	Interval  time.Duration // 0 = report once and exit
	Timeout   time.Duration // per-request timeout
	DryRun    bool          // print the report instead of sending it
	Retries   int           // extra attempts after a failed send
	Backoff   time.Duration // wait before the first retry, doubled per retry
}

// Agent collects and ships reports.
type Agent struct {
	opts      Options
	collector *Collector
	client    *http.Client
	out       io.Writer
}

// New returns an agent for opts using the given collector.
func New(opts Options, c *Collector) *Agent {
	return &Agent{
		opts:      opts,
		collector: c,
		client:    &http.Client{Timeout: opts.Timeout},
		out:       os.Stdout,
	}
}

// Run reports once, or every Interval until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	if a.opts.Interval <= 0 {
		return a.reportOnce(ctx)
	}
	ticker := time.NewTicker(a.opts.Interval)
	defer ticker.Stop()
	for {
		if err := a.reportOnce(ctx); err != nil {
			slog.Error("agent report failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (a *Agent) reportOnce(ctx context.Context) error {
	report := a.collector.Collect(ctx)
	for _, e := range report.Errors {
		slog.Warn("collection step failed", "detail", e)
	}
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encoding report: %w", err)
	}

	if a.opts.DryRun {
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, body, "", "  ")
		pretty.WriteByte('\n')
		_, err := a.out.Write(pretty.Bytes())
		return err
	}

	backoff := a.opts.Backoff
	for attempt := 0; ; attempt++ {
		err = a.send(ctx, body)
		if err == nil {
			slog.Info("report sent",
				"pending", report.Updates.Pending,
				"security", report.Updates.Security,
				"reboot_required", report.Reboot.Required)
			return nil
		}
		var perm *permanentError
		if errors.As(err, &perm) || attempt >= a.opts.Retries {
			return err
		}
		slog.Warn("send failed, retrying", "attempt", attempt+1, "in", backoff, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// permanentError marks a rejection that retrying will not fix.
type permanentError struct{ status int }

func (e *permanentError) Error() string {
	return fmt.Sprintf("server rejected report: HTTP %d", e.status)
}

func (a *Agent) send(ctx context.Context, body []byte) error {
	u := strings.TrimRight(a.opts.ServerURL, "/") + ReportPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := agentauth.Sign(req, a.opts.Name, a.opts.Secret, body, time.Now()); err != nil {
		return err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("posting report: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	switch {
	case resp.StatusCode/100 == 2:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests:
		return &permanentError{status: resp.StatusCode}
	default:
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
}

// Main is the `glint agent` entry point. It returns a process exit code.
func Main(ctx context.Context, args []string, version string, stderr io.Writer) int {
	fs := flag.NewFlagSet("glint agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	hostname, _ := os.Hostname()

	opts := Options{Timeout: 30 * time.Second, Retries: 2, Backoff: 10 * time.Second}
	fs.StringVar(&opts.ServerURL, "server", os.Getenv("GLINT_AGENT_SERVER"), "Glint base URL (env GLINT_AGENT_SERVER)")
	fs.StringVar(&opts.Name, "name", envOr("GLINT_AGENT_NAME", hostname), "agent name, as configured on the server (env GLINT_AGENT_NAME)")
	secretFile := fs.String("secret-file", envOr("GLINT_AGENT_SECRET_FILE", credentialPath()),
		"file holding the shared secret (env GLINT_AGENT_SECRET_FILE; default $CREDENTIALS_DIRECTORY/glint-agent-secret)")
	fs.DurationVar(&opts.Interval, "interval", 0, "report repeatedly at this interval; 0 reports once and exits")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "HTTP request timeout")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print the report as JSON instead of sending it")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !opts.DryRun {
		if err := opts.loadSecret(*secretFile); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
			return 2
		}
		if err := opts.validate(); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
			return 2
		}
	}

	if err := New(opts, NewCollector(opts.Name, version)).Run(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		return 1
	}
	return 0
}

func (o *Options) loadSecret(path string) error {
	if path == "" {
		return errors.New("no secret: set --secret-file, GLINT_AGENT_SECRET_FILE or a systemd credential")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading secret: %w", err)
	}
	o.Secret = strings.TrimSpace(string(b))
	return nil
}

func (o *Options) validate() error {
	if o.ServerURL == "" {
		return errors.New("--server is required")
	}
	u, err := url.Parse(o.ServerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("--server must be an http(s) URL, got %q", o.ServerURL)
	}
	if o.Name == "" {
		return errors.New("--name is required")
	}
	if len(o.Secret) < agentauth.MinSecretLen {
		return fmt.Errorf("secret must be at least %d characters", agentauth.MinSecretLen)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// credentialPath is where systemd LoadCredential= puts the secret.
func credentialPath() string {
	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		return filepath.Join(dir, "glint-agent-secret")
	}
	return ""
}
