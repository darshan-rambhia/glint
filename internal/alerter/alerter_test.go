package alerter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/notify"
	"github.com/darshan-rambhia/glint/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProvider records notifications for assertions.
type testProvider struct {
	sent []model.Notification
}

func (p *testProvider) Name() string { return "test" }
func (p *testProvider) Send(_ context.Context, n model.Notification) error {
	p.sent = append(p.sent, n)
	return nil
}

// Compile-time check that testProvider satisfies notify.Provider.
var _ notify.Provider = (*testProvider)(nil)

// newTestStore creates a SQLite store in a temp directory for testing.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

// newTestAlerter creates an Alerter wired with a test provider and temp store.
func newTestAlerter(t *testing.T, c *cache.Cache, cfg AlertConfig) (*Alerter, *testProvider) {
	t.Helper()
	s := newTestStore(t)
	p := &testProvider{}
	a := NewAlerter(c, s, []notify.Provider{p}, cfg)
	return a, p
}

func TestDefaultAlertConfig(t *testing.T) {
	cfg := DefaultAlertConfig()

	assert.NotNil(t, cfg.NodeCPUHigh)
	assert.NotNil(t, cfg.NodeMemHigh)
	assert.NotNil(t, cfg.GuestDown)
	assert.NotNil(t, cfg.BackupStale)
	assert.NotNil(t, cfg.DiskSmartFailed)
	assert.NotNil(t, cfg.DatastoreFull)

	assert.Equal(t, float64(90), cfg.NodeCPUHigh.Threshold)
	assert.Equal(t, 5*time.Minute, cfg.NodeCPUHigh.Duration)
	assert.Equal(t, "warning", cfg.NodeCPUHigh.Severity)
	assert.Equal(t, 1*time.Hour, cfg.NodeCPUHigh.Cooldown)

	assert.Equal(t, float64(90), cfg.NodeMemHigh.Threshold)
	assert.Equal(t, 2*time.Minute, cfg.GuestDown.GracePeriod)
	assert.Equal(t, "critical", cfg.GuestDown.Severity)
	assert.Equal(t, 36*time.Hour, cfg.BackupStale.MaxAge)
	assert.Equal(t, "critical", cfg.DiskSmartFailed.Severity)
	assert.Equal(t, float64(85), cfg.DatastoreFull.Threshold)
}

func TestNewAlerter(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	s := newTestStore(t)
	p := &testProvider{}

	a := NewAlerter(c, s, []notify.Provider{p}, cfg)

	assert.NotNil(t, a)
	assert.Equal(t, c, a.cache)
	assert.Equal(t, s, a.store)
	assert.Len(t, a.providers, 1)
	assert.Equal(t, cfg, a.config)
	assert.Equal(t, 30*time.Second, a.interval)
	assert.NotNil(t, a.lastFired)
	assert.NotNil(t, a.sustained)
}

func TestFormatSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"warning", "WARNING"},
		{"critical", "CRITICAL"},
		{"info", "INFO"},
		{"", ""},
		{"Warning", "WARNING"},
		{"CRITICAL", "CRITICAL"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatSeverity(tt.input))
		})
	}
}

func TestEvaluate_NodeCPUHigh(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	// Use short duration for testing.
	cfg.NodeCPUHigh.Duration = 0
	cfg.NodeCPUHigh.Cooldown = 1 * time.Hour

	a, p := newTestAlerter(t, c, cfg)

	// Set node with CPU at 95% (0.95 fraction).
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", CPU: 0.95},
	})

	// First evaluate seeds sustained tracker, no alert yet (duration=0 but first call seeds).
	a.evaluate(context.Background())
	assert.Empty(t, p.sent, "first call should only seed sustained tracker")

	// Second evaluate should fire since duration=0 and sustained is already seeded.
	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "node_cpu_high", p.sent[0].AlertType)
	assert.Equal(t, "warning", p.sent[0].Severity)
	assert.Contains(t, p.sent[0].Message, "CPU at 95%")
}

