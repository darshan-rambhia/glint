// Package config handles loading and validating Glint configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} placeholders in config values.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ErrConfigFileNotFound is returned by Load when the specified config file does not exist.
var ErrConfigFileNotFound = errors.New("config file not found")

// Config is the top-level Glint configuration.
type Config struct {
	Listen         string               `yaml:"listen"`
	DBPath         string               `yaml:"db_path"`
	LogLevel       string               `yaml:"log_level"`
	LogFormat      string               `yaml:"log_format"`
	HistoryHours   int                  `yaml:"history_hours"`
	WorkerPoolSize int                  `yaml:"worker_pool_size"`
	PVE            []PVEConfig          `yaml:"pve"`
	PBS            []PBSConfig          `yaml:"pbs"`
	Notifications  []NotificationConfig `yaml:"notifications"`
	Alerts         AlertsConfig         `yaml:"alerts"`
}

// PVEConfig describes a single Proxmox VE instance.
type PVEConfig struct {
	Name             string     `yaml:"name"`
	Host             string     `yaml:"host"`
	TokenID          string     `yaml:"token_id"`
	TokenSecret      string     `yaml:"token_secret"`
	Insecure         bool       `yaml:"insecure"`
	PollInterval     Duration   `yaml:"poll_interval"`
	DiskPollInterval Duration   `yaml:"disk_poll_interval"`
	SSH              *SSHConfig `yaml:"ssh,omitempty"`
}

// SSHConfig describes SSH access to a PVE node.
type SSHConfig struct {
	Host    string `yaml:"host"`
	User    string `yaml:"user"`
	KeyPath string `yaml:"key_path"`
}

// PBSConfig describes a single Proxmox Backup Server instance.
type PBSConfig struct {
	Name         string   `yaml:"name"`
	Host         string   `yaml:"host"`
	TokenID      string   `yaml:"token_id"`
	TokenSecret  string   `yaml:"token_secret"`
	Insecure     bool     `yaml:"insecure"`
	Datastores   []string `yaml:"datastores"`
	PollInterval Duration `yaml:"poll_interval"`
}

// NotificationConfig describes a notification target.
type NotificationConfig struct {
	Type    string            `yaml:"type"` // "ntfy" or "webhook"
	URL     string            `yaml:"url"`
	Topic   string            `yaml:"topic,omitempty"`   // ntfy only
	Method  string            `yaml:"method,omitempty"`  // webhook only
	Headers map[string]string `yaml:"headers,omitempty"` // webhook only
}

// AlertsConfig holds thresholds for each alert type.
type AlertsConfig struct {
	NodeCPUHigh     *AlertNodeCPUHigh     `yaml:"node_cpu_high,omitempty"`
	GuestDown       *AlertGuestDown       `yaml:"guest_down,omitempty"`
	BackupStale     *AlertBackupStale     `yaml:"backup_stale,omitempty"`
	DiskSmartFailed *AlertDiskSmartFailed `yaml:"disk_smart_failed,omitempty"`
	DatastoreFull   *AlertDatastoreFull   `yaml:"datastore_full,omitempty"`
}

type AlertNodeCPUHigh struct {
	Threshold float64  `yaml:"threshold"`
	Duration  Duration `yaml:"duration"`
	Severity  string   `yaml:"severity"`
}

type AlertGuestDown struct {
	GracePeriod Duration `yaml:"grace_period"`
	Severity    string   `yaml:"severity"`
}

type AlertBackupStale struct {
	MaxAge   Duration `yaml:"max_age"`
	Severity string   `yaml:"severity"`
}

type AlertDiskSmartFailed struct {
	Severity string `yaml:"severity"`
}

type AlertDatastoreFull struct {
	Threshold float64 `yaml:"threshold"`
	Severity  string  `yaml:"severity"`
}

// Duration wraps time.Duration with YAML string parsing support.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.String(), nil
}

// Load reads configuration from a YAML file. If no path is given, it falls
// back to environment variables for single-instance setup. If a path is given
// and the file does not exist, ErrConfigFileNotFound is returned.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrConfigFileNotFound, path)
		}
		if err != nil {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		if len(data) > 0 {
			if err := yaml.Unmarshal(expandEnvVars(data), cfg); err != nil {
				return nil, fmt.Errorf("parsing config: %w", err)
			}
		}
	}

	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
	return cfg, nil
}

