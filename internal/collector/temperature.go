package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConfig holds SSH connection settings for temperature polling.
type SSHConfig struct {
	Host           string
	User           string
	KeyPath        string
	KnownHostsFile string // path to known_hosts file; empty = insecure (warn at startup)
}

// TempCollector polls CPU temperatures via SSH.
type TempCollector struct {
	instance string
	node     string
	sshCfg   SSHConfig
	cache    *cache.Cache
	interval time.Duration
	signer   ssh.Signer // cached at startup
}

// NewTempCollector creates a temperature collector for a specific node.
// The SSH key is parsed once at startup rather than on every poll.
func NewTempCollector(instance, node string, cfg SSHConfig, c *cache.Cache) (*TempCollector, error) {
	keyBytes, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading SSH key %s: %w", cfg.KeyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key %s: %w", cfg.KeyPath, err)
	}

	return &TempCollector{
		instance: instance,
		node:     node,
		sshCfg:   cfg,
		cache:    c,
		interval: 60 * time.Second,
		signer:   signer,
	}, nil
}

func (t *TempCollector) Name() string            { return fmt.Sprintf("temp:%s/%s", t.instance, t.node) }
func (t *TempCollector) Interval() time.Duration { return t.interval }

// Collect polls `sensors -j` via SSH and updates the cache.
func (t *TempCollector) Collect(ctx context.Context) error {
	temp, err := t.pollTemperature(ctx)
	if err != nil {
		slog.Debug("temperature poll failed (graceful fallback)", "instance", t.instance, "node", t.node, "error", err)
		return nil // graceful degradation — don't return error
	}

	// Update the node's temperature in cache
	t.cache.UpdateNodeTemperature(t.instance, t.node, temp)

	return nil
}

func (t *TempCollector) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if t.sshCfg.KnownHostsFile == "" {
		slog.Warn("SSH host key verification disabled — set known_hosts_file to enable it",
			"instance", t.instance, "host", t.sshCfg.Host)
		return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // user opted out; warned above
	}
	cb, err := knownhosts.New(t.sshCfg.KnownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("loading known_hosts %s: %w", t.sshCfg.KnownHostsFile, err)
	}
	return cb, nil
}

func (t *TempCollector) pollTemperature(ctx context.Context) (float64, error) {
	hkCb, err := t.hostKeyCallback()
	if err != nil {
		return 0, err
	}
	config := &ssh.ClientConfig{
		User:            t.sshCfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(t.signer)},
		HostKeyCallback: hkCb,
		Timeout:         10 * time.Second,
	}

	addr := t.sshCfg.Host + ":22"
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		return 0, fmt.Errorf("SSH handshake with %s: %w", addr, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return 0, fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	session.Stdout = &stdout

	if err := session.Run("sensors -j 2>/dev/null"); err != nil {
		return 0, fmt.Errorf("running sensors: %w", err)
	}

	return parseSensorsJSON(stdout.Bytes())
}

// parseSensorsJSON extracts the highest CPU package temperature from `sensors -j` output.
func parseSensorsJSON(data []byte) (float64, error) {
	var sensors map[string]any
	if err := json.Unmarshal(data, &sensors); err != nil {
		return 0, fmt.Errorf("parsing sensors JSON: %w", err)
	}

	var maxTemp float64
	var found bool

	for chipName, chipData := range sensors {
		chipMap, ok := chipData.(map[string]any)
		if !ok {
			continue
		}

		// Look for coretemp or k10temp (common CPU sensor chips)
		if !isRelevantChip(chipName) {
			continue
		}

		for _, sensorData := range chipMap {
			sensorMap, ok := sensorData.(map[string]any)
			if !ok {
				continue
			}
			for key, val := range sensorMap {
				if len(key) > 6 && key[:4] == "temp" && key[len(key)-6:] == "_input" {
					if temp, ok := val.(float64); ok && temp > maxTemp {
						maxTemp = temp
						found = true
					}
				}
			}
		}
	}

	if !found {
		return 0, fmt.Errorf("no CPU temperature found in sensors output")
	}
	return maxTemp, nil
}

func isRelevantChip(name string) bool {
	relevant := []string{"coretemp", "k10temp", "zenpower", "acpitz", "Package"}
	for _, r := range relevant {
		if len(name) >= len(r) && name[:len(r)] == r {
			return true
		}
	}
	return false
}
