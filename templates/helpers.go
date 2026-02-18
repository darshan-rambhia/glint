// Package templates provides helper functions for templ templates.
package templates

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
)

// FormatBytes formats bytes into human-readable form.
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), units[exp])
}

// FormatPct formats a 0-100 percentage.
func FormatPct(v float64) string {
	return fmt.Sprintf("%.0f%%", v)
}

// FormatUptime formats seconds into "Xd Yh" form.
func FormatUptime(secs int64) string {
	d := secs / 86400
	h := (secs % 86400) / 3600
	if d > 0 {
		return fmt.Sprintf("%dd %dh", d, h)
	}
	m := (secs % 3600) / 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// FormatDuration formats a duration into human-readable form.
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// FormatAge formats a unix timestamp as "Xh ago" or "Xd ago".
func FormatAge(unixTS int64) string {
	age := time.Since(time.Unix(unixTS, 0))
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}

// FormatTime formats a unix timestamp.
func FormatTime(unixTS int64) string {
	return time.Unix(unixTS, 0).Format("2006-01-02 15:04")
}

// MemPct calculates memory usage percentage.
func MemPct(used, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// DiskStatusClass returns a CSS class based on disk status.
func DiskStatusClass(status int) string {
	if status&model.StatusFailedSmart != 0 || status&model.StatusFailedScrutiny != 0 {
		return "status-critical"
	}
	if status&model.StatusWarnScrutiny != 0 {
		return "status-warning"
	}
	if status&model.StatusUnknown != 0 {
		return "status-unknown"
	}
	if status&model.StatusInternalError != 0 {
		return "status-error"
	}
	return "status-ok"
}

// GuestStatusClass returns a CSS class for guest status.
func GuestStatusClass(status string) string {
	switch status {
	case "running":
		return "status-ok"
	case "stopped":
		return "status-critical"
	default:
		return "status-warning"
	}
}

// BackupStatusLabel returns a label for backup age.
func BackupStatusLabel(backupTime int64, staleHours float64) string {
	age := time.Since(time.Unix(backupTime, 0))
	if age.Hours() > staleHours {
		return "STALE"
	}
	return "OK"
}

// BackupStatusClass returns CSS class for backup status.
func BackupStatusClass(backupTime int64, staleHours float64) string {
	age := time.Since(time.Unix(backupTime, 0))
	if age.Hours() > staleHours {
		return "status-warning"
	}
	return "status-ok"
}

// ProgressBarWidth returns a width percentage capped at 100.
func ProgressBarWidth(pct float64) float64 {
	if pct > 100 {
		return 100
	}
	if pct < 0 {
		return 0
	}
	return pct
}

// ProgressBarClass returns CSS class based on usage percentage.
func ProgressBarClass(pct float64) string {
	if pct >= 90 {
		return "bar-critical"
	}
	if pct >= 75 {
		return "bar-warning"
	}
	return "bar-ok"
}

// CountGuestsByStatus counts running and stopped guests.
func CountGuestsByStatus(guests map[string]map[int]*model.Guest) (running, stopped int) {
	for _, clusterGuests := range guests {
		for _, g := range clusterGuests {
			if g.Status == "running" {
				running++
			} else {
				stopped++
			}
		}
	}
	return
}

// SortedGuestList returns all guests as a flat sorted slice.
func SortedGuestList(guests map[string]map[int]*model.Guest) []*model.Guest {
	var list []*model.Guest
	for _, clusterGuests := range guests {
		for _, g := range clusterGuests {
			list = append(list, g)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].VMID < list[j].VMID })
	return list
}

// SortedDiskList returns all disks as a sorted slice.
func SortedDiskList(disks map[string]*model.Disk) []*model.Disk {
	var list []*model.Disk
	for _, d := range disks {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].DevPath < list[j].DevPath })
	return list
}

// AllBackupsSorted returns all backups sorted by backup ID.
func AllBackupsSorted(backups map[string]map[string]*model.Backup) []*model.Backup {
	var list []*model.Backup
	for _, instanceBackups := range backups {
		for _, b := range instanceBackups {
			list = append(list, b)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].BackupTime > list[j].BackupTime })
	return list
}

// AllTasksSorted returns all PBS tasks sorted by start time descending.
func AllTasksSorted(tasks map[string][]*model.PBSTask) []*model.PBSTask {
	var list []*model.PBSTask
	for _, instanceTasks := range tasks {
		list = append(list, instanceTasks...)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].StartTime > list[j].StartTime })
	return list
}

// TaskDuration returns formatted task duration.
func TaskDuration(t *model.PBSTask) string {
	if t.EndTime == nil {
		return "running"
	}
	d := time.Duration(*t.EndTime-t.StartTime) * time.Second
	return FormatDuration(d)
}

// TaskStatusClass returns CSS class for a PBS task status.
func TaskStatusClass(status string) string {
	if status == "OK" || status == "" {
		return "status-ok"
	}
	if len(status) >= 5 && status[:5] == "Error" {
		return "status-critical"
	}
	return "status-warning"
}

// OldestPoll returns the time since the oldest poll.
func OldestPoll(lastPoll map[string]time.Time) string {
	if len(lastPoll) == 0 {
		return "never"
	}
	var oldest time.Time
	for _, t := range lastPoll {
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	ago := time.Since(oldest)
	return fmt.Sprintf("%ds ago", int(ago.Seconds()))
}

// IntDeref safely dereferences an *int, returning 0 if nil.
func IntDeref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// Int64Deref safely dereferences an *int64, returning 0 if nil.
func Int64Deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// Float64Deref safely dereferences a *float64, returning 0 if nil.
func Float64Deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// WearoutDisplay returns wearout as string or "--" for HDDs.
func WearoutDisplay(w *int) string {
	if w == nil {
		return "--"
	}
	return fmt.Sprintf("%d%%", *w)
}

// TempDisplay returns temperature as string or "--" if nil.
func TempDisplay(t *int) string {
	if t == nil {
		return "--"
	}
	return fmt.Sprintf("%dC", *t)
}

// NodeTempDisplay returns node temperature or "--".
func NodeTempDisplay(t *float64) string {
	if t == nil {
		return "--"
	}
	return fmt.Sprintf("%.0fC", *t)
}

// HoursDisplay returns power on hours formatted or "--".
func HoursDisplay(h *int) string {
	if h == nil {
		return "--"
	}
	if *h >= 1000 {
		return fmt.Sprintf("%d,%03d", *h/1000, *h%1000)
	}
	return fmt.Sprintf("%d", *h)
}

// SparklinePolyline returns SVG polyline points string from sparkline data.
// Points are normalized to fit within a 240x40 viewBox.
func SparklinePolyline(points []model.SparklinePoint, width, height float64) string {
	if len(points) == 0 {
		return ""
	}

	minVal, maxVal := math.Inf(1), math.Inf(-1)
	for _, p := range points {
		if p.Value < minVal {
			minVal = p.Value
		}
		if p.Value > maxVal {
			maxVal = p.Value
		}
	}
	valRange := maxVal - minVal
	if valRange == 0 {
		valRange = 1 // avoid division by zero for flat lines
	}

	var b strings.Builder
	for i, p := range points {
		if i > 0 {
			b.WriteByte(' ')
		}
		x := float64(i) / float64(len(points)-1) * width
		if len(points) == 1 {
			x = width / 2
		}
		y := height - (p.Value-minVal)/valRange*height // invert Y for SVG
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}
	return b.String()
}
