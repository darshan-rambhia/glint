package collector

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// testSSHKeyFile creates a temporary Ed25519 SSH key file for tests and returns
// its path. The key is cleaned up automatically when the test finishes.
func testSSHKeyFile(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	privPEM, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)

	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(privPEM), 0o600))
	return keyPath
}

// ---------------------------------------------------------------------------
// parseSensorsJSON
// ---------------------------------------------------------------------------

func TestParseSensorsJSON_Coretemp(t *testing.T) {
	data := []byte(`{
		"coretemp-isa-0000": {
			"Adapter": "ISA adapter",
			"Package id 0": {
				"temp1_input": 52.000,
				"temp1_max": 100.000,
				"temp1_crit": 100.000,
				"temp1_crit_alarm": 0.000
			},
			"Core 0": {
				"temp2_input": 48.000,
				"temp2_max": 100.000
			},
			"Core 1": {
				"temp3_input": 50.000,
				"temp3_max": 100.000
			}
		},
		"nvme0": {
			"Adapter": "Virtual device",
			"Composite": {
				"temp1_input": 38.000
			}
		}
	}`)

	temp, err := parseSensorsJSON(data)
	require.NoError(t, err)
	assert.InDelta(t, 52.0, temp, 0.01) // highest from coretemp
}

func TestParseSensorsJSON_K10temp(t *testing.T) {
	data := []byte(`{
		"k10temp-pci-00c3": {
			"Adapter": "PCI adapter",
			"Tctl": {
				"temp1_input": 65.500
			},
			"Tccd1": {
				"temp3_input": 61.250
			}
		}
	}`)

	temp, err := parseSensorsJSON(data)
	require.NoError(t, err)
	assert.InDelta(t, 65.5, temp, 0.01)
}

func TestParseSensorsJSON_NoCPUSensor(t *testing.T) {
	data := []byte(`{
		"nvme0": {
			"Adapter": "Virtual device",
			"Composite": {
				"temp1_input": 38.000
			}
		}
	}`)

	_, err := parseSensorsJSON(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no CPU temperature found")
}

func TestParseSensorsJSON_InvalidJSON(t *testing.T) {
	_, err := parseSensorsJSON([]byte(`{invalid`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing sensors JSON")
}

func TestParseSensorsJSON_EmptyOutput(t *testing.T) {
	_, err := parseSensorsJSON([]byte(`{}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no CPU temperature found")
}

func TestParseSensorsJSON_MultipleChips(t *testing.T) {
	// Two coretemp chips, pick highest overall temp
	data := []byte(`{
		"coretemp-isa-0000": {
			"Adapter": "ISA adapter",
			"Package id 0": {
				"temp1_input": 45.000
			}
		},
		"coretemp-isa-0001": {
			"Adapter": "ISA adapter",
			"Package id 1": {
				"temp1_input": 55.000
			}
		}
	}`)

	temp, err := parseSensorsJSON(data)
	require.NoError(t, err)
	assert.InDelta(t, 55.0, temp, 0.01)
}

// ---------------------------------------------------------------------------
// isRelevantChip
// ---------------------------------------------------------------------------

func TestIsRelevantChip(t *testing.T) {
	tests := []struct {
		name     string
		chip     string
		relevant bool
	}{
		{"coretemp-isa-0000", "coretemp-isa-0000", true},
		{"k10temp-pci-00c3", "k10temp-pci-00c3", true},
		{"zenpower-pci-00c3", "zenpower-pci-00c3", true},
		{"acpitz-acpi-0", "acpitz-acpi-0", true},
		{"Package id 0", "Package id 0", true},
		{"nvme0", "nvme0", false},
		{"iwlwifi_1", "iwlwifi_1", false},
		{"nouveau-pci-0100", "nouveau-pci-0100", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.relevant, isRelevantChip(tt.chip))
		})
	}
}

// ---------------------------------------------------------------------------
// NewTempCollector
// ---------------------------------------------------------------------------

func TestNewTempCollector_ValidKey(t *testing.T) {
	keyPath := testSSHKeyFile(t)
	c := cache.New()
	cfg := SSHConfig{Host: "192.168.1.215", User: "root", KeyPath: keyPath}
	tc, err := NewTempCollector("homelab", "pve", cfg, c)
	require.NoError(t, err)

	assert.Equal(t, "temp:homelab/pve", tc.Name())
	assert.Equal(t, 60*time.Second, tc.Interval())
	assert.Equal(t, "homelab", tc.instance)
	assert.Equal(t, "pve", tc.node)
	assert.Equal(t, "192.168.1.215", tc.sshCfg.Host)
	assert.NotNil(t, tc.signer)
}

func TestNewTempCollector_BadKeyPath(t *testing.T) {
	c := cache.New()
	cfg := SSHConfig{Host: "127.0.0.1", User: "root", KeyPath: "/nonexistent/key"}
	_, err := NewTempCollector("homelab", "pve", cfg, c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading SSH key")
}

func TestNewTempCollector_InvalidKey(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bad_key")
	require.NoError(t, os.WriteFile(tmpFile, []byte("not a valid key"), 0o600))

	c := cache.New()
	cfg := SSHConfig{Host: "127.0.0.1", User: "root", KeyPath: tmpFile}
	_, err := NewTempCollector("homelab", "pve", cfg, c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing SSH key")
}

// ---------------------------------------------------------------------------
// Collect (graceful degradation)
// ---------------------------------------------------------------------------

func TestTempCollect_GracefulFallbackOnConnFailure(t *testing.T) {
	keyPath := testSSHKeyFile(t)
	c := cache.New()
	cfg := SSHConfig{Host: "127.0.0.1", User: "root", KeyPath: keyPath}
	tc, err := NewTempCollector("homelab", "pve", cfg, c)
	require.NoError(t, err)

	// Collect should not return error even when SSH connection fails (graceful degradation)
	err = tc.Collect(context.Background())
	assert.NoError(t, err)

	// Cache should not have temperature (poll failed)
	snap := c.Snapshot()
	if nodes, ok := snap.Nodes["homelab"]; ok {
		if node, ok := nodes["pve"]; ok {
			assert.Nil(t, node.Temperature)
		}
	}
}

func TestPollTemperature_ConnectionFailure(t *testing.T) {
	keyPath := testSSHKeyFile(t)
	c := cache.New()
	// Use an unreachable address to force a connection error
	cfg := SSHConfig{Host: "192.0.2.1", User: "root", KeyPath: keyPath}
	tc, err := NewTempCollector("homelab", "pve", cfg, c)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = tc.pollTemperature(ctx)
	assert.Error(t, err)
}
