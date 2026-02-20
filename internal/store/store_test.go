package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t testing.TB) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew(t *testing.T) {
	s := newTestStore(t)
	assert.NotNil(t, s)
}

func TestNew_InvalidPath(t *testing.T) {
	_, err := New("/nonexistent/dir/test.db")
	assert.Error(t, err)
}

func TestInsertNodeSnapshot(t *testing.T) {
	s := newTestStore(t)

	snap := model.NodeSnapshot{
		Timestamp:  time.Now().Unix(),
		Instance:   "main",
		Node:       "pve",
		CPUPct:     42.5,
		MemUsed:    8_000_000_000,
		MemTotal:   16_000_000_000,
		SwapUsed:   100_000_000,
		SwapTotal:  4_000_000_000,
		RootUsed:   20_000_000_000,
		RootTotal:  100_000_000_000,
		Load1m:     1.2,
		Load5m:     0.8,
		Load15m:    0.6,
		IOWait:     0.1,
		UptimeSecs: 4_000_000,
	}

	err := s.InsertNodeSnapshot(snap)
	assert.NoError(t, err)
}

func TestInsertGuestSnapshot(t *testing.T) {
	s := newTestStore(t)

	snap := model.GuestSnapshot{
		Timestamp: time.Now().Unix(),
		Instance:  "main",
		VMID:      101,
		Node:      "pve",
		ClusterID: "main",
		GuestType: "lxc",
		Name:      "network-services",
		Status:    "running",
		CPUPct:    2.0,
		CPUs:      2,
		MemUsed:   312_000_000,
		MemTotal:  2_048_000_000,
		DiskUsed:  1_200_000_000,
		DiskTotal: 8_000_000_000,
		NetIn:     1_000_000,
		NetOut:    500_000,
	}

	err := s.InsertGuestSnapshot(snap)
	assert.NoError(t, err)
}

func TestInsertSMARTSnapshot(t *testing.T) {
	s := newTestStore(t)

	disk := &model.Disk{
		WWN:    "0x5000c500dc4e3541",
		Health: "PASSED",
		Status: model.StatusPassed,
		Attributes: []model.SMARTAttribute{
			{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, RawValue: 0},
		},
	}

	err := s.InsertSMARTSnapshot(time.Now().Unix(), disk)
	assert.NoError(t, err)
}

func TestQueryNodeSparkline(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Unix()

	// Insert a few snapshots
	for i := range 5 {
		err := s.InsertNodeSnapshot(model.NodeSnapshot{
			Timestamp:  now - int64((4-i)*60),
			Instance:   "main",
			Node:       "pve",
			CPUPct:     float64(20 + i*10),
			MemUsed:    8_000_000_000,
			MemTotal:   16_000_000_000,
			SwapUsed:   0,
			SwapTotal:  4_000_000_000,
			RootUsed:   20_000_000_000,
			RootTotal:  100_000_000_000,
			Load1m:     1.0,
			Load5m:     0.8,
			Load15m:    0.6,
			IOWait:     0.1,
			UptimeSecs: 100000,
		})
		require.NoError(t, err)
	}

	points, err := s.QueryNodeSparkline("main", "pve", "cpu", now-300)
	require.NoError(t, err)
	assert.Len(t, points, 5)
	assert.Equal(t, float64(20), points[0].Value)
	assert.Equal(t, float64(60), points[4].Value)
}

func TestQueryNodeSparkline_InvalidMetric(t *testing.T) {
	s := newTestStore(t)
	_, err := s.QueryNodeSparkline("main", "pve", "invalid", 0)
	assert.Error(t, err)
}

func TestUpsertDisk(t *testing.T) {
	s := newTestStore(t)

	disk := &model.Disk{
		WWN:       "0x5000c500dc4e3541",
		Instance:  "main",
		Node:      "pve",
		DevPath:   "/dev/sda",
		Model:     "WD Red 4TB",
		Serial:    "WD-ABC123",
		DiskType:  "hdd",
		Protocol:  "ata",
		SizeBytes: 4_000_000_000_000,
	}

	err := s.UpsertDisk(disk)
	assert.NoError(t, err)

	// Upsert again with different dev path
	disk.DevPath = "/dev/sdb"
	err = s.UpsertDisk(disk)
	assert.NoError(t, err)
}

func TestUpsertPVEInstance(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertPVEInstance("main", "https://192.168.1.215:8006", false, "")
	assert.NoError(t, err)
}

func TestUpsertPBSInstance(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertPBSInstance("main-pbs", "https://10.100.1.102:8007")
	assert.NoError(t, err)
}

func TestInsertBackupSnapshot(t *testing.T) {
	s := newTestStore(t)

	size := int64(4_200_000_000)
	verified := true
	b := &model.Backup{
		PBSInstance: "main-pbs",
		Datastore:   "homelab",
		BackupType:  "ct",
		BackupID:    "304",
		BackupTime:  time.Now().Unix(),
		SizeBytes:   &size,
		Verified:    &verified,
	}

	err := s.InsertBackupSnapshot(time.Now().Unix(), b)
	assert.NoError(t, err)
}

