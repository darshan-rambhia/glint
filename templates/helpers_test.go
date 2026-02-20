package templates

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 500, "500 B"},
		{"kilobytes", 1536, "1.5 KB"},
		{"megabytes", 10_485_760, "10.0 MB"},
		{"gigabytes", 8_000_000_000, "7.5 GB"},
		{"terabytes", 4_000_000_000_000, "3.6 TB"},
		{"petabytes", 1_125_899_906_842_624, "1.0 PB"},
		{"exabytes", 1_152_921_504_606_846_976, "1.0 EB"},
		{"max int64", math.MaxInt64, "8.0 EB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatBytes(tt.input))
		})
	}
}

func TestFormatPct(t *testing.T) {
	assert.Equal(t, "42%", FormatPct(42.3))
	assert.Equal(t, "0%", FormatPct(0))
	assert.Equal(t, "100%", FormatPct(100.0))
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		secs     int64
		expected string
	}{
		{"minutes only", 3600, "1h 0m"},
		{"hours only", 7200, "2h 0m"},
		{"days and hours", 90000, "1d 1h"},
		{"many days", 4000000, "46d 7h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FormatUptime(tt.secs))
		})
	}
}

func TestFormatDuration(t *testing.T) {
	assert.Equal(t, "30s", FormatDuration(30*time.Second))
	assert.Equal(t, "5m 30s", FormatDuration(5*time.Minute+30*time.Second))
	assert.Equal(t, "2h 15m", FormatDuration(2*time.Hour+15*time.Minute))
}

func TestFormatAge(t *testing.T) {
	now := time.Now()
	// 30 minutes ago
	assert.Equal(t, "30m", FormatAge(now.Add(-30*time.Minute).Unix()))
	// 5 hours ago
	assert.Equal(t, "5h", FormatAge(now.Add(-5*time.Hour).Unix()))
	// 3 days ago
	assert.Equal(t, "3d", FormatAge(now.Add(-72*time.Hour).Unix()))
}

func TestFormatTime(t *testing.T) {
	ts := time.Date(2026, 2, 16, 14, 30, 0, 0, time.UTC).Unix()
	result := FormatTime(ts)
	assert.Contains(t, result, "2026-02-16")
}

func TestMemPct(t *testing.T) {
	assert.InDelta(t, 50.0, MemPct(8_000_000_000, 16_000_000_000), 0.01)
	assert.InDelta(t, 0.0, MemPct(0, 16_000_000_000), 0.01)
	assert.InDelta(t, 0.0, MemPct(100, 0), 0.01) // divide by zero guard
}

func TestDiskStatusClass(t *testing.T) {
	assert.Equal(t, "chip-ok", DiskStatusClass(model.StatusPassed))
	assert.Equal(t, "chip-crit", DiskStatusClass(model.StatusFailedSmart))
	assert.Equal(t, "chip-crit", DiskStatusClass(model.StatusFailedScrutiny))
	assert.Equal(t, "chip-warn", DiskStatusClass(model.StatusWarnScrutiny))
	assert.Equal(t, "chip-unk", DiskStatusClass(model.StatusUnknown))
	assert.Equal(t, "chip-warn", DiskStatusClass(model.StatusInternalError))
	// Combined flags
	assert.Equal(t, "chip-crit", DiskStatusClass(model.StatusFailedSmart|model.StatusWarnScrutiny))
}

func TestGuestStatusClass(t *testing.T) {
	assert.Equal(t, "chip-ok", GuestStatusClass("running"))
	assert.Equal(t, "chip-crit", GuestStatusClass("stopped"))
	assert.Equal(t, "chip-warn", GuestStatusClass("paused"))
}

func TestBackupStatusLabel(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "Ok", BackupStatusLabel(now.Add(-10*time.Hour).Unix(), 36))
	assert.Equal(t, "Stale", BackupStatusLabel(now.Add(-48*time.Hour).Unix(), 36))
}

func TestBackupStatusClass(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "chip-ok", BackupStatusClass(now.Add(-10*time.Hour).Unix(), 36))
	assert.Equal(t, "chip-warn", BackupStatusClass(now.Add(-48*time.Hour).Unix(), 36))
}

func TestProgressBarWidth(t *testing.T) {
	assert.Equal(t, 50.0, ProgressBarWidth(50))
	assert.Equal(t, 100.0, ProgressBarWidth(120))
	assert.Equal(t, 0.0, ProgressBarWidth(-5))
}

