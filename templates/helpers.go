// Package templates provides helper functions for templ templates.
package templates

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
)

// CSSVersion is appended as a query parameter to static asset URLs so browsers
// pick up new CSS after a redeploy. Set this at startup (e.g. to the git commit).
var CSSVersion = "dev"

// BackupStaleHours is the age threshold (in hours) above which a backup is
// considered stale in the UI. Set this at startup from the alert config so the
// dashboard chip matches the alerter threshold.
var BackupStaleHours float64 = 36

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
	units := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
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

// DiskStatusClass returns a CSS chip class based on disk status.
func DiskStatusClass(status int) string {
	if status&model.StatusFailedSmart != 0 || status&model.StatusFailedScrutiny != 0 {
		return "chip-crit"
	}
	if status&model.StatusWarnScrutiny != 0 {
		return "chip-warn"
	}
	if status&model.StatusUnknown != 0 {
		return "chip-unk"
	}
	if status&model.StatusInternalError != 0 {
		return "chip-warn"
	}
	return "chip-ok"
}

// GuestStatusClass returns a CSS chip class for guest status.
func GuestStatusClass(status string) string {
	switch status {
	case "running":
		return "chip-ok"
	case "stopped":
		return "chip-crit"
	default:
		return "chip-warn"
	}
}

// BackupStatusLabel returns a label for backup age.
func BackupStatusLabel(backupTime int64, staleHours float64) string {
	age := time.Since(time.Unix(backupTime, 0))
	if age.Hours() > staleHours {
		return "Stale"
	}
	return "Ok"
}

// BackupStatusClass returns CSS chip class for backup status.
func BackupStatusClass(backupTime int64, staleHours float64) string {
	age := time.Since(time.Unix(backupTime, 0))
	if age.Hours() > staleHours {
		return "chip-warn"
	}
	return "chip-ok"
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
		return "bar-crit"
	}
	if pct >= 75 {
		return "bar-warn"
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

// InstanceNode pairs a PVE instance name with a Node for sorted iteration.
type InstanceNode struct {
	Instance string
	Node     *model.Node
}

// InstanceDatastore pairs a PBS instance name with a DatastoreStatus for sorted iteration.
type InstanceDatastore struct {
	Instance  string
	Datastore *model.DatastoreStatus
}

// SortedNodeList returns all nodes as a flat slice sorted by instance+name.
func SortedNodeList(nodes map[string]map[string]*model.Node) []InstanceNode {
	var list []InstanceNode
	for instance, instanceNodes := range nodes {
		for _, n := range instanceNodes {
			list = append(list, InstanceNode{Instance: instance, Node: n})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Instance+"/"+list[i].Node.Name < list[j].Instance+"/"+list[j].Node.Name
	})
	return list
}

// SortedDatastoreList returns all datastores as a flat slice sorted by instance+name.
func SortedDatastoreList(datastores map[string]map[string]*model.DatastoreStatus) []InstanceDatastore {
	var list []InstanceDatastore
	for instance, instanceDatastores := range datastores {
		for _, ds := range instanceDatastores {
			list = append(list, InstanceDatastore{Instance: instance, Datastore: ds})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Instance+"/"+list[i].Datastore.Name < list[j].Instance+"/"+list[j].Datastore.Name
	})
	return list
}

// NodeCount returns the total number of nodes across all PVE instances.
func NodeCount(nodes map[string]map[string]*model.Node) int {
	total := 0
	for _, ns := range nodes {
		total += len(ns)
	}
	return total
}

// DatastoreCount returns the total number of datastores across all PBS instances.
func DatastoreCount(datastores map[string]map[string]*model.DatastoreStatus) int {
	total := 0
	for _, ds := range datastores {
		total += len(ds)
	}
	return total
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
	sort.Slice(list, func(i, j int) bool {
		if list[i].BackupTime != list[j].BackupTime {
			return list[i].BackupTime > list[j].BackupTime
		}
		if list[i].BackupID != list[j].BackupID {
			return list[i].BackupID < list[j].BackupID
		}
		return list[i].Datastore < list[j].Datastore
	})
	return list
}

// BackupsForGuest returns all backups for a given VMID across all PBS instances
// and datastores, sorted by backup time descending (most recent first).
//
// PBS backup-id values vary depending on setup:
//   - PVE-initiated backups use the plain VMID: "101"
//   - Client-initiated (proxmox-backup-client) backups often use a prefixed form
//     such as "lxc-101" or "qemu-101"
//
// We match on both: exact equality OR the VMID appearing as the numeric suffix
// after a "-" separator.
func BackupsForGuest(backups map[string]map[string]*model.Backup, vmid int) []*model.Backup {
	vmidStr := fmt.Sprintf("%d", vmid)
	var result []*model.Backup
	for _, instanceBackups := range backups {
		for _, b := range instanceBackups {
			if backupIDMatchesVMID(b.BackupID, vmidStr) {
				result = append(result, b)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BackupTime > result[j].BackupTime })
	return result
}

// backupIDMatchesVMID returns true when a PBS backup-id corresponds to the given
// VMID string. It handles both the plain form ("101") and the type-prefixed form
// ("lxc-101", "qemu-101") by checking for an exact match or a "-{vmid}" suffix.
func backupIDMatchesVMID(backupID, vmidStr string) bool {
	return backupID == vmidStr || strings.HasSuffix(backupID, "-"+vmidStr)
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

// LatestBackupTime returns the most recent backup timestamp for a guest, or 0 if none.
func LatestBackupTime(backups map[string]map[string]*model.Backup, vmid int) int64 {
	bs := BackupsForGuest(backups, vmid)
	if len(bs) == 0 {
		return 0
	}
	return bs[0].BackupTime
}

// IntPtrSortValue returns the integer as a string, or "-1" if the pointer is nil.
// Used for data-sort-value attributes so nil values sort before valid ones.
func IntPtrSortValue(p *int) string {
	if p == nil {
		return "-1"
	}
	return fmt.Sprintf("%d", *p)
}

// TaskDurationSeconds returns the raw duration in seconds for a completed task,
// or -1 for still-running tasks (so they sort before completed ones).
func TaskDurationSeconds(t *model.PBSTask) int64 {
	if t.EndTime == nil {
		return -1
	}
	return *t.EndTime - t.StartTime
}

// TaskDuration returns formatted task duration.
func TaskDuration(t *model.PBSTask) string {
	if t.EndTime == nil {
		return "running"
	}
	d := time.Duration(*t.EndTime-t.StartTime) * time.Second
	return FormatDuration(d)
}

// TaskStatusClass returns CSS chip class for a PBS task status.
func TaskStatusClass(status string) string {
	if status == "OK" || status == "" {
		return "chip-ok"
	}
	if len(status) >= 5 && status[:5] == "Error" {
		return "chip-crit"
	}
	return "chip-warn"
}

// HeaderSummary returns a compact summary string for the dashboard header badge.
func HeaderSummary(snap cache.CacheSnapshot) string {
	nodeCount := 0
	for _, nodes := range snap.Nodes {
		nodeCount += len(nodes)
	}
	running, _ := CountGuestsByStatus(snap.Guests)
	return fmt.Sprintf("%d nodes · %d running", nodeCount, running)
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
