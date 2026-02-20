package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "glint.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))
	return p
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GLINT_LISTEN", "GLINT_DB_PATH", "GLINT_LOG_LEVEL",
		"GLINT_PVE_URL", "GLINT_PVE_TOKEN_ID", "GLINT_PVE_TOKEN_SECRET",
		"GLINT_PBS_URL", "GLINT_PBS_TOKEN_ID", "GLINT_PBS_TOKEN_SECRET",
		"GLINT_PBS_DATASTORE", "GLINT_NTFY_URL", "GLINT_NTFY_TOPIC",
		"GLINT_HISTORY_HOURS", "GLINT_WORKER_POOL_SIZE",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

const minimalYAML = `
pve:
  - name: main
    host: "https://192.168.1.215:8006"
    token_id: "glint@pam!monitor"
    token_secret: "secret123"
    poll_interval: "15s"
    disk_poll_interval: "1h"
`

const fullYAML = `
listen: ":9090"
db_path: "/tmp/test.db"
log_level: "debug"
history_hours: 24
worker_pool_size: 8

pve:
  - name: main
    host: "https://192.168.1.215:8006"
    token_id: "glint@pam!monitor"
    token_secret: "secret123"
    insecure: true
    poll_interval: "30s"
    disk_poll_interval: "2h"
    ssh:
      host: "192.168.1.215"
      user: "root"
      key_path: "/config/ssh/id_ed25519"

pbs:
  - name: main-pbs
    host: "https://10.100.1.102:8007"
    token_id: "glint@pbs!monitor"
    token_secret: "pbssecret"
    insecure: true
    datastores: ["homelab"]
    poll_interval: "5m"

notifications:
  - type: ntfy
    url: "http://10.100.1.104:8080"
    topic: "homelab-alerts"
  - type: webhook
    url: "https://hooks.example.com/glint"
    method: "POST"
    headers:
      Authorization: "Bearer xxx"

alerts:
  node_cpu_high:
    threshold: 90
    duration: "5m"
    severity: "warning"
  guest_down:
    grace_period: "2m"
    severity: "critical"
  backup_stale:
    max_age: "36h"
    severity: "warning"
  disk_smart_failed:
    severity: "critical"
  datastore_full:
    threshold: 85
    severity: "warning"
`

func TestLoad_FromYAML(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, fullYAML)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.Listen)
	assert.Equal(t, "/tmp/test.db", cfg.DBPath)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, 24, cfg.HistoryHours)
	assert.Equal(t, 8, cfg.WorkerPoolSize)

	// PVE
	require.Len(t, cfg.PVE, 1)
	assert.Equal(t, "main", cfg.PVE[0].Name)
	assert.Equal(t, "https://192.168.1.215:8006", cfg.PVE[0].Host)
	assert.Equal(t, "glint@pam!monitor", cfg.PVE[0].TokenID)
	assert.Equal(t, "secret123", cfg.PVE[0].TokenSecret)
	assert.True(t, cfg.PVE[0].Insecure)
	assert.Equal(t, 30*time.Second, cfg.PVE[0].PollInterval.Duration)
	assert.Equal(t, 2*time.Hour, cfg.PVE[0].DiskPollInterval.Duration)
	require.NotNil(t, cfg.PVE[0].SSH)
	assert.Equal(t, "192.168.1.215", cfg.PVE[0].SSH.Host)
	assert.Equal(t, "root", cfg.PVE[0].SSH.User)
	assert.Equal(t, "/config/ssh/id_ed25519", cfg.PVE[0].SSH.KeyPath)

	// PBS
	require.Len(t, cfg.PBS, 1)
	assert.Equal(t, "main-pbs", cfg.PBS[0].Name)
	assert.Equal(t, 5*time.Minute, cfg.PBS[0].PollInterval.Duration)
	assert.Equal(t, []string{"homelab"}, cfg.PBS[0].Datastores)

	// Notifications
	require.Len(t, cfg.Notifications, 2)
	assert.Equal(t, "ntfy", cfg.Notifications[0].Type)
	assert.Equal(t, "homelab-alerts", cfg.Notifications[0].Topic)
	assert.Equal(t, "webhook", cfg.Notifications[1].Type)
	assert.Equal(t, "POST", cfg.Notifications[1].Method)
	assert.Equal(t, "Bearer xxx", cfg.Notifications[1].Headers["Authorization"])

	// Alerts
	require.NotNil(t, cfg.Alerts.NodeCPUHigh)
	assert.Equal(t, 90.0, cfg.Alerts.NodeCPUHigh.Threshold)
	assert.Equal(t, 5*time.Minute, cfg.Alerts.NodeCPUHigh.Duration.Duration)
	assert.Equal(t, "warning", cfg.Alerts.NodeCPUHigh.Severity)

	require.NotNil(t, cfg.Alerts.GuestDown)
	assert.Equal(t, 2*time.Minute, cfg.Alerts.GuestDown.GracePeriod.Duration)
	assert.Equal(t, "critical", cfg.Alerts.GuestDown.Severity)

	require.NotNil(t, cfg.Alerts.BackupStale)
	assert.Equal(t, 36*time.Hour, cfg.Alerts.BackupStale.MaxAge.Duration)

	require.NotNil(t, cfg.Alerts.DiskSmartFailed)
	assert.Equal(t, "critical", cfg.Alerts.DiskSmartFailed.Severity)

	require.NotNil(t, cfg.Alerts.DatastoreFull)
	assert.Equal(t, 85.0, cfg.Alerts.DatastoreFull.Threshold)
}