func TestProgressBarClass(t *testing.T) {
	assert.Equal(t, "bar-ok", ProgressBarClass(50))
	assert.Equal(t, "bar-warn", ProgressBarClass(80))
	assert.Equal(t, "bar-crit", ProgressBarClass(95))
}

func TestCountGuestsByStatus(t *testing.T) {
	guests := map[string]map[int]*model.Guest{
		"main": {
			101: {Status: "running"},
			102: {Status: "running"},
			103: {Status: "stopped"},
		},
	}
	running, stopped := CountGuestsByStatus(guests)
	assert.Equal(t, 2, running)
	assert.Equal(t, 1, stopped)

	// Empty
	r, s := CountGuestsByStatus(nil)
	assert.Equal(t, 0, r)
	assert.Equal(t, 0, s)
}

func TestSortedGuestList(t *testing.T) {
	guests := map[string]map[int]*model.Guest{
		"main": {
			200: {VMID: 200, Name: "traefik"},
			101: {VMID: 101, Name: "network-services"},
			304: {VMID: 304, Name: "immich"},
		},
	}
	sorted := SortedGuestList(guests)
	assert.Len(t, sorted, 3)
	assert.Equal(t, 101, sorted[0].VMID)
	assert.Equal(t, 200, sorted[1].VMID)
	assert.Equal(t, 304, sorted[2].VMID)
}

func TestSortedDiskList(t *testing.T) {
	disks := map[string]*model.Disk{
		"wwn1": {DevPath: "/dev/sdb", WWN: "wwn1"},
		"wwn2": {DevPath: "/dev/sda", WWN: "wwn2"},
		"wwn3": {DevPath: "/dev/nvme0n1", WWN: "wwn3"},
	}
	sorted := SortedDiskList(disks)
	assert.Len(t, sorted, 3)
	assert.Equal(t, "/dev/nvme0n1", sorted[0].DevPath)
	assert.Equal(t, "/dev/sda", sorted[1].DevPath)
	assert.Equal(t, "/dev/sdb", sorted[2].DevPath)
}

func TestAllBackupsSorted(t *testing.T) {
	now := time.Now().Unix()
	backups := map[string]map[string]*model.Backup{
		"main-pbs": {
			"101": {BackupID: "101", BackupTime: now - 3600},
			"304": {BackupID: "304", BackupTime: now},
		},
	}
	sorted := AllBackupsSorted(backups)
	assert.Len(t, sorted, 2)
	assert.Equal(t, "304", sorted[0].BackupID) // most recent first
	assert.Equal(t, "101", sorted[1].BackupID)

	// Empty
	empty := AllBackupsSorted(nil)
	assert.Empty(t, empty)
}

func TestAllTasksSorted(t *testing.T) {
	now := time.Now().Unix()
	tasks := map[string][]*model.PBSTask{
		"main-pbs": {
			{UPID: "1", StartTime: now - 600},
			{UPID: "2", StartTime: now},
		},
	}
	sorted := AllTasksSorted(tasks)
	assert.Len(t, sorted, 2)
	assert.Equal(t, "2", sorted[0].UPID) // most recent first

	// Empty
	assert.Empty(t, AllTasksSorted(nil))
}

func TestTaskDuration(t *testing.T) {
	end := int64(1000)
	task := &model.PBSTask{StartTime: 900, EndTime: &end}
	assert.Contains(t, TaskDuration(task), "s")

	running := &model.PBSTask{StartTime: 900, EndTime: nil}
	assert.Equal(t, "running", TaskDuration(running))
}

func TestTaskStatusClass(t *testing.T) {
	assert.Equal(t, "chip-ok", TaskStatusClass("OK"))
	assert.Equal(t, "chip-ok", TaskStatusClass(""))
	assert.Equal(t, "chip-crit", TaskStatusClass("Error: something broke"))
	assert.Equal(t, "chip-warn", TaskStatusClass("WARNINGS: disk almost full"))
}

func TestOldestPoll(t *testing.T) {
	assert.Equal(t, "never", OldestPoll(nil))
	assert.Equal(t, "never", OldestPoll(map[string]time.Time{}))

	polls := map[string]time.Time{
		"pve": time.Now().Add(-10 * time.Second),
		"pbs": time.Now().Add(-30 * time.Second),
	}
	result := OldestPoll(polls)
	assert.Contains(t, result, "ago")
}

