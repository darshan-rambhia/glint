// Package hostagent implements `glint agent`: a small, read-only reporter that
// runs on a Debian/Ubuntu host, collects package-update and reboot state, and
// pushes a signed report to a Glint server.
package hostagent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
)

// MaxPackages caps the package list in a report so a host that has not been
// updated in months cannot produce an oversized request.
const MaxPackages = 300

// Collector gathers a HostReport. Root is prepended to every path it reads so
// tests can point it at a fake filesystem; it is "/" in production.
type Collector struct {
	Root    string
	Agent   string
	Version string
	// AptSimulate returns the output of a simulated dist-upgrade.
	AptSimulate func(ctx context.Context) ([]byte, error)
	Now         func() time.Time
}

// NewCollector returns a Collector for the real host.
func NewCollector(agent, version string) *Collector {
	return &Collector{
		Root:        "/",
		Agent:       agent,
		Version:     version,
		AptSimulate: aptSimulate,
		Now:         time.Now,
	}
}

// aptSimulate runs a dry-run dist-upgrade. -s needs no root and changes
// nothing; NoLocking lets it run while apt-daily holds the lock.
func aptSimulate(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "apt-get", "-s", "-q", "-o", "Debug::NoLocking=1", "dist-upgrade")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("apt-get -s dist-upgrade: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (c *Collector) path(p string) string { return filepath.Join(c.Root, p) }

// Collect builds a report. Individual steps that fail are recorded in
// report.Errors rather than aborting, so a partial report still arrives.
func (c *Collector) Collect(ctx context.Context) *model.HostReport {
	r := &model.HostReport{
		Agent:        c.Agent,
		AgentVersion: c.Version,
		CollectedAt:  c.Now().Unix(),
	}
	fail := func(step string, err error) {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %s", step, err))
	}

	if b, err := os.ReadFile(c.path("proc/sys/kernel/hostname")); err == nil {
		r.Hostname = strings.TrimSpace(string(b))
	} else {
		fail("hostname", err)
	}

	if osr, err := c.readOSRelease(); err == nil {
		r.OS = osr
	} else {
		fail("os-release", err)
	}

	if up, err := c.readUptime(); err == nil {
		r.UptimeSecs = up
	} else {
		fail("uptime", err)
	}

	if b, err := os.ReadFile(c.path("proc/sys/kernel/osrelease")); err == nil {
		r.Kernel.Running = strings.TrimSpace(string(b))
	} else {
		fail("kernel", err)
	}
	r.Kernel.NewestInstalled = c.newestKernel()

	if out, err := c.AptSimulate(ctx); err == nil {
		r.Updates.Packages, r.Updates.Pending, r.Updates.Security = parseAptSimulate(out)
	} else {
		fail("apt", err)
	}
	r.Updates.ListsUpdatedAt = newestMtime(c.path("var/lib/apt/lists"))
	r.Updates.UnattendedLastRun = mtime(c.path("var/lib/apt/periodic/unattended-upgrades-stamp"))

	if fi, err := os.Stat(c.path("run/reboot-required")); err == nil {
		r.Reboot.Required = true
		r.Reboot.Since = fi.ModTime().Unix()
		if b, err := os.ReadFile(c.path("run/reboot-required.pkgs")); err == nil {
			r.Reboot.Packages = uniqueLines(b)
		}
	}

	return r
}

func (c *Collector) readOSRelease() (model.HostOS, error) {
	b, err := os.ReadFile(c.path("etc/os-release"))
	if err != nil {
		b, err = os.ReadFile(c.path("usr/lib/os-release"))
		if err != nil {
			return model.HostOS{}, err
		}
	}
	o := model.HostOS{Arch: runtime.GOARCH}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "PRETTY_NAME":
			o.PrettyName = v
		case "ID":
			o.ID = v
		case "VERSION_ID":
			o.VersionID = v
		}
	}
	return o, nil
}

func (c *Collector) readUptime() (int64, error) {
	b, err := os.ReadFile(c.path("proc/uptime"))
	if err != nil {
		return 0, err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	f, err := strconv.ParseFloat(first, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing %q: %w", first, err)
	}
	return int64(f), nil
}

// newestKernel returns the highest-versioned /boot/vmlinuz-<version>.
func (c *Collector) newestKernel() string {
	matches, _ := filepath.Glob(c.path("boot/vmlinuz-*"))
	var newest string
	for _, m := range matches {
		v := strings.TrimPrefix(filepath.Base(m), "vmlinuz-")
		if v == "" || !strings.ContainsAny(v[:1], "0123456789") {
			continue
		}
		if newest == "" || compareVersions(v, newest) > 0 {
			newest = v
		}
	}
	return newest
}

// aptInst matches a simulated install line, e.g.
//
//	Inst openssl [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
//	Inst newpkg (1.2-1 Debian:13.1/stable [amd64])
var aptInst = regexp.MustCompile(`^Inst (\S+) (?:\[([^\]]*)\] )?\((\S+) (.*?)(?: \[[^\]]*\])?\)`)

func parseAptSimulate(out []byte) (pkgs []model.HostPackage, pending, security int) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		m := aptInst.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		p := model.HostPackage{Name: m[1], From: m[2], To: m[3], Origin: m[4]}
		p.Security = isSecurityOrigin(p.Origin)
		pending++
		if p.Security {
			security++
		}
		if len(pkgs) < MaxPackages {
			pkgs = append(pkgs, p)
		}
	}
	// Security first, then by name, so a truncated list keeps what matters.
	sort.SliceStable(pkgs, func(i, j int) bool {
		if pkgs[i].Security != pkgs[j].Security {
			return pkgs[i].Security
		}
		return pkgs[i].Name < pkgs[j].Name
	})
	return pkgs, pending, security
}

// isSecurityOrigin reports whether an apt origin list includes a security
// pocket: Ubuntu "noble-security", Debian "Debian-Security:13/stable-security".
func isSecurityOrigin(origin string) bool {
	return strings.Contains(strings.ToLower(origin), "security")
}

func mtime(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.ModTime().Unix()
}

func newestMtime(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var newest int64
	for _, e := range entries {
		if e.IsDir() || e.Name() == "lock" {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().Unix() > newest {
			newest = fi.ModTime().Unix()
		}
	}
	return newest
}

func uniqueLines(b []byte) []string {
	seen := map[string]bool{}
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

// compareVersions compares kernel release strings by their numeric runs, so
// 6.17.13-21-pve > 6.17.9-1-pve and 7.0.14-25-pve > 6.17.13-21-pve.
func compareVersions(a, b string) int {
	na, nb := numericRuns(a), numericRuns(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			if na[i] < nb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(na) < len(nb):
		return -1
	case len(na) > len(nb):
		return 1
	}
	return strings.Compare(a, b)
}

var digits = regexp.MustCompile(`\d+`)

func numericRuns(s string) []int64 {
	runs := digits.FindAllString(s, -1)
	out := make([]int64, 0, len(runs))
	for _, d := range runs {
		n, _ := strconv.ParseInt(d, 10, 64)
		out = append(out, n)
	}
	return out
}
