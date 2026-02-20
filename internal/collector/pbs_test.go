package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fixtures — realistic PBS API response JSON
// ---------------------------------------------------------------------------

const datastoreStatusJSON = `{
	"data": {
		"total": 1073741824000,
		"used": 536870912000,
		"avail": 536870912000
	}
}`

const datastoreUsageJSON = `{
	"data": [
		{
			"store": "local-backups",
			"total": 1073741824000,
			"used": 536870912000,
			"avail": 536870912000
		},
		{
			"store": "offsite",
			"total": 2147483648000,
			"used": 107374182400,
			"avail": 2040109465600,
			"error": null
		}
	]
}`

const snapshotsJSON = `{
	"data": [
		{
			"backup-type": "ct",
			"backup-id": "101",
			"backup-time": 1700000000,
			"size": 1073741824,
			"verification": {"state": "ok"}
		},
		{
			"backup-type": "ct",
			"backup-id": "101",
			"backup-time": 1699900000,
			"size": 1073000000,
			"verification": {"state": "failed"}
		},
		{
			"backup-type": "vm",
			"backup-id": "200",
			"backup-time": 1700000000,
			"size": 5368709120
		}
	]
}`

const tasksJSON = `{
	"data": [
		{
			"upid": "UPID:pbs:00001234:ABCD1234:67890ABC:backup:101:root@pam:",
			"worker_type": "backup",
			"worker_id": "101",
			"starttime": 1700000000,
			"endtime": 1700003600,
			"status": "OK",
			"user": "root@pam"
		},
		{
			"upid": "UPID:pbs:00001235:ABCD1235:67890ABD:verificationjob:local-backups:root@pam:",
			"worker_type": "verificationjob",
			"worker_id": "local-backups",
			"starttime": 1700010000,
			"endtime": 1700013600,
			"status": "OK",
			"user": "root@pam"
		},
		{
			"upid": "UPID:pbs:00001236:ABCD1236:67890ABE:garbage_collection:local-backups:root@pam:",
			"worker_type": "garbage_collection",
			"worker_id": "local-backups",
			"starttime": 1700020000,
			"endtime": null,
			"status": "",
			"user": "root@pam"
		}
	]
}`

// ---------------------------------------------------------------------------
// Helper: create PBS collector with httptest server
// ---------------------------------------------------------------------------

func newTestPBSCollector(t *testing.T, handler http.Handler, datastores ...string) (*PBSCollector, *cache.Cache, *store.Store, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)

	c := cache.New()
	dbPath := t.TempDir() + "/test.db"
	s, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close(); ts.Close() })

	pool := NewWorkerPool(4)
	cfg := PBSConfig{
		Name:         "test-pbs",
		Host:         ts.URL,
		TokenID:      "backup@pbs!monitor",
		TokenSecret:  "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Insecure:     true,
		Datastores:   datastores,
		PollInterval: 60 * time.Second,
	}
	coll := NewPBSCollector(cfg, pool, c, s)
	return coll, c, s, ts
}

// ---------------------------------------------------------------------------
// apiGet
// ---------------------------------------------------------------------------

func TestPBS_apiGet_AuthHeader(t *testing.T) {
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":{}}`)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.apiGet(context.Background(), "test", "/api2/json/status/datastore-usage")
	require.NoError(t, err)
	// PBS uses PBSAPIToken with colon separator (not = like PVE)
	assert.Equal(t, "PBSAPIToken=backup@pbs!monitor:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", gotAuth)
}

func TestPBS_apiGet_ErrorResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "permission denied")
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.apiGet(context.Background(), "test", "/test")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 401, apiErr.StatusCode)
	assert.False(t, apiErr.IsRetryable())
}

// ---------------------------------------------------------------------------
// collectDatastoreUsage
// ---------------------------------------------------------------------------