func TestIntDeref(t *testing.T) {
	v := 42
	assert.Equal(t, 42, IntDeref(&v))
	assert.Equal(t, 0, IntDeref(nil))
}

func TestInt64Deref(t *testing.T) {
	v := int64(1234)
	assert.Equal(t, int64(1234), Int64Deref(&v))
	assert.Equal(t, int64(0), Int64Deref(nil))
}

func TestFloat64Deref(t *testing.T) {
	v := 3.14
	assert.InDelta(t, 3.14, Float64Deref(&v), 0.001)
	assert.InDelta(t, 0.0, Float64Deref(nil), 0.001)
}

func TestWearoutDisplay(t *testing.T) {
	w := 90
	assert.Equal(t, "90%", WearoutDisplay(&w))
	assert.Equal(t, "--", WearoutDisplay(nil))
}

func TestTempDisplay(t *testing.T) {
	temp := 38
	assert.Equal(t, "38C", TempDisplay(&temp))
	assert.Equal(t, "--", TempDisplay(nil))
}

func TestNodeTempDisplay(t *testing.T) {
	temp := 52.3
	assert.Equal(t, "52C", NodeTempDisplay(&temp))
	assert.Equal(t, "--", NodeTempDisplay(nil))
}

func TestHoursDisplay(t *testing.T) {
	h1 := 500
	assert.Equal(t, "500", HoursDisplay(&h1))
	h2 := 28100
	assert.Equal(t, "28,100", HoursDisplay(&h2))
	assert.Equal(t, "--", HoursDisplay(nil))
}

func TestBackupsForGuest(t *testing.T) {
	makeBackup := func(id, ds string) *model.Backup {
		return &model.Backup{BackupID: id, Datastore: ds, BackupTime: 1000}
	}
	backups := map[string]map[string]*model.Backup{
		"pbs1": {
			"homelab/101":      makeBackup("101", "homelab"),      // plain VMID
			"homelab/lxc-200":  makeBackup("lxc-200", "homelab"),  // type-prefixed
			"offsite/lxc-200":  makeBackup("lxc-200", "offsite"),  // same guest, second datastore
			"homelab/lxc-1010": makeBackup("lxc-1010", "homelab"), // longer id — must not match 10
		},
	}

	// Plain numeric backup-id
	got := BackupsForGuest(backups, 101)
	assert.Len(t, got, 1)
	assert.Equal(t, "101", got[0].BackupID)

	// Prefixed backup-id (lxc-200) present in two datastores
	got = BackupsForGuest(backups, 200)
	assert.Len(t, got, 2)

	// "lxc-1010" must not match VMID 10
	got = BackupsForGuest(backups, 10)
	assert.Empty(t, got)

	// No backups at all for this VMID
	got = BackupsForGuest(backups, 999)
	assert.Empty(t, got)
}

func TestSortedNodeList(t *testing.T) {
	n1 := &model.Node{Name: "node1"}
	n2 := &model.Node{Name: "node2"}
	nodes := map[string]map[string]*model.Node{
		"pve2": {"node2": n2},
		"pve1": {"node1": n1},
	}
	list := SortedNodeList(nodes)
	assert.Len(t, list, 2)
	assert.Equal(t, "pve1", list[0].Instance)
	assert.Equal(t, "node1", list[0].Node.Name)
	assert.Equal(t, "pve2", list[1].Instance)
}

func TestSortedNodeList_Empty(t *testing.T) {
	assert.Empty(t, SortedNodeList(nil))
}

func TestSortedDatastoreList(t *testing.T) {
	ds1 := &model.DatastoreStatus{Name: "alpha"}
	ds2 := &model.DatastoreStatus{Name: "beta"}
	datastores := map[string]map[string]*model.DatastoreStatus{
		"pbs1": {"alpha": ds1, "beta": ds2},
	}
	list := SortedDatastoreList(datastores)
	assert.Len(t, list, 2)
	assert.Equal(t, "alpha", list[0].Datastore.Name)
	assert.Equal(t, "beta", list[1].Datastore.Name)
}

func TestSortedDatastoreList_Empty(t *testing.T) {
	assert.Empty(t, SortedDatastoreList(nil))
}

func TestNodeCount(t *testing.T) {
	nodes := map[string]map[string]*model.Node{
		"pve1": {"n1": {}, "n2": {}},
		"pve2": {"n3": {}},
	}
	assert.Equal(t, 3, NodeCount(nodes))
	assert.Equal(t, 0, NodeCount(nil))
}