// Validate checks that the configuration is usable.
func (c *Config) Validate() error {
	if len(c.PVE) == 0 {
		return fmt.Errorf("at least one PVE instance is required")
	}
	for i, pve := range c.PVE {
		if pve.Host == "" {
			return fmt.Errorf("pve[%d]: host is required", i)
		}
		if _, err := url.Parse(pve.Host); err != nil {
			return fmt.Errorf("pve[%d]: invalid host URL: %w", i, err)
		}
		if pve.TokenID == "" {
			return fmt.Errorf("pve[%d]: token_id is required", i)
		}
		if pve.TokenSecret == "" {
			return fmt.Errorf("pve[%d]: token_secret is required", i)
		}
		if pve.Name == "" {
			return fmt.Errorf("pve[%d]: name is required", i)
		}
	}
	for i, pbs := range c.PBS {
		if pbs.Host == "" {
			return fmt.Errorf("pbs[%d]: host is required", i)
		}
		if pbs.TokenID == "" {
			return fmt.Errorf("pbs[%d]: token_id is required", i)
		}
		if pbs.TokenSecret == "" {
			return fmt.Errorf("pbs[%d]: token_secret is required", i)
		}
		if pbs.Name == "" {
			return fmt.Errorf("pbs[%d]: name is required", i)
		}
	}
	for i, n := range c.Notifications {
		switch n.Type {
		case "ntfy":
			if n.URL == "" {
				return fmt.Errorf("notifications[%d]: url is required for ntfy", i)
			}
			if n.Topic == "" {
				return fmt.Errorf("notifications[%d]: topic is required for ntfy", i)
			}
		case "webhook":
			if n.URL == "" {
				return fmt.Errorf("notifications[%d]: url is required for webhook", i)
			}
		default:
			return fmt.Errorf("notifications[%d]: unknown type %q (expected ntfy or webhook)", i, n.Type)
		}
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.LogLevel] {
		return fmt.Errorf("log_level must be one of: debug, info, warn, error")
	}
	validFormats := map[string]bool{"text": true, "json": true}
	if !validFormats[c.LogFormat] {
		return fmt.Errorf("log_format must be one of: text, json")
	}
	if c.HistoryHours < 1 {
		return fmt.Errorf("history_hours must be >= 1")
	}
	if c.WorkerPoolSize < 1 {
		return fmt.Errorf("worker_pool_size must be >= 1")
	}

	// Validate alert thresholds
	if a := c.Alerts.NodeCPUHigh; a != nil {
		if a.Threshold <= 0 {
			return fmt.Errorf("alerts.node_cpu_high: threshold must be > 0")
		}
		if a.Duration.Duration <= 0 {
			return fmt.Errorf("alerts.node_cpu_high: duration must be > 0")
		}
	}
	if a := c.Alerts.GuestDown; a != nil {
		if a.GracePeriod.Duration <= 0 {
			return fmt.Errorf("alerts.guest_down: grace_period must be > 0")
		}
	}
	if a := c.Alerts.BackupStale; a != nil {
		if a.MaxAge.Duration <= 0 {
			return fmt.Errorf("alerts.backup_stale: max_age must be > 0")
		}
	}
	if a := c.Alerts.DatastoreFull; a != nil {
		if a.Threshold <= 0 {
			return fmt.Errorf("alerts.datastore_full: threshold must be > 0")
		}
	}

	return nil
}

func defaults() *Config {
	return &Config{
		Listen:         ":3800",
		DBPath:         "/data/glint.db",
		LogLevel:       "info",
		LogFormat:      "text",
		HistoryHours:   48,
		WorkerPoolSize: 4,
	}
}

// expandEnvVars replaces ${VAR_NAME} placeholders in raw YAML with the
// corresponding environment variable values. Unset variables are replaced
// with an empty string, which will then fail validation with a clear error.
func expandEnvVars(data []byte) []byte {
	return envVarPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		key := string(match[2 : len(match)-1]) // strip ${ and }
		return []byte(os.Getenv(key))
	})
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("GLINT_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("GLINT_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("GLINT_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("GLINT_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}

	// Single-instance PVE from env vars (only if no YAML PVE configured).
	if len(cfg.PVE) == 0 {
		if host := os.Getenv("GLINT_PVE_URL"); host != "" {
			insecurePVE := os.Getenv("GLINT_PVE_INSECURE") == "true" || os.Getenv("GLINT_PVE_INSECURE") == "1"
			pve := PVEConfig{
				Name:             "default",
				Host:             host,
				TokenID:          os.Getenv("GLINT_PVE_TOKEN_ID"),
				TokenSecret:      os.Getenv("GLINT_PVE_TOKEN_SECRET"),
				Insecure:         insecurePVE,
				PollInterval:     Duration{15 * time.Second},
				DiskPollInterval: Duration{1 * time.Hour},
			}
			cfg.PVE = append(cfg.PVE, pve)
		}
	}

	// Single-instance PBS from env vars (only if no YAML PBS configured).
	if len(cfg.PBS) == 0 {
		if host := os.Getenv("GLINT_PBS_URL"); host != "" {
			ds := os.Getenv("GLINT_PBS_DATASTORE")
			var datastores []string
			if ds != "" {
				datastores = strings.Split(ds, ",")
			}
			insecurePBS := os.Getenv("GLINT_PBS_INSECURE") == "true" || os.Getenv("GLINT_PBS_INSECURE") == "1"
			pbs := PBSConfig{
				Name:         "default",
				Host:         host,
				TokenID:      os.Getenv("GLINT_PBS_TOKEN_ID"),
				TokenSecret:  os.Getenv("GLINT_PBS_TOKEN_SECRET"),
				Insecure:     insecurePBS,
				Datastores:   datastores,
				PollInterval: Duration{5 * time.Minute},
			}
			cfg.PBS = append(cfg.PBS, pbs)
		}
	}

	// Single ntfy target from env vars (only if no YAML notifications configured).
	if len(cfg.Notifications) == 0 {
		if ntfyURL := os.Getenv("GLINT_NTFY_URL"); ntfyURL != "" {
			topic := os.Getenv("GLINT_NTFY_TOPIC")
			if topic == "" {
				topic = "glint-alerts"
			}
			cfg.Notifications = append(cfg.Notifications, NotificationConfig{
				Type:  "ntfy",
				URL:   ntfyURL,
				Topic: topic,
			})
		}
	}

	// History hours and worker pool size from env.
	if v := os.Getenv("GLINT_HISTORY_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.HistoryHours = n
		}
	}
	if v := os.Getenv("GLINT_WORKER_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WorkerPoolSize = n
		}
	}
}