func TestLoad_FileNotFound(t *testing.T) {
	clearEnv(t)
	_, err := Load("/nonexistent/path/glint.yml")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigFileNotFound)
}

func TestLoad_EnvVarSubstitution(t *testing.T) {
	clearEnv(t)
	t.Setenv("PVE_TOKEN_ID", "glint@pam!monitor")
	t.Setenv("PVE_TOKEN_SECRET", "test-secret-uuid")

	path := writeYAML(t, `
pve:
  - name: main
    host: "https://192.168.1.1:8006"
    token_id: "${PVE_TOKEN_ID}"
    token_secret: "${PVE_TOKEN_SECRET}"
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "glint@pam!monitor", cfg.PVE[0].TokenID)
	assert.Equal(t, "test-secret-uuid", cfg.PVE[0].TokenSecret)
}

func TestLoad_EnvVarSubstitution_Unset(t *testing.T) {
	clearEnv(t)

	path := writeYAML(t, `
pve:
  - name: main
    host: "https://192.168.1.1:8006"
    token_id: "${PVE_TOKEN_ID}"
    token_secret: "${PVE_TOKEN_SECRET}"
`)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token_id is required")
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, minimalYAML)

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, ":3800", cfg.Listen)
	assert.Equal(t, "/data/glint.db", cfg.DBPath)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, 48, cfg.HistoryHours)
	assert.Equal(t, 4, cfg.WorkerPoolSize)
}

func TestLoad_FromEnvVars(t *testing.T) {
	clearEnv(t)

	t.Setenv("GLINT_LISTEN", ":4000")
	t.Setenv("GLINT_DB_PATH", "/tmp/env.db")
	t.Setenv("GLINT_LOG_LEVEL", "warn")
	t.Setenv("GLINT_PVE_URL", "https://10.0.0.1:8006")
	t.Setenv("GLINT_PVE_TOKEN_ID", "test@pam!tok")
	t.Setenv("GLINT_PVE_TOKEN_SECRET", "envsecret")
	t.Setenv("GLINT_PBS_URL", "https://10.0.0.2:8007")
	t.Setenv("GLINT_PBS_TOKEN_ID", "pbs@pbs!tok")
	t.Setenv("GLINT_PBS_TOKEN_SECRET", "pbsenvsecret")
	t.Setenv("GLINT_PBS_DATASTORE", "store1,store2")
	t.Setenv("GLINT_NTFY_URL", "http://ntfy:8080")
	t.Setenv("GLINT_NTFY_TOPIC", "test-alerts")
	t.Setenv("GLINT_HISTORY_HOURS", "72")
	t.Setenv("GLINT_WORKER_POOL_SIZE", "2")

	cfg, err := Load("")
	require.NoError(t, err)

	assert.Equal(t, ":4000", cfg.Listen)
	assert.Equal(t, "/tmp/env.db", cfg.DBPath)
	assert.Equal(t, "warn", cfg.LogLevel)
	assert.Equal(t, 72, cfg.HistoryHours)
	assert.Equal(t, 2, cfg.WorkerPoolSize)

	require.Len(t, cfg.PVE, 1)
	assert.Equal(t, "default", cfg.PVE[0].Name)
	assert.Equal(t, "https://10.0.0.1:8006", cfg.PVE[0].Host)
	assert.Equal(t, "test@pam!tok", cfg.PVE[0].TokenID)
	assert.Equal(t, "envsecret", cfg.PVE[0].TokenSecret)
	assert.Equal(t, 15*time.Second, cfg.PVE[0].PollInterval.Duration)
	assert.Equal(t, 1*time.Hour, cfg.PVE[0].DiskPollInterval.Duration)

	require.Len(t, cfg.PBS, 1)
	assert.Equal(t, "default", cfg.PBS[0].Name)
	assert.Equal(t, []string{"store1", "store2"}, cfg.PBS[0].Datastores)
	assert.Equal(t, 5*time.Minute, cfg.PBS[0].PollInterval.Duration)

	require.Len(t, cfg.Notifications, 1)
	assert.Equal(t, "ntfy", cfg.Notifications[0].Type)
	assert.Equal(t, "test-alerts", cfg.Notifications[0].Topic)
}

func TestLoad_EnvOverridesYAMLScalars(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, minimalYAML)

	t.Setenv("GLINT_LISTEN", ":5555")
	t.Setenv("GLINT_LOG_LEVEL", "debug")

	cfg, err := Load(path)
	require.NoError(t, err)

	// Env overrides scalar fields.
	assert.Equal(t, ":5555", cfg.Listen)
	assert.Equal(t, "debug", cfg.LogLevel)
	// PVE from YAML is kept (env PVE only applies when YAML has none).
	require.Len(t, cfg.PVE, 1)
	assert.Equal(t, "main", cfg.PVE[0].Name)
}

func TestLoad_NtfyDefaultTopic(t *testing.T) {
	clearEnv(t)

	t.Setenv("GLINT_PVE_URL", "https://pve:8006")
	t.Setenv("GLINT_PVE_TOKEN_ID", "a@pam!t")
	t.Setenv("GLINT_PVE_TOKEN_SECRET", "s")
	t.Setenv("GLINT_NTFY_URL", "http://ntfy:8080")
	// No GLINT_NTFY_TOPIC set -> should default to "glint-alerts".

	cfg, err := Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Notifications, 1)
	assert.Equal(t, "glint-alerts", cfg.Notifications[0].Topic)
}

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{
			name:    "no PVE instances",
			mutate:  func(c *Config) { c.PVE = nil },
			wantErr: "at least one PVE instance is required",
		},
		{
			name:    "PVE missing host",
			mutate:  func(c *Config) { c.PVE[0].Host = "" },
			wantErr: "pve[0]: host is required",
		},
		{
			name:    "PVE missing token_id",
			mutate:  func(c *Config) { c.PVE[0].TokenID = "" },
			wantErr: "pve[0]: token_id is required",
		},
		{
			name:    "PVE missing token_secret",
			mutate:  func(c *Config) { c.PVE[0].TokenSecret = "" },
			wantErr: "pve[0]: token_secret is required",
		},
		{
			name:    "PVE missing name",
			mutate:  func(c *Config) { c.PVE[0].Name = "" },
			wantErr: "pve[0]: name is required",
		},
		{
			name: "PBS missing host",
			mutate: func(c *Config) {
				c.PBS = []PBSConfig{{Name: "x", TokenID: "t", TokenSecret: "s"}}
			},
			wantErr: "pbs[0]: host is required",
		},
		{
			name: "PBS missing name",
			mutate: func(c *Config) {
				c.PBS = []PBSConfig{{Host: "https://x", TokenID: "t", TokenSecret: "s"}}
			},
			wantErr: "pbs[0]: name is required",
		},
		{
			name: "notification unknown type",
			mutate: func(c *Config) {
				c.Notifications = []NotificationConfig{{Type: "slack", URL: "http://x"}}
			},
			wantErr: "unknown type \"slack\"",
		},
		{
			name: "ntfy missing topic",
			mutate: func(c *Config) {
				c.Notifications = []NotificationConfig{{Type: "ntfy", URL: "http://x"}}
			},
			wantErr: "topic is required for ntfy",
		},
		{
			name: "webhook missing url",
			mutate: func(c *Config) {
				c.Notifications = []NotificationConfig{{Type: "webhook"}}
			},
			wantErr: "url is required for webhook",
		},
		{
			name:    "invalid log level",
			mutate:  func(c *Config) { c.LogLevel = "verbose" },
			wantErr: "log_level must be one of",
		},
		{
			name:    "invalid log format",
			mutate:  func(c *Config) { c.LogFormat = "yaml" },
			wantErr: "log_format must be one of",
		},
		{
			name:    "history_hours zero",
			mutate:  func(c *Config) { c.HistoryHours = 0 },
			wantErr: "history_hours must be >= 1",
		},
		{
			name:    "worker_pool_size zero",
			mutate:  func(c *Config) { c.WorkerPoolSize = 0 },
			wantErr: "worker_pool_size must be >= 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := validConfig()
	assert.NoError(t, cfg.Validate())
}

func TestLoad_InvalidYAML(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, "{{invalid yaml")
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config")
}

func TestLoad_InvalidDuration(t *testing.T) {
	clearEnv(t)
	path := writeYAML(t, `
pve:
  - name: x
    host: "https://x"
    token_id: "t"
    token_secret: "s"
    poll_interval: "not-a-duration"
`)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestDuration_MarshalYAML(t *testing.T) {
	d := Duration{Duration: 5 * time.Minute}
	v, err := d.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, "5m0s", v)
}

func TestDuration_MarshalYAML_SubSecond(t *testing.T) {
	d := Duration{Duration: 500 * time.Millisecond}
	v, err := d.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, "500ms", v)
}

func TestLoad_ValidationFails(t *testing.T) {
	clearEnv(t)
	// YAML file with no PVE — triggers validation error
	path := writeYAML(t, `listen: ":3800"`)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config validation")
}

func TestLoad_EmptyFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("GLINT_PVE_URL", "https://pve:8006")
	t.Setenv("GLINT_PVE_TOKEN_ID", "a@pam!t")
	t.Setenv("GLINT_PVE_TOKEN_SECRET", "s")

	path := writeYAML(t, "")
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.PVE, 1)
}

func TestLoad_PBSValidation_MissingTokenID(t *testing.T) {
	cfg := validConfig()
	cfg.PBS = []PBSConfig{{Name: "x", Host: "https://x", TokenSecret: "s"}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pbs[0]: token_id is required")
}

func TestLoad_PBSValidation_MissingTokenSecret(t *testing.T) {
	cfg := validConfig()
	cfg.PBS = []PBSConfig{{Name: "x", Host: "https://x", TokenID: "t"}}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pbs[0]: token_secret is required")
}

func FuzzExpandEnvVars(f *testing.F) {
	f.Add([]byte(`listen: ":3800"`))
	f.Add([]byte(`token_secret: "${MY_SECRET}"`))
	f.Add([]byte(`${} ${VAR} $VAR`))
	f.Add([]byte(`pve_secret: "${A}${B}"`))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic
		_ = expandEnvVars(data)
	})
}

// validConfig returns a minimal valid Config for mutation in tests.
func validConfig() *Config {
	return &Config{
		Listen:         ":3800",
		DBPath:         "/data/glint.db",
		LogLevel:       "info",
		LogFormat:      "text",
		HistoryHours:   48,
		WorkerPoolSize: 4,
		PVE: []PVEConfig{
			{
				Name:        "main",
				Host:        "https://192.168.1.215:8006",
				TokenID:     "glint@pam!monitor",
				TokenSecret: "secret",
			},
		},
	}
}
