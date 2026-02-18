package templates

import (
	"testing"
	"time"

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
	assert.Equal(t, "status-ok", DiskStatusClass(model.StatusPassed))
	assert.Equal(t, "status-critical", DiskStatusClass(model.StatusFailedSmart))
	assert.Equal(t, "status-critical", DiskStatusClass(model.StatusFailedScrutiny))
	assert.Equal(t, "status-warning", DiskStatusClass(model.StatusWarnScrutiny))
	assert.Equal(t, "status-unknown", DiskStatusClass(model.StatusUnknown))
	assert.Equal(t, "status-error", DiskStatusClass(model.StatusInternalError))
	// Combined flags
	assert.Equal(t, "status-critical", DiskStatusClass(model.StatusFailedSmart|model.StatusWarnScrutiny))
}

func TestGuestStatusClass(t *testing.T) {
	assert.Equal(t, "status-ok", GuestStatusClass("running"))
	assert.Equal(t, "status-critical", GuestStatusClass("stopped"))
	assert.Equal(t, "status-warning", GuestStatusClass("paused"))
}

func TestBackupStatusLabel(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "OK", BackupStatusLabel(now.Add(-10*time.Hour).Unix(), 36))
	assert.Equal(t, "STALE", BackupStatusLabel(now.Add(-48*time.Hour).Unix(), 36))
}

func TestBackupStatusClass(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "status-ok", BackupStatusClass(now.Add(-10*time.Hour).Unix(), 36))
	assert.Equal(t, "status-warning", BackupStatusClass(now.Add(-48*time.Hour).Unix(), 36))
}

func TestProgressBarWidth(t *testing.T) {
	assert.Equal(t, 50.0, ProgressBarWidth(50))
	assert.Equal(t, 100.0, ProgressBarWidth(120))
	assert.Equal(t, 0.0, ProgressBarWidth(-5))
}

func TestProgressBarClass(t *testing.T) {
	assert.Equal(t, "bar-ok", ProgressBarClass(50))
	assert.Equal(t, "bar-warning", ProgressBarClass(80))
	assert.Equal(t, "bar-critical", ProgressBarClass(95))
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
	assert.Equal(t, "status-ok", TaskStatusClass("OK"))
	assert.Equal(t, "status-ok", TaskStatusClass(""))
	assert.Equal(t, "status-critical", TaskStatusClass("Error: something broke"))
	assert.Equal(t, "status-warning", TaskStatusClass("WARNINGS: disk almost full"))
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
