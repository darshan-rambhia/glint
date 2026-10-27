package hostagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const aptUbuntu = `NOTE: This is only a simulation!
      apt-get needs root privileges for real execution.
Reading package lists...
The following packages will be upgraded:
  libssl3t64 openssl tzdata
Inst openssl [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Inst libssl3t64 [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Inst tzdata [2024a-3ubuntu1.1] (2024b-0ubuntu0.24.04 Ubuntu:24.04/noble-updates [all])
Inst linux-image-6.8.0-1021-oracle (6.8.0-1021.22 Ubuntu:24.04/noble-updates [arm64])
Conf openssl (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
`

// Captured from a Debian trixie LXC.
const aptDebian = `Inst docker-ce-cli [5:29.1.2-1~debian.13~trixie] (5:29.9.2-1~debian.13~trixie Docker CE:trixie [amd64])
Inst tzdata [2026c-0+deb13u1] (2026e-0+deb13u1 Debian:13-updates/stable-updates [all])
Inst libc6 [2.41-12] (2.41-12+deb13u1 Debian-Security:13/stable-security [amd64])
`

func TestParseAptSimulate_Ubuntu(t *testing.T) {
	pkgs, pending, security := parseAptSimulate([]byte(aptUbuntu))
	assert.Equal(t, 4, pending)
	assert.Equal(t, 2, security)
	require.Len(t, pkgs, 4)

	// Security first, then by name.
	assert.Equal(t, "libssl3t64", pkgs[0].Name)
	assert.True(t, pkgs[0].Security)
	assert.Equal(t, "openssl", pkgs[1].Name)
	assert.Equal(t, "3.0.13-0ubuntu3.4", pkgs[1].From)
	assert.Equal(t, "3.0.13-0ubuntu3.5", pkgs[1].To)
	assert.Equal(t, "Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security", pkgs[1].Origin)

	// A new install has no old version.
	assert.Equal(t, "linux-image-6.8.0-1021-oracle", pkgs[2].Name)
	assert.Empty(t, pkgs[2].From)
	assert.False(t, pkgs[2].Security)
}

func TestParseAptSimulate_Debian(t *testing.T) {
	pkgs, pending, security := parseAptSimulate([]byte(aptDebian))
	assert.Equal(t, 3, pending)
	assert.Equal(t, 1, security)
	assert.Equal(t, "libc6", pkgs[0].Name)
	assert.Equal(t, "Docker CE:trixie", pkgs[1].Origin, "origins may contain spaces")
}

func TestParseAptSimulate_CapsPackageList(t *testing.T) {
	var out []byte
	for range MaxPackages + 5 {
		out = append(out, "Inst pkg [1] (2 Debian:13/stable [amd64])\n"...)
	}
	pkgs, pending, _ := parseAptSimulate(out)
	assert.Equal(t, MaxPackages+5, pending, "the count is not capped")
	assert.Len(t, pkgs, MaxPackages)
}

func TestParseAptSimulate_Empty(t *testing.T) {
	pkgs, pending, security := parseAptSimulate([]byte("Reading package lists...\n0 upgraded, 0 newly installed\n"))
	assert.Empty(t, pkgs)
	assert.Zero(t, pending)
	assert.Zero(t, security)
}

func TestCompareVersions(t *testing.T) {
	assert.Positive(t, compareVersions("6.17.13-21-pve", "6.17.9-1-pve"))
	assert.Positive(t, compareVersions("7.0.14-25-pve", "6.17.13-21-pve"))
	assert.Positive(t, compareVersions("6.17.13-21-pve", "6.17.13-2-pve"))
	assert.Negative(t, compareVersions("6.8.0-1020-oracle", "6.8.0-1021-oracle"))
	assert.Negative(t, compareVersions("6.8", "6.8.1"))
	assert.Positive(t, compareVersions("6.8.1", "6.8"))
	assert.Zero(t, compareVersions("6.8.0-1-pve", "6.8.0-1-pve"))
}

func write(t *testing.T, root, rel, content string, mod time.Time) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	if !mod.IsZero() {
		require.NoError(t, os.Chtimes(p, mod, mod))
	}
}

func fakeHost(t *testing.T) (string, time.Time) {
	t.Helper()
	root := t.TempDir()
	at := time.Unix(1_800_000_000, 0)
	write(t, root, "proc/sys/kernel/hostname", "edge\n", time.Time{})
	write(t, root, "proc/sys/kernel/osrelease", "6.8.0-1020-oracle\n", time.Time{})
	write(t, root, "proc/uptime", "12345.67 98765.43\n", time.Time{})
	write(t, root, "etc/os-release", "PRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\nID=ubuntu\nVERSION_ID=\"24.04\"\nnot a pair\n", time.Time{})
	write(t, root, "boot/vmlinuz-6.8.0-1020-oracle", "", time.Time{})
	write(t, root, "boot/vmlinuz-6.8.0-1021-oracle", "", time.Time{})
	write(t, root, "boot/vmlinuz.old", "", time.Time{})
	write(t, root, "var/lib/apt/lists/ports.ubuntu.com_InRelease", "", at.Add(-2*time.Hour))
	write(t, root, "var/lib/apt/lists/ports.ubuntu.com_Packages", "", at.Add(-time.Hour))
	write(t, root, "var/lib/apt/lists/lock", "", at)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "var/lib/apt/lists/partial"), 0o755))
	write(t, root, "var/lib/apt/periodic/unattended-upgrades-stamp", "", at.Add(-30*time.Minute))
	return root, at
}

