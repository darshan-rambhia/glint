package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/store"
	"github.com/darshan-rambhia/glint/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failWriter is a ResponseWriter whose Write always returns an error.
// Used to exercise the "client disconnected" debug-log paths in renderHTML / writeJSON.
type failWriter struct {
	header http.Header
}

func (fw *failWriter) Header() http.Header         { return fw.header }
func (fw *failWriter) WriteHeader(int)              {}
func (fw *failWriter) Write([]byte) (int, error)   { return 0, errors.New("write failed") }

func newTestServer(t *testing.T) (*Server, *cache.Cache, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	c := cache.New()
	srv := NewServer(":0", c, s)
	return srv, c, s
}

func populateCache(c *cache.Cache) {
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {
			Instance: "pve1",
			Name:     "node1",
			Status:   "online",
			CPU:      0.45,
			CPUInfo:  model.CPUInfo{Model: "AMD EPYC", Cores: 8, Threads: 16, Sockets: 1},
			Memory:   model.MemUsage{Used: 8 * 1024 * 1024 * 1024, Total: 32 * 1024 * 1024 * 1024},
			Swap:     model.MemUsage{Used: 0, Total: 4 * 1024 * 1024 * 1024},
			RootFS:   model.DiskUsage{Used: 50 * 1024 * 1024 * 1024, Total: 500 * 1024 * 1024 * 1024},
			Uptime:   86400,
		},
	})

	c.UpdateGuests("pve1", map[int]*model.Guest{
		101: {
			Instance:  "pve1",
			Node:      "node1",
			ClusterID: "pve1",
			Type:      "lxc",
			VMID:      101,
			Name:      "network-services",
			Status:    "running",
			CPU:       0.05,
			CPUs:      2,
			Mem:       512 * 1024 * 1024,
			MaxMem:    2 * 1024 * 1024 * 1024,
			Uptime:    86400,
		},
	})

	temp := 42
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-test-001": {
			Instance:    "pve1",
			Node:        "node1",
			WWN:         "wwn-test-001",
			DevPath:     "/dev/sda",
			Model:       "Samsung 870 EVO",
			Serial:      "S1234",
			DiskType:    "ssd",
			Protocol:    "ata",
			SizeBytes:   500 * 1024 * 1024 * 1024,
			Health:      "PASSED",
			Status:      model.StatusPassed,
			Temperature: &temp,
			Attributes: []model.SMARTAttribute{
				{ID: 5, Name: "Reallocated_Sector_Ct", Value: 100, Worst: 100, Threshold: 10},
			},
		},
	})

	verified := true
	c.UpdateBackups("pbs1", map[string]*model.Backup{
		"ct/101/2025-01-01T00:00:00Z": {
			PBSInstance: "pbs1",
			Datastore:   "local",
			BackupType:  "ct",
			BackupID:    "101",
			BackupTime:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			Verified:    &verified,
		},
	})

	c.SetLastPoll("pve1", time.Now())
}

// --- handleDashboard ---

func TestHandleDashboard_Root(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestHandleDashboard_NotFound(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- handleNodesFragment ---

func TestHandleNodesFragment_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fragments/nodes", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestHandleNodesFragment_Populated(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/fragments/nodes", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "node1")
}

// --- handleGuestsFragment ---

func TestHandleGuestsFragment_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fragments/guests", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestHandleGuestsFragment_Populated(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/fragments/guests", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "network-services")
}

// --- handleBackupsFragment ---

func TestHandleBackupsFragment_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fragments/backups", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestHandleBackupsFragment_Populated(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/fragments/backups", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- handleEventsFragment ---

func TestHandleEventsFragment_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fragments/events", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestHandleEventsFragment_Populated(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/fragments/events", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- handleDisksFragment ---

func TestHandleDisksFragment_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fragments/disks", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestHandleDisksFragment_Populated(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/fragments/disks", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Samsung 870 EVO")
}

// --- handleDiskDetailFragment ---

func TestHandleDiskDetailFragment_Found(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/fragments/disk/wwn-test-001", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "wwn-test-001")
}