func TestInsertAlert(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertAlert(time.Now().Unix(), "node_cpu_high", "main", "pve", "CPU at 95%", "warning")
	assert.NoError(t, err)
}

func TestInsertDatastoreSnapshot(t *testing.T) {
	s := newTestStore(t)

	total := int64(1_000_000_000_000)
	used := int64(400_000_000_000)
	avail := int64(600_000_000_000)
	dedup := 1.5
	estFull := int64(1800000000)

	ds := &model.DatastoreStatus{
		PBSInstance: "main-pbs",
		Name:        "homelab",
		TotalBytes:  &total,
		UsedBytes:   &used,
		AvailBytes:  &avail,
		DedupRatio:  &dedup,
		EstFullDate: &estFull,
	}

	err := s.InsertDatastoreSnapshot(time.Now().Unix(), ds)
	assert.NoError(t, err)
}

func TestQueryGuestSparkline(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Unix()

	// Insert guest snapshots
	for i := range 5 {
		err := s.InsertGuestSnapshot(model.GuestSnapshot{
			Timestamp: now - int64((4-i)*60),
			Instance:  "main",
			VMID:      101,
			Node:      "pve",
			ClusterID: "main",
			GuestType: "lxc",
			Name:      "network-services",
			Status:    "running",
			CPUPct:    float64(10 + i*5),
			CPUs:      2,
			MemUsed:   312_000_000,
			MemTotal:  2_048_000_000,
			DiskUsed:  1_200_000_000,
			DiskTotal: 8_000_000_000,
			NetIn:     1_000_000,
			NetOut:    500_000,
		})
		require.NoError(t, err)
	}

	points, err := s.QueryGuestSparkline("main", 101, now-300)
	require.NoError(t, err)
	assert.Len(t, points, 5)
	assert.Equal(t, float64(10), points[0].Value)
	assert.Equal(t, float64(30), points[4].Value)
}

func TestQueryGuestSparkline_Empty(t *testing.T) {
	s := newTestStore(t)
	points, err := s.QueryGuestSparkline("main", 999, 0)
	require.NoError(t, err)
	assert.Empty(t, points)
}

func TestInsertBackupSnapshot_NilVerified(t *testing.T) {
	s := newTestStore(t)

	size := int64(3_000_000_000)
	b := &model.Backup{
		PBSInstance: "main-pbs",
		Datastore:   "homelab",
		BackupType:  "ct",
		BackupID:    "101",
		BackupTime:  time.Now().Unix(),
		SizeBytes:   &size,
		Verified:    nil,
	}

	err := s.InsertBackupSnapshot(time.Now().Unix(), b)
	assert.NoError(t, err)
}

func TestInsertSMARTSnapshot_NilAttributes(t *testing.T) {
	s := newTestStore(t)

	disk := &model.Disk{
		WWN:        "0x5000c500dc4e9999",
		Health:     "PASSED",
		Status:     model.StatusPassed,
		Attributes: nil,
	}

	err := s.InsertSMARTSnapshot(time.Now().Unix(), disk)
	assert.NoError(t, err)
}

func TestQueryNodeSparkline_Memory(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Unix()

	err := s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp:  now,
		Instance:   "main",
		Node:       "pve",
		CPUPct:     20,
		MemUsed:    8_000_000_000,
		MemTotal:   16_000_000_000,
		SwapUsed:   0,
		SwapTotal:  4_000_000_000,
		RootUsed:   20_000_000_000,
		RootTotal:  100_000_000_000,
		Load1m:     1.0,
		Load5m:     0.8,
		Load15m:    0.6,
		IOWait:     0.1,
		UptimeSecs: 100000,
	})
	require.NoError(t, err)

	points, err := s.QueryNodeSparkline("main", "pve", "memory", now-60)
	require.NoError(t, err)
	assert.Len(t, points, 1)
	assert.InDelta(t, 50.0, points[0].Value, 0.1)
}

// ---------------------------------------------------------------------------
// Error paths: closed DB triggers all error returns
// ---------------------------------------------------------------------------

func closedTestStore(t testing.TB) *Store {
	t.Helper()
	s := newTestStore(t)
	s.Close()
	return s
}

func TestInsertNodeSnapshot_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	err := s.InsertNodeSnapshot(model.NodeSnapshot{Timestamp: 1, Instance: "a", Node: "b"})
	assert.Error(t, err)
}

func TestInsertGuestSnapshot_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	err := s.InsertGuestSnapshot(model.GuestSnapshot{Timestamp: 1, Instance: "a", VMID: 1, Node: "b", GuestType: "lxc", Name: "c", Status: "running"})
	assert.Error(t, err)
}