func TestPBS_collectDatastoreUsage(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api2/json/status/datastore-usage", r.URL.Path)
		fmt.Fprint(w, datastoreUsageJSON)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	datastores, err := coll.collectAllDatastoreUsage(context.Background())
	require.NoError(t, err)
	require.Len(t, datastores, 2)

	lb := datastores["local-backups"]
	require.NotNil(t, lb)
	assert.Equal(t, "test-pbs", lb.PBSInstance)
	assert.Equal(t, "local-backups", lb.Name)
	require.NotNil(t, lb.TotalBytes)
	assert.Equal(t, int64(1073741824000), *lb.TotalBytes)
	require.NotNil(t, lb.UsedBytes)
	assert.Equal(t, int64(536870912000), *lb.UsedBytes)

	off := datastores["offsite"]
	require.NotNil(t, off)
	assert.Nil(t, off.Error) // null in JSON maps to nil
}

// ---------------------------------------------------------------------------
// collectSnapshots
// ---------------------------------------------------------------------------

func TestPBS_collectSnapshots(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api2/json/admin/datastore/local-backups/snapshots", r.URL.Path)
		fmt.Fprint(w, snapshotsJSON)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	backups, err := coll.collectSnapshots(context.Background(), "local-backups")
	require.NoError(t, err)
	require.Len(t, backups, 3)

	// First backup: verified ok
	b0 := backups[0]
	assert.Equal(t, "ct", b0.BackupType)
	assert.Equal(t, "101", b0.BackupID)
	assert.Equal(t, int64(1700000000), b0.BackupTime)
	require.NotNil(t, b0.SizeBytes)
	assert.Equal(t, int64(1073741824), *b0.SizeBytes)
	require.NotNil(t, b0.Verified)
	assert.True(t, *b0.Verified)

	// Second backup: verified failed
	b1 := backups[1]
	require.NotNil(t, b1.Verified)
	assert.False(t, *b1.Verified)

	// Third backup: no verification
	b2 := backups[2]
	assert.Nil(t, b2.Verified)
	assert.Equal(t, "vm", b2.BackupType)
}

// ---------------------------------------------------------------------------
// collectTasks
// ---------------------------------------------------------------------------

func TestPBS_collectTasks(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api2/json/nodes/localhost/tasks")
		assert.NotEmpty(t, r.URL.Query().Get("since"))
		fmt.Fprint(w, tasksJSON)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	tasks, err := coll.collectTasks(context.Background())
	require.NoError(t, err)
	require.Len(t, tasks, 3)

	// Regular backup task
	assert.Equal(t, "backup", tasks[0].Type)
	assert.Equal(t, "OK", tasks[0].Status)
	assert.Equal(t, "root@pam", tasks[0].User)
	require.NotNil(t, tasks[0].EndTime)
	assert.Equal(t, int64(1700003600), *tasks[0].EndTime)

	// verificationjob -> verify
	assert.Equal(t, "verify", tasks[1].Type)

	// garbage_collection -> gc
	assert.Equal(t, "gc", tasks[2].Type)
	assert.Nil(t, tasks[2].EndTime) // still running
	assert.Empty(t, tasks[2].Status)
}

// ---------------------------------------------------------------------------
// Datastore filtering
// ---------------------------------------------------------------------------

func TestPBS_Collect_DatastoreFiltering(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/admin/datastore/local-backups/status":
			fmt.Fprint(w, datastoreStatusJSON)
		case r.URL.Path == "/api2/json/admin/datastore/local-backups/snapshots":
			fmt.Fprint(w, snapshotsJSON)
		case r.URL.Path == "/api2/json/admin/datastore/offsite/status":
			t.Error("should not query offsite status when filtered")
			fmt.Fprint(w, datastoreStatusJSON)
		case r.URL.Path == "/api2/json/admin/datastore/offsite/snapshots":
			t.Error("should not query offsite snapshots when filtered")
			fmt.Fprint(w, `{"data": []}`)
		default:
			// tasks endpoint
			fmt.Fprint(w, `{"data": []}`)
		}
	})
	// Only monitor "local-backups"
	coll, ch, _, _ := newTestPBSCollector(t, handler, "local-backups")

	err := coll.Collect(context.Background())
	require.NoError(t, err)

	snap := ch.Snapshot()
	require.Contains(t, snap.Datastores, "test-pbs")
	assert.Len(t, snap.Datastores["test-pbs"], 1)
	assert.Contains(t, snap.Datastores["test-pbs"], "local-backups")
}