func testCollector(root string, at time.Time, apt string, aptErr error) *Collector {
	return &Collector{
		Root:    root,
		Agent:   "oci-edge",
		Version: "test",
		AptSimulate: func(context.Context) ([]byte, error) {
			return []byte(apt), aptErr
		},
		Now: func() time.Time { return at },
	}
}

func TestCollect_FullReport(t *testing.T) {
	root, at := fakeHost(t)
	write(t, root, "run/reboot-required", "*** System restart required ***\n", at.Add(-3*time.Hour))
	write(t, root, "run/reboot-required.pkgs", "linux-base\nlibc6\nlinux-base\n", time.Time{})

	r := testCollector(root, at, aptUbuntu, nil).Collect(context.Background())

	assert.Empty(t, r.Errors)
	assert.Equal(t, "oci-edge", r.Agent)
	assert.Equal(t, "test", r.AgentVersion)
	assert.Equal(t, at.Unix(), r.CollectedAt)
	assert.Equal(t, "edge", r.Hostname)
	assert.Equal(t, "Ubuntu 24.04.3 LTS", r.OS.PrettyName)
	assert.Equal(t, "ubuntu", r.OS.ID)
	assert.Equal(t, "24.04", r.OS.VersionID)
	assert.NotEmpty(t, r.OS.Arch)
	assert.Equal(t, int64(12345), r.UptimeSecs)
	assert.Equal(t, "6.8.0-1020-oracle", r.Kernel.Running)
	assert.Equal(t, "6.8.0-1021-oracle", r.Kernel.NewestInstalled)
	assert.Equal(t, 4, r.Updates.Pending)
	assert.Equal(t, 2, r.Updates.Security)
	assert.Equal(t, at.Add(-time.Hour).Unix(), r.Updates.ListsUpdatedAt, "lock files are ignored")
	assert.Equal(t, at.Add(-30*time.Minute).Unix(), r.Updates.UnattendedLastRun)
	assert.True(t, r.Reboot.Required)
	assert.Equal(t, at.Add(-3*time.Hour).Unix(), r.Reboot.Since)
	assert.Equal(t, []string{"linux-base", "libc6"}, r.Reboot.Packages)
}

func TestCollect_NoRebootAndUsrLibOSRelease(t *testing.T) {
	root, at := fakeHost(t)
	require.NoError(t, os.Remove(filepath.Join(root, "etc/os-release")))
	write(t, root, "usr/lib/os-release", "PRETTY_NAME='Debian GNU/Linux 13 (trixie)'\nID=debian\n", time.Time{})

	r := testCollector(root, at, "", nil).Collect(context.Background())

	assert.Empty(t, r.Errors)
	assert.Equal(t, "Debian GNU/Linux 13 (trixie)", r.OS.PrettyName)
	assert.False(t, r.Reboot.Required)
	assert.Zero(t, r.Reboot.Since)
	assert.Zero(t, r.Updates.Pending)
}

func TestCollect_PartialFailuresAreReported(t *testing.T) {
	root := t.TempDir() // nothing exists
	write(t, root, "proc/uptime", "garbage\n", time.Time{})

	r := testCollector(root, time.Unix(1, 0), "", errors.New("apt-get missing")).Collect(context.Background())

	require.Len(t, r.Errors, 5)
	assert.Contains(t, r.Errors[0], "hostname")
	assert.Contains(t, r.Errors[1], "os-release")
	assert.Contains(t, r.Errors[2], "uptime")
	assert.Contains(t, r.Errors[3], "kernel")
	assert.Contains(t, r.Errors[4], "apt: apt-get missing")
	assert.Empty(t, r.Kernel.NewestInstalled)
	assert.Zero(t, r.Updates.ListsUpdatedAt)
	assert.Zero(t, r.Updates.UnattendedLastRun)
}

func TestNewCollector(t *testing.T) {
	c := NewCollector("n", "v")
	assert.Equal(t, "/", c.Root)
	assert.NotNil(t, c.AptSimulate)
	assert.NotNil(t, c.Now)
}

func TestAptSimulate_MissingBinaryErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := aptSimulate(context.Background())
	assert.Error(t, err)
}