func TestHandleDiskDetailFragment_NotFound(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/fragments/disk/wwn-nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- handleNodeSparkline ---

func TestHandleNodeSparkline_Default(t *testing.T) {
	srv, _, s := newTestServer(t)

	now := time.Now()
	for i := range 5 {
		err := s.InsertNodeSnapshot(model.NodeSnapshot{
			Timestamp: now.Add(-time.Duration(i) * time.Hour).Unix(),
			Instance:  "pve1",
			Node:      "node1",
			CPUPct:    0.25 + float64(i)*0.05,
			MemUsed:   4 * 1024 * 1024 * 1024,
			MemTotal:  32 * 1024 * 1024 * 1024,
		})
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var points []model.SparklinePoint
	err := json.NewDecoder(w.Body).Decode(&points)
	require.NoError(t, err)
	assert.Len(t, points, 5)
}

func TestHandleNodeSparkline_CustomHours(t *testing.T) {
	srv, _, s := newTestServer(t)

	now := time.Now()
	// Insert one point within 2h window and one outside
	require.NoError(t, s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp: now.Add(-1 * time.Hour).Unix(),
		Instance:  "pve1",
		Node:      "node1",
		CPUPct:    0.5,
		MemUsed:   4 * 1024 * 1024 * 1024,
		MemTotal:  32 * 1024 * 1024 * 1024,
	}))
	require.NoError(t, s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp: now.Add(-48 * time.Hour).Unix(),
		Instance:  "pve1",
		Node:      "node1",
		CPUPct:    0.3,
		MemUsed:   4 * 1024 * 1024 * 1024,
		MemTotal:  32 * 1024 * 1024 * 1024,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1?hours=2", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var points []model.SparklinePoint
	require.NoError(t, json.NewDecoder(w.Body).Decode(&points))
	assert.Len(t, points, 1)
}

func TestHandleNodeSparkline_MemoryMetric(t *testing.T) {
	srv, _, s := newTestServer(t)

	now := time.Now()
	require.NoError(t, s.InsertNodeSnapshot(model.NodeSnapshot{
		Timestamp: now.Add(-1 * time.Hour).Unix(),
		Instance:  "pve1",
		Node:      "node1",
		CPUPct:    0.5,
		MemUsed:   16 * 1024 * 1024 * 1024,
		MemTotal:  32 * 1024 * 1024 * 1024,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1?metric=memory", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var points []model.SparklinePoint
	require.NoError(t, json.NewDecoder(w.Body).Decode(&points))
	require.Len(t, points, 1)
	assert.InDelta(t, 50.0, points[0].Value, 0.1)
}

func TestHandleNodeSparkline_InvalidHoursIgnored(t *testing.T) {
	srv, _, _ := newTestServer(t)

	// Invalid hours param should fall back to 24
	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1?hours=abc", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleNodeSparkline_HoursOutOfRange(t *testing.T) {
	srv, _, _ := newTestServer(t)

	// hours=0 is out of range (must be >0 and <=168), should fallback to 24
	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1?hours=0", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleNodeSparkline_EmptyResult(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// null JSON for nil slice is fine
}

func TestHandleNodeSparkline_UnknownMetric(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/node/pve1/node1?metric=bogus", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- handleGuestSparkline ---

func TestHandleGuestSparkline_Valid(t *testing.T) {
	srv, _, s := newTestServer(t)

	now := time.Now()
	require.NoError(t, s.InsertGuestSnapshot(model.GuestSnapshot{
		Timestamp: now.Add(-1 * time.Hour).Unix(),
		Instance:  "pve1",
		VMID:      101,
		Node:      "node1",
		ClusterID: "pve1",
		GuestType: "lxc",
		Name:      "network-services",
		Status:    "running",
		CPUPct:    0.05,
		CPUs:      2,
		MemUsed:   512 * 1024 * 1024,
		MemTotal:  2 * 1024 * 1024 * 1024,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/guest/pve1/101", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var points []model.SparklinePoint
	require.NoError(t, json.NewDecoder(w.Body).Decode(&points))
	assert.Len(t, points, 1)
}

func TestHandleGuestSparkline_InvalidVMID(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/guest/pve1/notanumber", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid VMID")
}

func TestHandleGuestSparkline_EmptyResult(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/guest/pve1/999", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleGuestSparkline_StoreError(t *testing.T) {
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	c := cache.New()
	srv := NewServer(":0", c, s)
	// Close store to trigger query error
	s.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/sparkline/guest/pve1/101", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- handleNodeSparklineSVG ---

func TestHandleNodeSparklineSVG_WithData(t *testing.T) {
	srv, _, s := newTestServer(t)

	now := time.Now()
	for i := range 5 {
		require.NoError(t, s.InsertNodeSnapshot(model.NodeSnapshot{
			Timestamp: now.Add(-time.Duration(i) * time.Hour).Unix(),
			Instance:  "pve1",
			Node:      "node1",
			CPUPct:    0.25 + float64(i)*0.05,
			MemUsed:   4 * 1024 * 1024 * 1024,
			MemTotal:  32 * 1024 * 1024 * 1024,
		}))
	}

	req := httptest.NewRequest(http.MethodGet, "/fragments/sparkline/node/pve1/node1?hours=24&metric=cpu", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "<svg")
	assert.Contains(t, w.Body.String(), "polyline")
}

func TestHandleNodeSparklineSVG_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/fragments/sparkline/node/pve1/node1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "No data")
}

// --- handleGuestSparklineSVG ---

func TestHandleGuestSparklineSVG_WithData(t *testing.T) {
	srv, _, s := newTestServer(t)

	now := time.Now()
	require.NoError(t, s.InsertGuestSnapshot(model.GuestSnapshot{
		Timestamp: now.Add(-1 * time.Hour).Unix(),
		Instance:  "pve1",
		VMID:      101,
		Node:      "node1",
		ClusterID: "pve1",
		GuestType: "lxc",
		Name:      "network-services",
		Status:    "running",
		CPUPct:    0.05,
		CPUs:      2,
		MemUsed:   512 * 1024 * 1024,
		MemTotal:  2 * 1024 * 1024 * 1024,
	}))

	req := httptest.NewRequest(http.MethodGet, "/fragments/sparkline/guest/pve1/101", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "<svg")
}

func TestHandleGuestSparklineSVG_InvalidVMID(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/fragments/sparkline/guest/pve1/notanumber", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- handleHealthz ---

func TestHandleHealthz_NoData(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "no_data", resp["status"])
	assert.Contains(t, resp, "timestamp")
	assert.Contains(t, resp, "collectors")
}

func TestHandleHealthz_WithData(t *testing.T) {
	srv, c, _ := newTestServer(t)
	c.SetLastPoll("pve1", time.Now())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp["status"])

	collectors, ok := resp["collectors"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, collectors, "pve1")
}

// --- Server.Run ---

func TestServerRun_GracefulShutdown(t *testing.T) {
	srv, _, _ := newTestServer(t)
	// Use a high port to avoid conflicts
	srv.server.Addr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	err := <-errCh
	// Should return nil (graceful shutdown) or context.Canceled
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestHandleDashboard_Populated(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "node1")
}

func TestHandleDashboard_CancelledContext(t *testing.T) {
	srv, _, _ := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before render

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	// May return 200 (partial write) or 500 depending on timing — either is acceptable.
	// The key is that it doesn't panic.
}

func TestHandleNodesFragment_CancelledContext(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/fragments/nodes", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	// Should not panic.
}

func TestHandleGuestsFragment_CancelledContext(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/fragments/guests", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
}

func TestHandleBackupsFragment_CancelledContext(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/fragments/backups", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
}

func TestHandleDisksFragment_CancelledContext(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/fragments/disks", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
}

func TestHandleDiskDetailFragment_CancelledContext(t *testing.T) {
	srv, c, _ := newTestServer(t)
	populateCache(c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/fragments/disk/wwn-test-001", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
}

func TestHandleHealthz_MultipleCollectors(t *testing.T) {
	srv, c, _ := newTestServer(t)
	c.SetLastPoll("pve1", time.Now())
	c.SetLastPoll("pbs1", time.Now().Add(-5*time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp["status"])

	collectors := resp["collectors"].(map[string]any)
	assert.Contains(t, collectors, "pve1")
	assert.Contains(t, collectors, "pbs1")
}

// --- SecurityHeadersMiddleware ---

func TestSecurityHeadersMiddleware(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	// Use the full handler stack (includes SecurityHeadersMiddleware).
	srv.server.Handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

// --- renderHTML / writeJSON error paths ---

func TestRenderHTML_WriteBodyFail(t *testing.T) {
	w := &failWriter{header: make(http.Header)}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// The component renders fine; the subsequent Write to w returns an error.
	// This exercises the slog.Debug path — must not panic.
	renderHTML(w, r, templates.Dashboard(cache.CacheSnapshot{}))
}

func TestWriteJSON_MarshalError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// channels cannot be marshalled to JSON.
	writeJSON(w, r, make(chan int))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestWriteJSON_WriteBodyFail(t *testing.T) {
	w := &failWriter{header: make(http.Header)}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// Marshal succeeds; Write to w fails — exercises the slog.Debug path.
	writeJSON(w, r, "ok")
}

// --- handleNodeSparklineSVG store error ---

func TestHandleNodeSparklineSVG_StoreError(t *testing.T) {
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	c := cache.New()
	srv := NewServer(":0", c, s)
	s.Close() // closed store forces a query error

	req := httptest.NewRequest(http.MethodGet, "/fragments/sparkline/node/pve1/node1", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- handleGuestSparklineSVG store error ---

func TestHandleGuestSparklineSVG_StoreError(t *testing.T) {
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	c := cache.New()
	srv := NewServer(":0", c, s)
	s.Close() // closed store forces a query error

	req := httptest.NewRequest(http.MethodGet, "/fragments/sparkline/guest/pve1/101", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- handleWidget ---

func TestHandleWidget_Empty(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 0, resp.Nodes.Total)
	assert.Equal(t, 0, resp.Guests.Total)
	assert.Equal(t, 0.0, resp.CPU.UsagePct)
	assert.Equal(t, 0, resp.Backups.Total)
}

func TestHandleWidget_WithData(t *testing.T) {
	srv, c, _ := newTestServer(t)

	// Two nodes: one online, one offline.
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {
			Instance: "pve1", Name: "node1", Status: "online",
			CPU:    0.40,
			Memory: model.MemUsage{Used: 8 * 1024 * 1024 * 1024, Total: 32 * 1024 * 1024 * 1024},
		},
		"node2": {Instance: "pve1", Name: "node2", Status: "offline"},
	})
	// Three guests: two running VMs, one stopped LXC.
	c.UpdateGuests("pve1", map[int]*model.Guest{
		100: {Type: "qemu", Status: "running"},
		101: {Type: "qemu", Status: "running"},
		102: {Type: "lxc", Status: "stopped"},
	})
	// Four disks covering all status categories.
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-pass":    {Status: model.StatusPassed},
		"wwn-warn":    {Status: model.StatusWarnScrutiny},
		"wwn-fail":    {Status: model.StatusFailedSmart},
		"wwn-unknown": {Status: model.StatusUnknown},
	})
	// Two backups; the later one should be reflected in LastBackupTime.
	size := int64(1024)
	c.UpdateBackups("pbs1", map[string]*model.Backup{
		"vm/100/2025-01-01": {BackupTime: 1000, SizeBytes: &size},
		"vm/100/2025-06-01": {BackupTime: 2000, SizeBytes: &size},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	assert.Equal(t, 2, resp.Nodes.Total)
	assert.Equal(t, 1, resp.Nodes.Online)
	assert.Equal(t, 1, resp.Nodes.Offline)

	assert.Equal(t, 40.0, resp.CPU.UsagePct)
	assert.Greater(t, resp.Memory.TotalBytes, int64(0))
	assert.Greater(t, resp.Memory.UsagePct, 0.0)

	assert.Equal(t, 3, resp.Guests.Total)
	assert.Equal(t, 2, resp.Guests.Running)
	assert.Equal(t, 1, resp.Guests.Stopped)
	assert.Equal(t, 2, resp.Guests.VMs)
	assert.Equal(t, 1, resp.Guests.LXC)

	assert.Equal(t, 4, resp.Disks.Total)
	assert.Equal(t, 1, resp.Disks.Passed)
	assert.Equal(t, 1, resp.Disks.Failed)
	assert.Equal(t, 1, resp.Disks.Warning)
	assert.Equal(t, 1, resp.Disks.Unknown)

	assert.Equal(t, 2, resp.Backups.Total)
	assert.Equal(t, int64(2000), resp.Backups.LastBackupTime)
}

func TestHandleWidget_CPUAveragedAcrossNodes(t *testing.T) {
	// CPU must be averaged over online node count, not summed.
	srv, c, _ := newTestServer(t)
	c.UpdateNodes("pve1", map[string]*model.Node{
		"node1": {Status: "online", CPU: 0.20},
		"node2": {Status: "online", CPU: 0.60},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 40.0, resp.CPU.UsagePct) // (20+60)/2, not 80
}

func TestHandleWidget_OfflineNodeExcluded(t *testing.T) {
	// An offline node must not contribute to CPU or memory aggregates.
	srv, c, _ := newTestServer(t)
	c.UpdateNodes("pve1", map[string]*model.Node{
		"online": {
			Status: "online",
			CPU:    0.50,
			Memory: model.MemUsage{Used: 4 * 1024 * 1024 * 1024, Total: 8 * 1024 * 1024 * 1024},
		},
		"offline": {
			Status: "offline",
			CPU:    0.90, // must not be included in the average
			Memory: model.MemUsage{Used: 7 * 1024 * 1024 * 1024, Total: 8 * 1024 * 1024 * 1024},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Nodes.Total)
	assert.Equal(t, 1, resp.Nodes.Online)
	assert.Equal(t, 1, resp.Nodes.Offline)
	assert.Equal(t, 50.0, resp.CPU.UsagePct)
	assert.Equal(t, int64(8*1024*1024*1024), resp.Memory.TotalBytes) // only the online node
}

func TestHandleWidget_DiskFailedScrutinyBit(t *testing.T) {
	// StatusFailedScrutiny (4) alone must count as Failed, not Warning.
	srv, c, _ := newTestServer(t)
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-scrutiny-fail": {Status: model.StatusFailedScrutiny},
		"wwn-combined-fail": {Status: model.StatusFailedSmart | model.StatusWarnScrutiny},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Disks.Total)
	assert.Equal(t, 2, resp.Disks.Failed)
	assert.Equal(t, 0, resp.Disks.Warning)
}

func TestHandleWidget_BackupsAcrossInstances(t *testing.T) {
	// Backups from multiple PBS instances must all be counted and the global
	// maximum BackupTime used for LastBackupTime.
	srv, c, _ := newTestServer(t)
	size := int64(1024)
	c.UpdateBackups("pbs1", map[string]*model.Backup{
		"vm/100/a": {BackupTime: 1000, SizeBytes: &size},
		"vm/100/b": {BackupTime: 3000, SizeBytes: &size},
	})
	c.UpdateBackups("pbs2", map[string]*model.Backup{
		"vm/200/a": {BackupTime: 2000, SizeBytes: &size},
		"vm/200/b": {BackupTime: 5000, SizeBytes: &size}, // global max
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 4, resp.Backups.Total)
	assert.Equal(t, int64(5000), resp.Backups.LastBackupTime)
}

func TestHandleWidget_GuestPausedStatus(t *testing.T) {
	// A paused guest increments Total but neither Running nor Stopped.
	srv, c, _ := newTestServer(t)
	c.UpdateGuests("pve1", map[int]*model.Guest{
		100: {Type: "qemu", Status: "running"},
		101: {Type: "qemu", Status: "paused"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp widgetResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Guests.Total)
	assert.Equal(t, 1, resp.Guests.Running)
	assert.Equal(t, 0, resp.Guests.Stopped)
}