func TestPBS_Collect_NoFilterShowsAll(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/status/datastore-usage":
			fmt.Fprint(w, datastoreUsageJSON)
		case r.URL.Path == "/api2/json/admin/datastore/local-backups/snapshots":
			fmt.Fprint(w, snapshotsJSON)
		case r.URL.Path == "/api2/json/admin/datastore/offsite/snapshots":
			fmt.Fprint(w, `{"data": []}`)
		default:
			fmt.Fprint(w, `{"data": []}`)
		}
	})
	// No filter — monitor all
	coll, ch, _, _ := newTestPBSCollector(t, handler)

	err := coll.Collect(context.Background())
	require.NoError(t, err)

	snap := ch.Snapshot()
	require.Contains(t, snap.Datastores, "test-pbs")
	assert.Len(t, snap.Datastores["test-pbs"], 2)
}

// ---------------------------------------------------------------------------
// Full Collect cycle
// ---------------------------------------------------------------------------

func TestPBS_Collect_FullCycle(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/status/datastore-usage":
			fmt.Fprint(w, datastoreUsageJSON)
		case r.URL.Path == "/api2/json/admin/datastore/local-backups/snapshots":
			fmt.Fprint(w, snapshotsJSON)
		case r.URL.Path == "/api2/json/admin/datastore/offsite/snapshots":
			fmt.Fprint(w, `{"data": []}`)
		default:
			fmt.Fprint(w, tasksJSON)
		}
	})
	coll, ch, _, _ := newTestPBSCollector(t, handler)

	err := coll.Collect(context.Background())
	require.NoError(t, err)

	snap := ch.Snapshot()

	// Datastores
	require.Contains(t, snap.Datastores, "test-pbs")
	assert.Len(t, snap.Datastores["test-pbs"], 2)

	// Backups — only latest per (datastore, backup_id) kept
	require.Contains(t, snap.Backups, "test-pbs")
	backups := snap.Backups["test-pbs"]
	// "local-backups/101": two snapshots across the datastore, only latest (1700000000) kept
	// "local-backups/200": one snapshot
	assert.Len(t, backups, 2)
	b101 := backups["local-backups/101"]
	require.NotNil(t, b101)
	assert.Equal(t, int64(1700000000), b101.BackupTime) // latest

	// Tasks
	require.Contains(t, snap.Tasks, "test-pbs")
	assert.Len(t, snap.Tasks["test-pbs"], 3)

	// LastPoll
	_, ok := snap.LastPoll["pbs:test-pbs"]
	assert.True(t, ok)
}

// ---------------------------------------------------------------------------
// Name and Interval
// ---------------------------------------------------------------------------

func TestPBS_NameAndInterval(t *testing.T) {
	coll := &PBSCollector{config: PBSConfig{Name: "prod-pbs", PollInterval: 2 * time.Minute}}
	assert.Equal(t, "pbs:prod-pbs", coll.Name())
	assert.Equal(t, 2*time.Minute, coll.Interval())
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestPBS_collectDatastoreUsage_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.collectAllDatastoreUsage(context.Background())
	require.Error(t, err)
}

func TestPBS_collectDatastoreUsage_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.collectAllDatastoreUsage(context.Background())
	require.Error(t, err)
}

func TestPBS_collectSnapshots_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden")
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.collectSnapshots(context.Background(), "local-backups")
	require.Error(t, err)
}

func TestPBS_collectSnapshots_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.collectSnapshots(context.Background(), "local-backups")
	require.Error(t, err)
}

func TestPBS_collectTasks_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.collectTasks(context.Background())
	require.Error(t, err)
}

func TestPBS_collectTasks_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.collectTasks(context.Background())
	require.Error(t, err)
}

func TestPBS_Collect_DatastoreUsageFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	err := coll.Collect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collecting datastore usage")
}

