package templates

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func received(at time.Time) *model.HostReceived {
	return &model.HostReceived{At: at.Unix(), Interval: 3600}
}

func TestHostStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		h     *model.HostReport
		label string
		class string
	}{
		{"never reported", &model.HostReport{}, "No data", "chip-unk"},
		{"stale", &model.HostReport{Received: received(now.Add(-3 * time.Hour))}, "Stale", "chip-crit"},
		{"reboot", &model.HostReport{Received: received(now), Reboot: model.HostReboot{Required: true}}, "Reboot", "chip-warn"},
		{"security", &model.HostReport{Received: received(now), Updates: model.HostUpdates{Pending: 2, Security: 1}}, "Security", "chip-warn"},
		{"updates", &model.HostReport{Received: received(now), Updates: model.HostUpdates{Pending: 2}}, "Updates", "chip-unk"},
		{"clean", &model.HostReport{Received: received(now)}, "Up to date", "chip-ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, class := HostStatus(tt.h, now)
			assert.Equal(t, tt.label, label)
			assert.Equal(t, tt.class, class)
			assert.Equal(t, tt.label, HostStatusLabel(tt.h))
			assert.Equal(t, tt.class, HostStatusClass(tt.h))
		})
	}
}

func TestHostStale_NoInterval(t *testing.T) {
	h := &model.HostReport{Received: &model.HostReceived{At: 1}}
	assert.False(t, HostStale(h, time.Now()), "unknown interval never counts as stale")
}

func TestHostUpdatesTitle(t *testing.T) {
	assert.Empty(t, HostUpdatesTitle(&model.HostReport{}))

	h := &model.HostReport{Updates: model.HostUpdates{Pending: 25}}
	h.Updates.Packages = append(h.Updates.Packages, model.HostPackage{Name: "openssl", From: "1", To: "2", Security: true})
	for range 24 {
		h.Updates.Packages = append(h.Updates.Packages, model.HostPackage{Name: "p", From: "1", To: "2"})
	}
	title := HostUpdatesTitle(h)
	assert.Contains(t, title, "[security] openssl 1 → 2\n")
	assert.Contains(t, title, "… and 5 more")
}

func TestHostSmallHelpers(t *testing.T) {
	h := &model.HostReport{
		Errors: []string{"apt: boom", "uptime: bad"},
		Kernel: model.HostKernel{Running: "6.17.9-1-pve", NewestInstalled: "7.0.14-25-pve"},
	}
	assert.Equal(t, "apt: boom\nuptime: bad", HostErrorsTitle(h))
	assert.True(t, HostKernelPending(h))
	h.Kernel.NewestInstalled = h.Kernel.Running
	assert.False(t, HostKernelPending(h))
	assert.False(t, HostKernelPending(&model.HostReport{}))

	assert.Equal(t, "—", FormatAgeOrDash(0))
	assert.Equal(t, "2h", FormatAgeOrDash(time.Now().Add(-2*time.Hour).Unix()))

	assert.Zero(t, HostReceivedAt(&model.HostReport{}))
	assert.Equal(t, int64(5), HostReceivedAt(&model.HostReport{Received: &model.HostReceived{At: 5}}))
}

func TestSortedHostsAndCounts(t *testing.T) {
	now := time.Now()
	hosts := map[string]*model.HostReport{
		"pve":  {Agent: "pve", Received: received(now)},
		"edge": {Agent: "edge", Received: received(now), Updates: model.HostUpdates{Security: 1}},
	}
	sorted := SortedHosts(hosts)
	require.Len(t, sorted, 2)
	assert.Equal(t, "edge", sorted[0].Agent)

	total, attention := HostCounts(hosts)
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, attention)
}

func renderHosts(t *testing.T, hosts map[string]*model.HostReport) string {
	t.Helper()
	c := cache.New()
	for _, h := range hosts {
		c.UpdateHost(h)
	}
	var buf bytes.Buffer
	require.NoError(t, HostsFragment(c.Snapshot()).Render(context.Background(), &buf))
	return buf.String()
}

func TestHostsFragment(t *testing.T) {
	assert.Contains(t, renderHosts(t, nil), "No host agents configured.")

	now := time.Now()
	html := renderHosts(t, map[string]*model.HostReport{
		"edge": {
			Agent:    "edge",
			Hostname: "pangolin",
			OS:       model.HostOS{PrettyName: "Ubuntu 24.04.3 LTS"},
			Kernel:   model.HostKernel{Running: "6.8.0-1020-oracle", NewestInstalled: "6.8.0-1021-oracle"},
			Updates:  model.HostUpdates{Pending: 4, Security: 2, ListsUpdatedAt: now.Add(-time.Hour).Unix()},
			Reboot:   model.HostReboot{Required: true, Since: now.Add(-3 * time.Hour).Unix()},
			Received: received(now),
		},
		"pve": {Agent: "pve", Received: received(now)},
	})
	assert.Contains(t, html, "2 agents · 1 need attention")
	assert.Contains(t, html, "Ubuntu 24.04.3 LTS")
	assert.Contains(t, html, "→ 6.8.0-1021-oracle")
	assert.Contains(t, html, `<span class="chip chip-warn">2</span>`)
	assert.Contains(t, html, `<span class="chip chip-warn">3h</span>`)
	assert.Contains(t, html, "Reboot")
	assert.Contains(t, html, "Up to date")

	allClean := renderHosts(t, map[string]*model.HostReport{"pve": {Agent: "pve", Received: received(now)}})
	assert.Contains(t, allClean, "1 agents")
	assert.NotContains(t, allClean, "need attention")
}

func TestDashboard_HostsSectionOnlyWithAgents(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Dashboard(cache.New().Snapshot()).Render(context.Background(), &buf))
	assert.NotContains(t, buf.String(), "hosts-section")

	c := cache.New()
	c.UpdateHost(&model.HostReport{Agent: "edge"})
	buf.Reset()
	require.NoError(t, Dashboard(c.Snapshot()).Render(context.Background(), &buf))
	assert.Contains(t, buf.String(), `href="#hosts-section"`)
	assert.Contains(t, buf.String(), `hx-get="/fragments/hosts"`)
}