func TestDatastoreCount(t *testing.T) {
	ds := map[string]map[string]*model.DatastoreStatus{
		"pbs1": {"a": {}, "b": {}},
		"pbs2": {"c": {}},
	}
	assert.Equal(t, 3, DatastoreCount(ds))
	assert.Equal(t, 0, DatastoreCount(nil))
}

func TestAllBackupsSorted_TieBreak(t *testing.T) {
	b1 := &model.Backup{BackupID: "101", Datastore: "ds1", BackupTime: 200}
	b2 := &model.Backup{BackupID: "102", Datastore: "ds1", BackupTime: 100}
	b3 := &model.Backup{BackupID: "101", Datastore: "ds2", BackupTime: 200}
	backups := map[string]map[string]*model.Backup{
		"pbs1": {"a": b1, "b": b2, "c": b3},
	}
	list := AllBackupsSorted(backups)
	assert.Len(t, list, 3)
	// Most recent first.
	assert.Equal(t, int64(200), list[0].BackupTime)
	assert.Equal(t, int64(200), list[1].BackupTime)
	assert.Equal(t, int64(100), list[2].BackupTime)
	// Tie-break by BackupID then Datastore.
	assert.Equal(t, "101", list[0].BackupID)
	assert.Equal(t, "ds1", list[0].Datastore)
	assert.Equal(t, "101", list[1].BackupID)
	assert.Equal(t, "ds2", list[1].Datastore)

	assert.Empty(t, AllBackupsSorted(nil))
}

func TestAllBackupsSorted_BackupIDTiebreak(t *testing.T) {
	// Two backups with identical BackupTime but different BackupID — exercises
	// the second sort criterion (BackupID ascending).
	b1 := &model.Backup{BackupID: "200", BackupTime: 500}
	b2 := &model.Backup{BackupID: "101", BackupTime: 500}
	backups := map[string]map[string]*model.Backup{
		"pbs1": {"a": b1, "b": b2},
	}
	list := AllBackupsSorted(backups)
	assert.Len(t, list, 2)
	assert.Equal(t, "101", list[0].BackupID) // lexicographically first
	assert.Equal(t, "200", list[1].BackupID)
}

func TestAllBackupsSorted_DatastoreTiebreak(t *testing.T) {
	// Two backups with identical BackupTime AND BackupID — only the Datastore
	// field differs, so the third sort criterion must be reached.
	b1 := &model.Backup{BackupID: "101", Datastore: "ds-b", BackupTime: 200}
	b2 := &model.Backup{BackupID: "101", Datastore: "ds-a", BackupTime: 200}
	backups := map[string]map[string]*model.Backup{
		"pbs1": {"a": b1, "b": b2},
	}
	list := AllBackupsSorted(backups)
	assert.Len(t, list, 2)
	assert.Equal(t, "ds-a", list[0].Datastore) // lexicographically first
	assert.Equal(t, "ds-b", list[1].Datastore)
}

func TestHeaderSummary(t *testing.T) {
	snap := cache.CacheSnapshot{
		Nodes: map[string]map[string]*model.Node{
			"pve1": {"n1": {}, "n2": {}},
		},
		Guests: map[string]map[int]*model.Guest{
			"pve1": {
				101: {Status: "running"},
				102: {Status: "stopped"},
				103: {Status: "running"},
			},
		},
	}
	assert.Equal(t, "2 nodes · 2 running", HeaderSummary(snap))
}

func TestHeaderSummary_Empty(t *testing.T) {
	assert.Equal(t, "0 nodes · 0 running", HeaderSummary(cache.CacheSnapshot{}))
}

func TestSparklinePolyline(t *testing.T) {
	// Empty input.
	assert.Empty(t, SparklinePolyline(nil, 240, 40))

	// Single point — x should be width/2, y at bottom (min value = bottom in SVG).
	single := []model.SparklinePoint{{Value: 50}}
	out := SparklinePolyline(single, 240, 40)
	assert.Equal(t, "120.0,40.0", out)

	// Flat line (all same value) — should not divide by zero.
	flat := []model.SparklinePoint{{Value: 10}, {Value: 10}}
	out = SparklinePolyline(flat, 240, 40)
	assert.NotEmpty(t, out)

	// Two distinct points — first should be at x=0, last at x=width.
	points := []model.SparklinePoint{{Value: 0}, {Value: 100}}
	out = SparklinePolyline(points, 240, 40)
	assert.Contains(t, out, "0.0,")
	assert.Contains(t, out, "240.0,")
}

