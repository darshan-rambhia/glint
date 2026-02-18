package store

import (
	"context"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRetention(t *testing.T) {
	r := DefaultRetention()
	assert.Equal(t, 48*time.Hour, r.NodeSnapshots)
	assert.Equal(t, 48*time.Hour, r.GuestSnapshots)
	assert.Equal(t, 30*24*time.Hour, r.SMARTSnapshots)
	assert.Equal(t, 7*24*time.Hour, r.BackupSnapshots)
	assert.Equal(t, 7*24*time.Hour, r.DatastoreSnapshots)
	assert.Equal(t, 30*24*time.Hour, r.AlertLog)
}

func TestNewPruner(t *testing.T) {
	s := newTestStore(t)
	r := DefaultRetention()
	p := NewPruner(s, r)

	assert.NotNil(t, p)
	assert.Equal(t, s, p.store)
	assert.Equal(t, r, p.retention)
	assert.Equal(t, 1*time.Hour, p.interval)
}

func TestPrunerRun_CancelledContext(t *testing.T) {
	s := newTestStore(t)
	p := NewPruner(s, DefaultRetention())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := p.Run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPrune_DeletesOldData(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Unix()
	oldTS := now - int64((49 * time.Hour).Seconds()) // older than 48h retention

	// Insert old node snapshot
	err := s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp: oldTS, Instance: "main", Node: "pve",
		CPUPct: 50, MemUsed: 8e9, MemTotal: 16e9,
		SwapUsed: 0, SwapTotal: 4e9, RootUsed: 20e9, RootTotal: 100e9,
		Load1m: 1, Load5m: 0.8, Load15m: 0.6, IOWait: 0.1, UptimeSecs: 100000,
	})
	require.NoError(t, err)

	// Insert recent node snapshot
	err = s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp: now, Instance: "main", Node: "pve",
		CPUPct: 50, MemUsed: 8e9, MemTotal: 16e9,
		SwapUsed: 0, SwapTotal: 4e9, RootUsed: 20e9, RootTotal: 100e9,
		Load1m: 1, Load5m: 0.8, Load15m: 0.6, IOWait: 0.1, UptimeSecs: 100000,
	})
	require.NoError(t, err)

	// Insert old guest snapshot
	err = s.InsertGuestSnapshot(model.GuestSnapshot{
		Timestamp: oldTS, Instance: "main", VMID: 101, Node: "pve",
		ClusterID: "main", GuestType: "lxc", Name: "test", Status: "running",
		CPUPct: 2, CPUs: 2, MemUsed: 312e6, MemTotal: 2048e6,
		DiskUsed: 1200e6, DiskTotal: 8000e6, NetIn: 1e6, NetOut: 5e5,
	})
	require.NoError(t, err)

	// Insert old alert
	err = s.InsertAlert(oldTS, "test", "main", "pve", "old alert", "info")
	require.NoError(t, err)

	// Run pruner
	retention := DefaultRetention()
	p := NewPruner(s, retention)
	p.prune()

	// Old node snapshot should be deleted, recent one kept
	points, err := s.QueryNodeSparkline("main", "pve", "cpu", 0)
	require.NoError(t, err)
	assert.Len(t, points, 1)
	assert.Equal(t, now, points[0].Timestamp)

	// Old guest snapshot should be deleted
	guestPoints, err := s.QueryGuestSparkline("main", 101, 0)
	require.NoError(t, err)
	assert.Empty(t, guestPoints)
}

func TestPrune_ClosedDB(t *testing.T) {
	s := newTestStore(t)
	p := NewPruner(s, DefaultRetention())
	s.Close()

	// Should not panic when DB is closed; errors are logged but not returned.
	p.prune()
}

func TestPrune_NoRowsDeleted(t *testing.T) {
	s := newTestStore(t)
	p := NewPruner(s, DefaultRetention())

	// Empty tables — prune should complete without error.
	p.prune()
}

func TestPrunerRun_TickerFires(t *testing.T) {
	s := newTestStore(t)
	p := NewPruner(s, DefaultRetention())
	p.interval = 10 * time.Millisecond // fast ticker

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := p.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPrunerRun_PrunesOnStartup(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Unix()
	oldTS := now - int64((49 * time.Hour).Seconds())

	// Insert old data
	err := s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp: oldTS, Instance: "main", Node: "pve",
		CPUPct: 50, MemUsed: 8e9, MemTotal: 16e9,
		SwapUsed: 0, SwapTotal: 4e9, RootUsed: 20e9, RootTotal: 100e9,
		Load1m: 1, Load5m: 0.8, Load15m: 0.6, IOWait: 0.1, UptimeSecs: 100000,
	})
	require.NoError(t, err)

	p := NewPruner(s, DefaultRetention())

	// Run with short-lived context so it prunes once at startup then exits
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = p.Run(ctx)

	// Old data should be pruned
	points, err := s.QueryNodeSparkline("main", "pve", "cpu", 0)
	require.NoError(t, err)
	assert.Empty(t, points)
}