func TestInsertSMARTSnapshot_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	disk := &model.Disk{WWN: "w", Health: "PASSED", Status: model.StatusPassed}
	err := s.InsertSMARTSnapshot(1, disk)
	assert.Error(t, err)
}

func TestInsertBackupSnapshot_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	b := &model.Backup{PBSInstance: "p", Datastore: "d", BackupType: "ct", BackupID: "1", BackupTime: 1}
	err := s.InsertBackupSnapshot(1, b)
	assert.Error(t, err)
}

func TestInsertDatastoreSnapshot_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	ds := &model.DatastoreStatus{PBSInstance: "p", Name: "d"}
	err := s.InsertDatastoreSnapshot(1, ds)
	assert.Error(t, err)
}

func TestInsertAlert_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	err := s.InsertAlert(1, "test", "inst", "subj", "msg", "warning")
	assert.Error(t, err)
}

func TestUpsertDisk_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	d := &model.Disk{WWN: "w", Instance: "i", Node: "n", DiskType: "ssd", Protocol: "ata", SizeBytes: 100}
	err := s.UpsertDisk(d)
	assert.Error(t, err)
}

func TestQueryNodeSparkline_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	_, err := s.QueryNodeSparkline("a", "b", "cpu", 0)
	assert.Error(t, err)
}

func TestQueryGuestSparkline_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	_, err := s.QueryGuestSparkline("a", 1, 0)
	assert.Error(t, err)
}

func TestUpsertPVEInstance_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	err := s.UpsertPVEInstance("a", "http://host", false, "")
	assert.Error(t, err)
}

func TestUpsertPBSInstance_ClosedDB(t *testing.T) {
	s := closedTestStore(t)
	err := s.UpsertPBSInstance("a", "http://host")
	assert.Error(t, err)
}

func TestInsertBackupSnapshot_VerifiedFalse(t *testing.T) {
	s := newTestStore(t)
	v := false
	size := int64(100)
	b := &model.Backup{
		PBSInstance: "pbs", Datastore: "d", BackupType: "ct",
		BackupID: "1", BackupTime: time.Now().Unix(),
		SizeBytes: &size, Verified: &v,
	}
	err := s.InsertBackupSnapshot(time.Now().Unix(), b)
	assert.NoError(t, err)
}

func TestUpsertPVEInstance_Cluster(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertPVEInstance("main", "https://192.168.1.215:8006", true, "my-cluster")
	assert.NoError(t, err)

	// Upsert again to cover the ON CONFLICT path.
	err = s.UpsertPVEInstance("main", "https://192.168.1.216:8006", true, "my-cluster")
	assert.NoError(t, err)
}

func TestUpsertPBSInstance_Upsert(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertPBSInstance("pbs1", "https://10.100.1.102:8007")
	assert.NoError(t, err)

	// Upsert again to cover ON CONFLICT path.
	err = s.UpsertPBSInstance("pbs1", "https://10.100.1.103:8007")
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkQueryNodeSparkline(b *testing.B) {
	s := newTestStore(b)
	now := time.Now().Unix()
	for i := range 50 {
		_ = s.InsertNodeSnapshot(model.NodeSnapshot{
			Timestamp:  now - int64((50-i)*60),
			Instance:   "main",
			Node:       "pve",
			CPUPct:     float64(20 + (i % 80)),
			MemUsed:    8_000_000_000,
			MemTotal:   16_000_000_000,
			SwapUsed:   0,
			SwapTotal:  4_000_000_000,
			RootUsed:   20_000_000_000,
			RootTotal:  100_000_000_000,
			Load1m:     1.0,
			Load5m:     0.8,
			Load15m:    0.6,
			IOWait:     0.1,
			UptimeSecs: 100000,
		})
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.QueryNodeSparkline("main", "pve", "cpu", now-3600)
	}
}

func BenchmarkQueryGuestSparkline(b *testing.B) {
	s := newTestStore(b)
	now := time.Now().Unix()
	for i := range 50 {
		_ = s.InsertGuestSnapshot(model.GuestSnapshot{
			Timestamp: now - int64((50-i)*60),
			Instance:  "main",
			VMID:      101,
			Node:      "pve",
			ClusterID: "main",
			GuestType: "lxc",
			Name:      "test",
			Status:    "running",
			CPUPct:    float64(5 + (i % 30)),
			CPUs:      2,
			MemUsed:   312_000_000,
			MemTotal:  2_048_000_000,
			DiskUsed:  1_200_000_000,
			DiskTotal: 8_000_000_000,
			NetIn:     1_000_000,
			NetOut:    500_000,
		})
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = s.QueryGuestSparkline("main", 101, now-3600)
	}
}

func TestNew_WritesToDisk(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := New(dbPath)
	require.NoError(t, err)
	s.Close()

	_, err = os.Stat(dbPath)
	assert.NoError(t, err)
}