func TestEvaluate_NodeCPUClears(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.NodeCPUHigh.Duration = 0

	a, p := newTestAlerter(t, c, cfg)

	// Seed with high CPU.
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", CPU: 0.95},
	})
	a.evaluate(context.Background())

	// Drop CPU below threshold.
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", CPU: 0.50},
	})
	a.evaluate(context.Background())

	// Raise again -- should need to re-seed sustained.
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", CPU: 0.95},
	})
	a.evaluate(context.Background())
	assert.Empty(t, p.sent, "sustained tracker should have been cleared; re-seeding required")
}

func TestEvaluate_NodeMemHigh(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.NodeMemHigh.Duration = 0
	cfg.NodeMemHigh.Cooldown = 1 * time.Hour

	a, p := newTestAlerter(t, c, cfg)

	// Memory at 95%: 950 used / 1000 total.
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", Memory: model.MemUsage{Used: 950, Total: 1000}},
	})

	a.evaluate(context.Background()) // seed
	a.evaluate(context.Background()) // fire
	require.Len(t, p.sent, 1)
	assert.Equal(t, "node_mem_high", p.sent[0].AlertType)
	assert.Contains(t, p.sent[0].Message, "Memory at 95%")
}

func TestEvaluate_NodeMemSkipsZeroTotal(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.NodeMemHigh.Duration = 0

	a, p := newTestAlerter(t, c, cfg)

	// Total=0 should not trigger (avoids division by zero).
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", Memory: model.MemUsage{Used: 950, Total: 0}},
	})

	a.evaluate(context.Background())
	a.evaluate(context.Background())
	assert.Empty(t, p.sent)
}

func TestEvaluate_GuestDown(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.GuestDown.GracePeriod = 0

	a, p := newTestAlerter(t, c, cfg)

	c.UpdateGuests("cluster1", map[int]*model.Guest{
		100: {Instance: "pve1", ClusterID: "cluster1", VMID: 100, Name: "myguest", Status: "stopped"},
	})

	// First eval seeds sustained.
	a.evaluate(context.Background())
	assert.Empty(t, p.sent)

	// Second eval fires (grace period=0 and sustained is set).
	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "guest_down", p.sent[0].AlertType)
	assert.Equal(t, "critical", p.sent[0].Severity)
	assert.Contains(t, p.sent[0].Message, "myguest")
	assert.Contains(t, p.sent[0].Message, "stopped")
}

func TestEvaluate_GuestRunning(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	c.UpdateGuests("cluster1", map[int]*model.Guest{
		100: {Instance: "pve1", ClusterID: "cluster1", VMID: 100, Name: "myguest", Status: "running"},
	})

	a.evaluate(context.Background())
	a.evaluate(context.Background())
	assert.Empty(t, p.sent, "running guest should not trigger alert")
}

func TestEvaluate_GuestRecovery(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.GuestDown.GracePeriod = 0

	a, p := newTestAlerter(t, c, cfg)

	// Guest is stopped.
	c.UpdateGuests("cluster1", map[int]*model.Guest{
		100: {Instance: "pve1", ClusterID: "cluster1", VMID: 100, Name: "myguest", Status: "stopped"},
	})
	a.evaluate(context.Background()) // seed

	// Guest recovers before second eval.
	c.UpdateGuests("cluster1", map[int]*model.Guest{
		100: {Instance: "pve1", ClusterID: "cluster1", VMID: 100, Name: "myguest", Status: "running"},
	})
	a.evaluate(context.Background())
	assert.Empty(t, p.sent, "recovered guest should not fire alert")
}