func TestLatestBackupTime(t *testing.T) {
	backups := map[string]map[string]*model.Backup{
		"pbs1": {
			"homelab/101": {BackupID: "101", Datastore: "homelab", BackupTime: 2000},
			"offsite/101": {BackupID: "101", Datastore: "offsite", BackupTime: 1000},
		},
	}
	// Returns the most recent timestamp (2000, not 1000)
	assert.Equal(t, int64(2000), LatestBackupTime(backups, 101))

	// No backups for this VMID → 0
	assert.Equal(t, int64(0), LatestBackupTime(backups, 999))

	// Empty map → 0
	assert.Equal(t, int64(0), LatestBackupTime(nil, 101))
}

func TestIntPtrSortValue(t *testing.T) {
	v := 42
	assert.Equal(t, "42", IntPtrSortValue(&v))
	assert.Equal(t, "-1", IntPtrSortValue(nil))
}

func TestTaskDurationSeconds(t *testing.T) {
	start := int64(1000)
	end := int64(1060)
	task := &model.PBSTask{StartTime: start, EndTime: &end}
	assert.Equal(t, int64(60), TaskDurationSeconds(task))

	// Running task (no EndTime) → -1
	running := &model.PBSTask{StartTime: start}
	assert.Equal(t, int64(-1), TaskDurationSeconds(running))
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkFormatBytes(b *testing.B) {
	for b.Loop() {
		FormatBytes(8_500_000_000)
	}
}

func BenchmarkSparklinePolyline(b *testing.B) {
	points := make([]model.SparklinePoint, 100)
	for i := range points {
		points[i] = model.SparklinePoint{Value: float64(i)}
	}
	b.ResetTimer()
	for b.Loop() {
		SparklinePolyline(points, 240, 40)
	}
}

func BenchmarkAllBackupsSorted(b *testing.B) {
	ds := make(map[string]*model.Backup)
	for i := range 100 {
		key := fmt.Sprintf("%d", i+100)
		ds[key] = &model.Backup{BackupID: key, BackupTime: int64(i * 3600)}
	}
	backups := map[string]map[string]*model.Backup{"pbs1": ds}
	b.ResetTimer()
	for b.Loop() {
		AllBackupsSorted(backups)
	}
}

func BenchmarkSortedGuestList(b *testing.B) {
	inner := make(map[int]*model.Guest)
	for i := range 100 {
		inner[i+100] = &model.Guest{VMID: i + 100, Name: fmt.Sprintf("vm-%d", i)}
	}
	guests := map[string]map[int]*model.Guest{"pve1": inner}
	b.ResetTimer()
	for b.Loop() {
		SortedGuestList(guests)
	}
}

// ---------------------------------------------------------------------------
// Fuzz tests
// ---------------------------------------------------------------------------

func FuzzFormatBytes(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1023))
	f.Add(int64(1024))
	f.Add(int64(8_500_000_000))
	f.Add(int64(-1))
	f.Add(int64(1_125_899_906_842_624)) // 1 PB — one unit below the previously-panicking boundary
	f.Add(int64(1_152_921_504_606_846_976)) // 1 EB — previously triggered index-out-of-bounds panic
	f.Add(int64(math.MaxInt64))             // 8 EB — largest valid int64
	f.Fuzz(func(t *testing.T, n int64) {
		out := FormatBytes(n)
		// Must not be empty and must end with a known unit suffix.
		validSuffix := false
		for _, s := range []string{" B", " KB", " MB", " GB", " TB", " PB", " EB"} {
			if strings.HasSuffix(out, s) {
				validSuffix = true
				break
			}
		}
		if !validSuffix {
			t.Errorf("FormatBytes(%d) = %q: missing known unit suffix", n, out)
		}
	})
}

func FuzzBackupIDMatchesVMID(f *testing.F) {
	f.Add("101", "101")
	f.Add("lxc-200", "200")
	f.Add("vm-100", "100")
	f.Add("lxc-1010", "10")
	f.Add("", "0")
	f.Fuzz(func(t *testing.T, id, vmidStr string) {
		// Must not panic
		_ = backupIDMatchesVMID(id, vmidStr)
	})
}