func TestPBS_apiGet_RetryableOnNetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	ts.Close()

	c := cache.New()
	dbPath := t.TempDir() + "/test.db"
	s, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	pool := NewWorkerPool(4)
	cfg := PBSConfig{Name: "test", Host: ts.URL, TokenID: "t", TokenSecret: "s", PollInterval: 60 * time.Second}
	coll := NewPBSCollector(cfg, pool, c, s)

	_, err = coll.apiGet(context.Background(), "test", "/test")
	require.Error(t, err)
	var re *RetryableError
	assert.ErrorAs(t, err, &re)
}

func TestPBS_Collect_SnapshotsFail_ContinuesOtherDatastores(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/status/datastore-usage":
			fmt.Fprint(w, datastoreUsageJSON)
		case r.URL.Path == "/api2/json/admin/datastore/local-backups/snapshots":
			// Fail for this datastore
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "error")
		case r.URL.Path == "/api2/json/admin/datastore/offsite/snapshots":
			callCount++
			fmt.Fprint(w, `{"data": []}`)
		default:
			fmt.Fprint(w, `{"data": []}`)
		}
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	err := coll.Collect(context.Background())
	require.NoError(t, err) // snapshot failure is logged but not fatal
	assert.Equal(t, 1, callCount, "should still query offsite snapshots")
}

func TestPBS_Collect_TasksFail_StillSucceeds(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/status/datastore-usage":
			fmt.Fprint(w, `{"data": []}`)
		default:
			// Tasks endpoint fails
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "error")
		}
	})
	coll, ch, _, _ := newTestPBSCollector(t, handler)

	err := coll.Collect(context.Background())
	require.NoError(t, err) // task failure is logged but not fatal

	snap := ch.Snapshot()
	_, hasTasks := snap.Tasks["test-pbs"]
	assert.False(t, hasTasks, "tasks should not be in cache when nil")
}

func TestPBS_Collect_Tasks403_StillSucceeds(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/status/datastore-usage":
			fmt.Fprint(w, `{"data": []}`)
		default:
			// Tasks endpoint returns 403 (token lacks Sys.Audit)
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "permission denied")
		}
	})
	coll, ch, _, _ := newTestPBSCollector(t, handler)

	err := coll.Collect(context.Background())
	require.NoError(t, err) // 403 on tasks is a warning, not fatal

	snap := ch.Snapshot()
	_, hasTasks := snap.Tasks["test-pbs"]
	assert.False(t, hasTasks, "tasks should not be in cache on 403")
}

func TestPBS_ApiGet_CancelledContext(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := coll.apiGet(ctx, "test", "/api2/json/nodes")
	assert.Error(t, err)
}

func TestPBS_ApiGet_Non200Status(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	coll, _, _, _ := newTestPBSCollector(t, handler)

	_, err := coll.apiGet(context.Background(), "test", "/api2/json/status/datastore-usage")
	assert.Error(t, err)

	var apiErr *APIError
	assert.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

// ---------------------------------------------------------------------------
// Fuzz tests — PBS response parsing
// ---------------------------------------------------------------------------

// FuzzParsePBSDatastoreUsage exercises the system-wide datastore usage
// response. All capacity fields are nullable (*int64) and error is *string,
// so the parser must not panic on null or unexpected values.
func FuzzParsePBSDatastoreUsage(f *testing.F) {
	f.Add([]byte(`{"data":[{"store":"local","total":1073741824000,"used":536870912000,"avail":536870912000}]}`))
	f.Add([]byte(`{"data":[{"store":"broken","error":"device not found"}]}`))           // error field present
	f.Add([]byte(`{"data":[{"store":"partial","total":null,"used":null,"avail":null}]}`)) // all nulls
	f.Add([]byte(`{"data":[]}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Mirrors the anonymous struct in collectAllDatastoreUsage exactly.
		var resp struct {
			Data []struct {
				Store string  `json:"store"`
				Total *int64  `json:"total"`
				Used  *int64  `json:"used"`
				Avail *int64  `json:"avail"`
				Error *string `json:"error"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return
		}
		// Dereference all pointer fields as the production code does — must not panic.
		for _, ds := range resp.Data {
			if ds.Total != nil {
				_ = *ds.Total
			}
			if ds.Used != nil {
				_ = *ds.Used
			}
			if ds.Avail != nil {
				_ = *ds.Avail
			}
			if ds.Error != nil {
				_ = *ds.Error
			}
		}
	})
}