func TestEvaluate_BackupStale(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.BackupStale.MaxAge = 1 * time.Hour
	cfg.BackupStale.Cooldown = 1 * time.Hour

	a, p := newTestAlerter(t, c, cfg)

	// Backup from 2 hours ago.
	c.UpdateBackups("pbs1", map[string]*model.Backup{
		"ct/100": {
			PBSInstance: "pbs1",
			BackupType:  "ct",
			BackupID:    "100",
			BackupTime:  time.Now().Add(-2 * time.Hour).Unix(),
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "backup_stale", p.sent[0].AlertType)
	assert.Equal(t, "warning", p.sent[0].Severity)
}

func TestEvaluate_BackupFresh(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.BackupStale.MaxAge = 1 * time.Hour

	a, p := newTestAlerter(t, c, cfg)

	// Backup from 10 minutes ago -- fresh.
	c.UpdateBackups("pbs1", map[string]*model.Backup{
		"ct/100": {
			PBSInstance: "pbs1",
			BackupType:  "ct",
			BackupID:    "100",
			BackupTime:  time.Now().Add(-10 * time.Minute).Unix(),
		},
	})

	a.evaluate(context.Background())
	assert.Empty(t, p.sent)
}

func TestEvaluate_DiskSmartFailed(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	c.UpdateDisks(map[string]*model.Disk{
		"wwn-123": {
			Instance: "pve1", Node: "node1", WWN: "wwn-123",
			DevPath: "/dev/sda", Model: "WDC", Health: "FAILED",
			Status: model.StatusFailedSmart,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "disk_smart_failed", p.sent[0].AlertType)
	assert.Equal(t, "critical", p.sent[0].Severity)
	assert.Contains(t, p.sent[0].Message, "SMART health: FAILED")
}

func TestEvaluate_DiskSmartFailedByStatusOnly(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	// Health is "PASSED" but status bitfield has StatusFailedSmart.
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-456": {
			Instance: "pve1", Node: "node1", WWN: "wwn-456",
			DevPath: "/dev/sdb", Model: "Samsung", Health: "PASSED",
			Status: model.StatusFailedSmart,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "disk_smart_failed", p.sent[0].AlertType)
}

func TestEvaluate_DiskScrutinyWarning(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	c.UpdateDisks(map[string]*model.Disk{
		"wwn-789": {
			Instance: "pve1", Node: "node1", WWN: "wwn-789",
			DevPath: "/dev/sdc", Model: "Seagate", Health: "PASSED",
			Status: model.StatusWarnScrutiny,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "disk_scrutiny_warning", p.sent[0].AlertType)
	assert.Equal(t, "warning", p.sent[0].Severity)
}

func TestEvaluate_DiskScrutinyFailed(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	c.UpdateDisks(map[string]*model.Disk{
		"wwn-abc": {
			Instance: "pve1", Node: "node1", WWN: "wwn-abc",
			DevPath: "/dev/sdd", Model: "Toshiba", Health: "PASSED",
			Status: model.StatusFailedScrutiny,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "disk_scrutiny_warning", p.sent[0].AlertType)
}

func TestEvaluate_DiskSmartAndScrutiny(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	// Both SMART failed and scrutiny warning -- should fire two alerts.
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-dual": {
			Instance: "pve1", Node: "node1", WWN: "wwn-dual",
			DevPath: "/dev/sde", Model: "Mixed", Health: "FAILED",
			Status: model.StatusFailedSmart | model.StatusWarnScrutiny,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 2)

	types := map[string]bool{}
	for _, n := range p.sent {
		types[n.AlertType] = true
	}
	assert.True(t, types["disk_smart_failed"])
	assert.True(t, types["disk_scrutiny_warning"])
}

func TestEvaluate_DatastoreFull(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.DatastoreFull.Threshold = 85

	a, p := newTestAlerter(t, c, cfg)

	total := int64(1000)
	used := int64(900) // 90%
	c.UpdateDatastores("pbs1", map[string]*model.DatastoreStatus{
		"ds1": {
			PBSInstance: "pbs1", Name: "ds1",
			TotalBytes: &total, UsedBytes: &used,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "datastore_full", p.sent[0].AlertType)
	assert.Equal(t, "warning", p.sent[0].Severity)
	assert.Contains(t, p.sent[0].Message, "90%")
}

func TestEvaluate_DatastoreBelowThreshold(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	cfg.DatastoreFull.Threshold = 85

	a, p := newTestAlerter(t, c, cfg)

	total := int64(1000)
	used := int64(500) // 50%
	c.UpdateDatastores("pbs1", map[string]*model.DatastoreStatus{
		"ds1": {
			PBSInstance: "pbs1", Name: "ds1",
			TotalBytes: &total, UsedBytes: &used,
		},
	})

	a.evaluate(context.Background())
	assert.Empty(t, p.sent)
}

func TestEvaluate_DatastoreOffline(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	errMsg := "I/O error"
	c.UpdateDatastores("pbs1", map[string]*model.DatastoreStatus{
		"ds1": {
			PBSInstance: "pbs1", Name: "ds1",
			Error: &errMsg,
		},
	})

	a.evaluate(context.Background())
	require.Len(t, p.sent, 1)
	assert.Equal(t, "datastore_offline", p.sent[0].AlertType)
	assert.Equal(t, "critical", p.sent[0].Severity)
	assert.Contains(t, p.sent[0].Message, "I/O error")
}

func TestEvaluate_DatastoreNilBytes(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	// Nil TotalBytes/UsedBytes should not panic or fire.
	c.UpdateDatastores("pbs1", map[string]*model.DatastoreStatus{
		"ds1": {PBSInstance: "pbs1", Name: "ds1"},
	})

	a.evaluate(context.Background())
	assert.Empty(t, p.sent)
}

func TestCheckSustainedThreshold_SeededThenFires(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()

	a, p := newTestAlerter(t, c, cfg)

	now := time.Now()
	key := "test_sustained"
	threshold := &ThresholdAlert{
		Threshold: 80, Duration: 1 * time.Minute, Severity: "warning", Cooldown: 1 * time.Hour,
	}
	notif := model.Notification{
		AlertType: "test", Severity: "warning", Title: "test", Message: "test",
		Instance: "i", Subject: "s", Timestamp: now,
	}

	// First call seeds sustained tracker.
	a.checkSustainedThreshold(context.Background(), now, key, 85, threshold, notif)
	assert.Empty(t, p.sent)
	assert.Contains(t, a.sustained, key)

	// Call within duration -- should not fire.
	a.checkSustainedThreshold(context.Background(), now.Add(30*time.Second), key, 85, threshold, notif)
	assert.Empty(t, p.sent)

	// Call after duration -- should fire.
	a.checkSustainedThreshold(context.Background(), now.Add(2*time.Minute), key, 85, threshold, notif)
	require.Len(t, p.sent, 1)
	assert.Equal(t, "test", p.sent[0].AlertType)
}

func TestCheckSustainedThreshold_Clears(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	a, _ := newTestAlerter(t, c, cfg)

	now := time.Now()
	key := "test_clear"
	threshold := &ThresholdAlert{Threshold: 80, Duration: 1 * time.Minute, Severity: "warning", Cooldown: 1 * time.Hour}
	notif := model.Notification{AlertType: "test", Timestamp: now}

	// Seed.
	a.checkSustainedThreshold(context.Background(), now, key, 85, threshold, notif)
	assert.Contains(t, a.sustained, key)

	// Drop below threshold.
	a.checkSustainedThreshold(context.Background(), now.Add(10*time.Second), key, 70, threshold, notif)
	assert.NotContains(t, a.sustained, key)
}

func TestFire_Deduplication(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	a, p := newTestAlerter(t, c, cfg)

	now := time.Now()
	cooldown := 1 * time.Hour
	key := "dedup_test"
	notif := model.Notification{
		AlertType: "test", Severity: "warning", Title: "test", Message: "test msg",
		Instance: "i", Subject: "s", Timestamp: now,
	}

	// First fire should go through.
	a.fire(context.Background(), now, key, cooldown, notif)
	require.Len(t, p.sent, 1)

	// Second fire within cooldown should be suppressed.
	a.fire(context.Background(), now.Add(30*time.Minute), key, cooldown, notif)
	assert.Len(t, p.sent, 1, "second fire within cooldown should be suppressed")

	// Third fire after cooldown expires should go through.
	a.fire(context.Background(), now.Add(2*time.Hour), key, cooldown, notif)
	assert.Len(t, p.sent, 2, "fire after cooldown should succeed")
}

func TestFire_LogsToStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "alert_store.db")
	s, err := store.New(dbPath)
	require.NoError(t, err)
	defer s.Close()

	c := cache.New()
	p := &testProvider{}
	cfg := DefaultAlertConfig()
	a := NewAlerter(c, s, []notify.Provider{p}, cfg)

	now := time.Now()
	notif := model.Notification{
		AlertType: "test_store", Severity: "critical", Title: "Store Test",
		Message: "testing store", Instance: "inst1", Subject: "subj1", Timestamp: now,
	}

	a.fire(context.Background(), now, "store_key", 1*time.Hour, notif)

	// Verify provider received the notification.
	require.Len(t, p.sent, 1)

	// Verify alert was logged to the database by checking the file was written.
	info, err := os.Stat(dbPath)
	require.NoError(t, err)
	assert.Positive(t, info.Size())
}

func TestFire_MultipleProviders(t *testing.T) {
	c := cache.New()
	s := newTestStore(t)
	p1 := &testProvider{}
	p2 := &testProvider{}
	cfg := DefaultAlertConfig()

	a := NewAlerter(c, s, []notify.Provider{p1, p2}, cfg)

	now := time.Now()
	notif := model.Notification{
		AlertType: "multi", Severity: "warning", Title: "Multi",
		Message: "multi provider test", Instance: "i", Subject: "s", Timestamp: now,
	}

	a.fire(context.Background(), now, "multi_key", 1*time.Hour, notif)

	assert.Len(t, p1.sent, 1)
	assert.Len(t, p2.sent, 1)
}

func TestEvaluate_NilConfigFields(t *testing.T) {
	c := cache.New()
	// Config with all nil alert types.
	cfg := AlertConfig{}

	a, p := newTestAlerter(t, c, cfg)

	// Populate cache with everything.
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Instance: "pve1", Name: "node1", CPU: 0.99, Memory: model.MemUsage{Used: 999, Total: 1000}},
	})
	c.UpdateGuests("cluster1", map[int]*model.Guest{
		100: {Instance: "pve1", ClusterID: "cluster1", VMID: 100, Name: "g", Status: "stopped"},
	})
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-x": {Health: "FAILED", Status: model.StatusFailedSmart | model.StatusWarnScrutiny},
	})
	total := int64(100)
	used := int64(99)
	errMsg := "offline"
	c.UpdateDatastores("pbs1", map[string]*model.DatastoreStatus{
		"ds1": {Name: "ds1", TotalBytes: &total, UsedBytes: &used, Error: &errMsg},
	})
	c.UpdateBackups("pbs1", map[string]*model.Backup{
		"ct/100": {BackupType: "ct", BackupID: "100", BackupTime: 0},
	})

	// Should not panic or fire any alerts.
	a.evaluate(context.Background())
	a.evaluate(context.Background())
	assert.Empty(t, p.sent)
}

// failingProvider simulates a provider that returns errors.
type failingProvider struct{}

func (p *failingProvider) Name() string { return "failing" }
func (p *failingProvider) Send(_ context.Context, _ model.Notification) error {
	return fmt.Errorf("provider unavailable")
}

var _ notify.Provider = (*failingProvider)(nil)

func TestFire_ProviderError(t *testing.T) {
	c := cache.New()
	s := newTestStore(t)
	fp := &failingProvider{}
	cfg := DefaultAlertConfig()
	a := NewAlerter(c, s, []notify.Provider{fp}, cfg)

	now := time.Now()
	notif := model.Notification{
		AlertType: "test_fail", Severity: "warning", Title: "Fail",
		Message: "test provider error", Instance: "i", Subject: "s", Timestamp: now,
	}

	// Should not panic even when provider returns error.
	a.fire(context.Background(), now, "fail_key", 1*time.Hour, notif)
	// Alert was still logged to store (store doesn't fail).
}

func TestFire_StoreError(t *testing.T) {
	c := cache.New()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "alerter_closed.db"))
	require.NoError(t, err)
	s.Close() // close to trigger store error

	p := &testProvider{}
	cfg := DefaultAlertConfig()
	a := NewAlerter(c, s, []notify.Provider{p}, cfg)

	now := time.Now()
	notif := model.Notification{
		AlertType: "test_store_err", Severity: "warning", Title: "StoreErr",
		Message: "test store error", Instance: "i", Subject: "s", Timestamp: now,
	}

	// Should not panic even when store insert fails.
	a.fire(context.Background(), now, "store_err_key", 1*time.Hour, notif)
	// Provider still received the notification.
	require.Len(t, p.sent, 1)
}

func TestRun_CancelsCleanly(t *testing.T) {
	c := cache.New()
	cfg := DefaultAlertConfig()
	a, _ := newTestAlerter(t, c, cfg)
	a.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx)
	}()

	// Let it tick a few times.
	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	assert.ErrorIs(t, err, context.Canceled)
}
