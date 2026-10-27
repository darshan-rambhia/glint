package templates

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
)

// AgentMissedReports is how many report intervals may pass before a host is
// shown as stale. Set at startup from the alert config, like BackupStaleHours.
var AgentMissedReports = 2

// SortedHosts returns host reports ordered by agent name.
func SortedHosts(hosts map[string]*model.HostReport) []*model.HostReport {
	out := make([]*model.HostReport, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out
}

// HostStale reports whether a host has missed AgentMissedReports intervals.
func HostStale(h *model.HostReport, now time.Time) bool {
	if h.Received == nil || h.Received.At == 0 || h.Received.Interval <= 0 {
		return false
	}
	limit := time.Duration(AgentMissedReports) * time.Duration(h.Received.Interval) * time.Second
	return now.Sub(time.Unix(h.Received.At, 0)) > limit
}

// HostStatus returns the chip label and class for a host, worst state first.
func HostStatus(h *model.HostReport, now time.Time) (label, class string) {
	switch {
	case h.Received == nil || h.Received.At == 0:
		return "No data", "chip-unk"
	case HostStale(h, now):
		return "Stale", "chip-crit"
	case h.Reboot.Required:
		return "Reboot", "chip-warn"
	case h.Updates.Security > 0:
		return "Security", "chip-warn"
	case h.Updates.Pending > 0:
		return "Updates", "chip-unk"
	default:
		return "Up to date", "chip-ok"
	}
}

// HostStatusLabel and HostStatusClass split HostStatus for templ attributes.
func HostStatusLabel(h *model.HostReport) string { l, _ := HostStatus(h, time.Now()); return l }
func HostStatusClass(h *model.HostReport) string { _, c := HostStatus(h, time.Now()); return c }

// HostUpdatesTitle lists pending packages for a hover tooltip.
func HostUpdatesTitle(h *model.HostReport) string {
	const show = 20
	if len(h.Updates.Packages) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range h.Updates.Packages {
		if i == show {
			fmt.Fprintf(&b, "… and %d more", h.Updates.Pending-show)
			break
		}
		if p.Security {
			b.WriteString("[security] ")
		}
		fmt.Fprintf(&b, "%s %s → %s\n", p.Name, p.From, p.To)
	}
	return strings.TrimRight(b.String(), "\n")
}

// HostErrorsTitle lists the agent's failed collection steps, if any.
func HostErrorsTitle(h *model.HostReport) string {
	return strings.Join(h.Errors, "\n")
}

// HostKernelPending reports whether a newer kernel is installed than the one
// running (a pinned kernel shows here too; that is intentional information).
func HostKernelPending(h *model.HostReport) bool {
	return h.Kernel.NewestInstalled != "" && h.Kernel.Running != "" && h.Kernel.NewestInstalled != h.Kernel.Running
}

// FormatAgeOrDash is FormatAge, or an em dash for an unknown (zero) time.
func FormatAgeOrDash(unixTS int64) string {
	if unixTS <= 0 {
		return "—"
	}
	return FormatAge(unixTS)
}

// HostReceivedAt returns when Glint last received a report, or 0.
func HostReceivedAt(h *model.HostReport) int64 {
	if h.Received == nil {
		return 0
	}
	return h.Received.At
}

// HostCounts summarizes host reports for the section header.
func HostCounts(hosts map[string]*model.HostReport) (total, attention int) {
	now := time.Now()
	for _, h := range hosts {
		total++
		if _, class := HostStatus(h, now); class != "chip-ok" {
			attention++
		}
	}
	return total, attention
}