// FuzzParsePBSSnapshots exercises snapshot list parsing. The verification
// sub-object is a nullable pointer; size is also nullable.
func FuzzParsePBSSnapshots(f *testing.F) {
	f.Add([]byte(`{"data":[{"backup-type":"vm","backup-id":"100","backup-time":1700000000,"size":5368709120,"verification":{"state":"ok"}}]}`))
	f.Add([]byte(`{"data":[{"backup-type":"ct","backup-id":"200","backup-time":1700000000,"size":1073741824,"verification":null}]}`))   // null verification
	f.Add([]byte(`{"data":[{"backup-type":"vm","backup-id":"101","backup-time":1700000000,"size":null,"verification":{"state":"failed"}}]}`)) // null size + failed
	f.Add([]byte(`{"data":[{"backup-type":"host","backup-id":"pbs","backup-time":1700000000}]}`)) // minimal — no size or verification
	f.Add([]byte(`{"data":[]}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Mirrors the anonymous struct in collectSnapshots exactly.
		var resp struct {
			Data []struct {
				BackupType   string `json:"backup-type"`
				BackupID     string `json:"backup-id"`
				BackupTime   int64  `json:"backup-time"`
				Size         *int64 `json:"size"`
				Verification *struct {
					State string `json:"state"`
				} `json:"verification"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return
		}
		// Mirror the nil checks in collectSnapshots — must never panic.
		for _, s := range resp.Data {
			if s.Verification != nil {
				_ = s.Verification.State == "ok"
			}
			if s.Size != nil {
				_ = *s.Size
			}
		}
	})
}

// FuzzParsePBSTasks exercises task list parsing, including the worker_type
// normalisation (verificationjob→verify, garbage_collection→gc) and the
// nullable EndTime field.
func FuzzParsePBSTasks(f *testing.F) {
	f.Add([]byte(`{"data":[{"upid":"UPID:pbs:1","worker_type":"backup","worker_id":"100","starttime":1700000000,"endtime":1700003600,"status":"OK","user":"root@pam"}]}`))
	f.Add([]byte(`{"data":[{"upid":"UPID:pbs:2","worker_type":"verificationjob","worker_id":"local","starttime":1700000000,"status":"running","user":"admin@pbs"}]}`)) // no endtime
	f.Add([]byte(`{"data":[{"upid":"UPID:pbs:3","worker_type":"garbage_collection","worker_id":"","starttime":1700000000,"endtime":1700001800,"status":"OK","user":"root@pam"}]}`))
	f.Add([]byte(`{"data":[{"upid":"UPID:pbs:4","worker_type":"prune","worker_id":"local","starttime":1700000000,"endtime":null,"status":"OK","user":"root@pam"}]}`)) // null endtime
	f.Add([]byte(`{"data":[]}`))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Mirrors the anonymous struct in collectTasks exactly.
		var resp struct {
			Data []struct {
				UPID      string `json:"upid"`
				Type      string `json:"worker_type"`
				ID        string `json:"worker_id"`
				StartTime int64  `json:"starttime"`
				EndTime   *int64 `json:"endtime"`
				Status    string `json:"status"`
				User      string `json:"user"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return
		}
		// Mirror task type normalisation from collectTasks — must never panic.
		for _, task := range resp.Data {
			taskType := task.Type
			switch taskType {
			case "verificationjob":
				taskType = "verify"
			case "garbage_collection":
				taskType = "gc"
			}
			_ = taskType
			if task.EndTime != nil {
				_ = *task.EndTime
			}
		}
	})
}
