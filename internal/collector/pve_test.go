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
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test fixtures — realistic PVE API response JSON
// ---------------------------------------------------------------------------

const nodesListJSON = `{
	"data": [
		{"node": "pve", "status": "online", "id": "node/pve"},
		{"node": "pve2", "status": "offline", "id": "node/pve2"}
	]
}`

const nodeStatusJSON = `{
	"data": {
		"cpu": 0.0423,
		"cpuinfo": {
			"model": "Intel(R) Core(TM) i7-10700 CPU @ 2.90GHz",
			"cores": 8,
			"cpus": 16,
			"sockets": 1
		},
		"memory": {"used": 8589934592, "total": 34359738368},
		"swap": {"used": 0, "total": 8589934592},
		"rootfs": {"used": 21474836480, "total": 107374182400},
		"loadavg": ["0.42", "0.38", "0.35"],
		"uptime": 1234567,
		"wait": 0.0012,
		"pveversion": "pve-manager/8.2.4/faa83925abd647c7",
		"kversion": "Linux 6.8.4-2-pve #1 SMP PREEMPT_DYNAMIC"
	}
}`

const nodeStatusFloatLoadJSON = `{
	"data": {
		"cpu": 0.15,
		"cpuinfo": {"model": "AMD EPYC 7543", "cores": 32, "cpus": 64, "sockets": 1},
		"memory": {"used": 1073741824, "total": 17179869184},
		"swap": {"used": 0, "total": 4294967296},
		"rootfs": {"used": 5368709120, "total": 53687091200},
		"loadavg": [1.23, 0.98, 0.76],
		"uptime": 999999,
		"wait": 0.005,
		"pveversion": "pve-manager/8.3.0",
		"kversion": "Linux 6.8.12-1-pve"
	}
}`

const lxcListJSON = `{
	"data": [
		{
			"vmid": 101,
			"name": "network-services",
			"status": "running",
			"cpu": 0.0015,
			"cpus": 2,
			"mem": 268435456,
			"maxmem": 536870912,
			"disk": 1073741824,
			"maxdisk": 8589934592,
			"netin": 1234567890,
			"netout": 987654321,
			"uptime": 86400
		},
		{
			"vmid": 200,
			"name": "traefik",
			"status": "running",
			"cpu": 0.003,
			"cpus": 1,
			"mem": 134217728,
			"maxmem": 268435456,
			"disk": 536870912,
			"maxdisk": 4294967296,
			"netin": 5555555,
			"netout": 3333333,
			"uptime": 86400
		}
	]
}`

const qemuListJSON = `{"data": []}`

const diskListJSON = `{
	"data": [
		{
			"devpath": "/dev/sda",
			"model": "Samsung SSD 870 EVO",
			"serial": "S6PPNX0T123456",
			"wwn": "0x5002538f4321abcd",
			"size": 1000204886016,
			"type": "ssd"
		},
		{
			"devpath": "/dev/nvme0n1",
			"model": "Samsung 970 EVO Plus",
			"serial": "S4EWNX0M123456",
			"wwn": "eui.002538b331234567",
			"size": 500107862016,
			"type": "nvme"
		}
	]
}`

const smartSDAJSON = `{
	"data": {
		"health": "PASSED",
		"type": "ata",
		"wearout": 98,
		"attributes": [
			{"id": 194, "name": "Temperature_Celsius", "value": 100, "worst": 100, "threshold": 0, "raw": "32 (Min/Max 20/45)"},
			{"id": 9, "name": "Power_On_Hours", "value": 99, "worst": 99, "threshold": 0, "raw": "12345"}
		]
	}
}`

const smartNVMeJSON = `{
	"data": {
		"health": "PASSED",
		"type": "nvme",
		"wearout": "95",
		"attributes": [],
		"text": "SMART/Health Information (NVMe Log 0x02)\nTemperature:                        38 Celsius"
	}
}`

const clusterStatusJSON = `{
	"data": [
		{"type": "cluster", "name": "homelab-cluster", "id": "cluster"},
		{"type": "node", "name": "pve", "id": "node/pve", "online": 1}
	]
}`

const clusterStatusStandaloneJSON = `{
	"data": [
		{"type": "node", "name": "pve", "id": "node/pve", "online": 1}
	]
}`

// ---------------------------------------------------------------------------
// parseNodeStatus
// ---------------------------------------------------------------------------

func TestParseNodeStatus_Valid(t *testing.T) {
	var resp pveResponse
	require.NoError(t, json.Unmarshal([]byte(nodeStatusJSON), &resp))

	node, err := parseNodeStatus("homelab", "pve", resp.Data)
	require.NoError(t, err)

	assert.Equal(t, "homelab", node.Instance)
	assert.Equal(t, "pve", node.Name)
	assert.Equal(t, "online", node.Status)
	assert.InDelta(t, 0.0423, node.CPU, 0.0001)
	assert.Equal(t, "Intel(R) Core(TM) i7-10700 CPU @ 2.90GHz", node.CPUInfo.Model)
	assert.Equal(t, 8, node.CPUInfo.Cores)
	assert.Equal(t, 16, node.CPUInfo.Threads)
	assert.Equal(t, 1, node.CPUInfo.Sockets)
	assert.Equal(t, int64(8589934592), node.Memory.Used)
	assert.Equal(t, int64(34359738368), node.Memory.Total)
	assert.Equal(t, int64(0), node.Swap.Used)
	assert.Equal(t, int64(21474836480), node.RootFS.Used)
	assert.InDelta(t, 0.42, node.LoadAvg[0], 0.01)
	assert.InDelta(t, 0.38, node.LoadAvg[1], 0.01)
	assert.InDelta(t, 0.35, node.LoadAvg[2], 0.01)
	assert.Equal(t, int64(1234567), node.Uptime)
	assert.InDelta(t, 0.0012, node.IOWait, 0.0001)
}

func TestParseNodeStatus_FloatLoadAvg(t *testing.T) {
	var resp pveResponse
	require.NoError(t, json.Unmarshal([]byte(nodeStatusFloatLoadJSON), &resp))

	node, err := parseNodeStatus("remote", "pve2", resp.Data)
	require.NoError(t, err)

	assert.InDelta(t, 1.23, node.LoadAvg[0], 0.01)
	assert.InDelta(t, 0.98, node.LoadAvg[1], 0.01)
	assert.InDelta(t, 0.76, node.LoadAvg[2], 0.01)
}

func TestParseNodeStatus_MissingFields(t *testing.T) {
	data := json.RawMessage(`{"cpu": 0.5}`)
	node, err := parseNodeStatus("inst", "n1", data)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, node.CPU, 0.01)
	assert.Equal(t, [3]float64{0, 0, 0}, node.LoadAvg)
}

func TestParseNodeStatus_InvalidJSON(t *testing.T) {
	_, err := parseNodeStatus("inst", "n1", json.RawMessage(`{invalid`))
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// parseLoadAvg
// ---------------------------------------------------------------------------

func TestParseLoadAvg(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected [3]float64
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: [3]float64{0, 0, 0},
		},
		{
			name:     "empty array",
			input:    []interface{}{},
			expected: [3]float64{0, 0, 0},
		},
		{
			name:     "string values",
			input:    []interface{}{"1.23", "0.45", "0.67"},
			expected: [3]float64{1.23, 0.45, 0.67},
		},
		{
			name:     "float values",
			input:    []interface{}{1.23, 0.45, 0.67},
			expected: [3]float64{1.23, 0.45, 0.67},
		},
		{
			name:     "mixed string and float",
			input:    []interface{}{"0.50", 1.0, "2.5"},
			expected: [3]float64{0.50, 1.0, 2.5},
		},
		{
			name:     "only two values",
			input:    []interface{}{1.0, 2.0},
			expected: [3]float64{1.0, 2.0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLoadAvg(tt.input)
			assert.InDeltaSlice(t, tt.expected[:], result[:], 0.001)
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: create PVE collector with httptest server
// ---------------------------------------------------------------------------

func newTestPVECollector(t *testing.T, handler http.Handler) (*PVECollector, *cache.Cache, *store.Store, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)

	c := cache.New()
	dbPath := t.TempDir() + "/test.db"
	s, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close(); ts.Close() })

	pool := NewWorkerPool(4)
	cfg := PVEConfig{
		Name:             "test-pve",
		Host:             ts.URL,
		TokenID:          "root@pam!monitor",
		TokenSecret:      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Insecure:         true,
		PollInterval:     30 * time.Second,
		DiskPollInterval: 5 * time.Minute,
	}
	collector := NewPVECollector(cfg, pool, c, s)
	return collector, c, s, ts
}

// ---------------------------------------------------------------------------
// apiGet
// ---------------------------------------------------------------------------

func TestPVE_apiGet_AuthHeader(t *testing.T) {
	var gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":{}}`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.apiGet(context.Background(), "/api2/json/nodes")
	require.NoError(t, err)
	assert.Equal(t, "PVEAPIToken=root@pam!monitor=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", gotAuth)
}

func TestPVE_apiGet_ErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{"401 unauthorized", 401, false},
		{"500 server error", 500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, "error body")
			})
			coll, _, _, _ := newTestPVECollector(t, handler)

			_, err := coll.apiGet(context.Background(), "/test")
			require.Error(t, err)

			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.statusCode, apiErr.StatusCode)
			assert.Equal(t, tt.retryable, apiErr.IsRetryable())
		})
	}
}

// ---------------------------------------------------------------------------
// discoverNodes
// ---------------------------------------------------------------------------

func TestPVE_discoverNodes_FiltersOffline(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, nodesListJSON)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.discoverNodes(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []string{"pve"}, coll.nodes)
}

func TestPVE_discoverNodes_AllOnline(t *testing.T) {
	resp := `{"data": [{"node": "pve1", "status": "online"}, {"node": "pve2", "status": "online"}]}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, resp)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.discoverNodes(context.Background())
	require.NoError(t, err)
	assert.Len(t, coll.nodes, 2)
}

// ---------------------------------------------------------------------------
// detectCluster
// ---------------------------------------------------------------------------

func TestPVE_detectCluster_ClusterFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, clusterStatusJSON)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.detectCluster(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "homelab-cluster", coll.clusterID)
}

func TestPVE_detectCluster_Standalone(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, clusterStatusStandaloneJSON)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.detectCluster(context.Background())
	assert.Error(t, err) // "no cluster entry found"
}

// ---------------------------------------------------------------------------
// collectNodeStatus
// ---------------------------------------------------------------------------

func TestPVE_collectNodeStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api2/json/nodes/pve/status", r.URL.Path)
		fmt.Fprint(w, nodeStatusJSON)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	node, err := coll.collectNodeStatus(context.Background(), "pve")
	require.NoError(t, err)
	assert.Equal(t, "pve", node.Name)
	assert.InDelta(t, 0.0423, node.CPU, 0.001)
}

// ---------------------------------------------------------------------------
// collectGuestType / collectGuests
// ---------------------------------------------------------------------------

func TestPVE_collectGuestType_LXC(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api2/json/nodes/pve/lxc", r.URL.Path)
		fmt.Fprint(w, lxcListJSON)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)
	coll.clusterID = "test"

	guests, err := coll.collectGuestType(context.Background(), "pve", "lxc")
	require.NoError(t, err)
	require.Len(t, guests, 2)

	assert.Equal(t, 101, guests[0].VMID)
	assert.Equal(t, "network-services", guests[0].Name)
	assert.Equal(t, "running", guests[0].Status)
	assert.Equal(t, "lxc", guests[0].Type)
	assert.Equal(t, "test", guests[0].ClusterID)
	assert.Equal(t, int64(1234567890), guests[0].NetIn)

	assert.Equal(t, 200, guests[1].VMID)
	assert.Equal(t, "traefik", guests[1].Name)
}

func TestPVE_collectGuests_CombinesLXCAndQemu(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/pve/lxc":
			fmt.Fprint(w, lxcListJSON)
		case "/api2/json/nodes/pve/qemu":
			fmt.Fprint(w, qemuListJSON)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)
	coll.clusterID = "test"

	guests, err := coll.collectGuests(context.Background(), "pve")
	require.NoError(t, err)
	assert.Len(t, guests, 2) // 2 LXC + 0 QEMU
}

// ---------------------------------------------------------------------------
// collectDisks
// ---------------------------------------------------------------------------

func TestPVE_collectDisks(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/nodes/pve/disks/list":
			fmt.Fprint(w, diskListJSON)
		case r.URL.Path == "/api2/json/nodes/pve/disks/smart" && r.URL.Query().Get("disk") == "/dev/sda":
			fmt.Fprint(w, smartSDAJSON)
		case r.URL.Path == "/api2/json/nodes/pve/disks/smart" && r.URL.Query().Get("disk") == "/dev/nvme0n1":
			fmt.Fprint(w, smartNVMeJSON)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disks, err := coll.collectDisks(context.Background(), "pve")
	require.NoError(t, err)
	require.Len(t, disks, 2)

	// SSD disk
	ssd := disks[0]
	assert.Equal(t, "0x5002538f4321abcd", ssd.WWN)
	assert.Equal(t, "ssd", ssd.DiskType)
	assert.Equal(t, "ata", ssd.Protocol)
	assert.Equal(t, "PASSED", ssd.Health)
	require.NotNil(t, ssd.Wearout)
	assert.Equal(t, 98, *ssd.Wearout)
	require.NotNil(t, ssd.Temperature)
	assert.Equal(t, 32, *ssd.Temperature)
	require.NotNil(t, ssd.PowerOnHours)
	assert.Equal(t, 12345, *ssd.PowerOnHours)

	// NVMe disk
	nvme := disks[1]
	assert.Equal(t, "eui.002538b331234567", nvme.WWN)
	assert.Equal(t, "nvme", nvme.DiskType)
	assert.Equal(t, "nvme", nvme.Protocol)
	assert.Equal(t, "PASSED", nvme.Health)
	require.NotNil(t, nvme.Wearout)
	assert.Equal(t, 95, *nvme.Wearout) // parsed from string "95"
}

func TestPVE_collectDisks_DevPathFallback(t *testing.T) {
	// Disks with no WWN and no serial fall back to DevPath as identity so they
	// are still collected (with a SMART error if that also fails).
	resp := `{"data": [{"devpath": "/dev/sdb", "model": "Unknown", "serial": "", "wwn": "", "size": 100, "type": "hdd"}]}`
	smartResp := `{"data": {"health": "PASSED", "type": "ata", "attributes": []}}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/nodes/pve/disks/list" {
			fmt.Fprint(w, resp)
		} else {
			fmt.Fprint(w, smartResp)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disks, err := coll.collectDisks(context.Background(), "pve")
	require.NoError(t, err)
	require.Len(t, disks, 1)
	assert.Equal(t, "/dev/sdb", disks[0].WWN) // DevPath used as identity
}

func TestPVE_collectDisks_FallsBackToSerial(t *testing.T) {
	resp := `{"data": [{"devpath": "/dev/sdb", "model": "Test", "serial": "SER123", "wwn": "", "size": 100, "type": "hdd"}]}`
	smartResp := `{"data": {"health": "PASSED", "type": "ata", "attributes": []}}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/nodes/pve/disks/list" {
			fmt.Fprint(w, resp)
		} else {
			fmt.Fprint(w, smartResp)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disks, err := coll.collectDisks(context.Background(), "pve")
	require.NoError(t, err)
	require.Len(t, disks, 1)
	assert.Equal(t, "SER123", disks[0].WWN)
}

// ---------------------------------------------------------------------------
// Full Collect cycle
// ---------------------------------------------------------------------------

func TestPVE_Collect_FullCycle(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes":
			fmt.Fprint(w, nodesListJSON)
		case "/api2/json/nodes/pve/status":
			fmt.Fprint(w, nodeStatusJSON)
		case "/api2/json/nodes/pve/lxc":
			fmt.Fprint(w, lxcListJSON)
		case "/api2/json/nodes/pve/qemu":
			fmt.Fprint(w, qemuListJSON)
		case "/api2/json/nodes/pve/disks/list":
			fmt.Fprint(w, diskListJSON)
		case "/api2/json/nodes/pve/disks/smart":
			disk := r.URL.Query().Get("disk")
			switch disk {
			case "/dev/sda":
				fmt.Fprint(w, smartSDAJSON)
			case "/dev/nvme0n1":
				fmt.Fprint(w, smartNVMeJSON)
			default:
				http.Error(w, "unknown disk", 404)
			}
		default:
			http.Error(w, "not found", 404)
		}
	})
	coll, ch, _, _ := newTestPVECollector(t, handler)

	err := coll.Collect(context.Background())
	require.NoError(t, err)

	// Verify cache was updated
	snap := ch.Snapshot()

	// Nodes: only "pve" (pve2 was offline, filtered by discoverNodes)
	require.Contains(t, snap.Nodes, "test-pve")
	require.Contains(t, snap.Nodes["test-pve"], "pve")
	assert.InDelta(t, 0.0423, snap.Nodes["test-pve"]["pve"].CPU, 0.001)

	// Guests: 2 LXC containers, keyed by cluster ID (instance name since not cluster)
	require.Contains(t, snap.Guests, "test-pve")
	assert.Len(t, snap.Guests["test-pve"], 2)
	assert.Equal(t, "network-services", snap.Guests["test-pve"][101].Name)

	// Disks: 2 disks
	assert.Len(t, snap.Disks, 2)

	// LastPoll should be set
	_, ok := snap.LastPoll["pve:test-pve"]
	assert.True(t, ok)
}

func TestPVE_Collect_ClusterMode(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes":
			fmt.Fprint(w, `{"data": [{"node": "pve", "status": "online"}]}`)
		case "/api2/json/cluster/status":
			fmt.Fprint(w, clusterStatusJSON)
		case "/api2/json/nodes/pve/status":
			fmt.Fprint(w, nodeStatusJSON)
		case "/api2/json/nodes/pve/lxc":
			fmt.Fprint(w, lxcListJSON)
		case "/api2/json/nodes/pve/qemu":
			fmt.Fprint(w, qemuListJSON)
		case "/api2/json/nodes/pve/disks/list":
			fmt.Fprint(w, diskListJSON)
		case "/api2/json/nodes/pve/disks/smart":
			fmt.Fprint(w, smartSDAJSON) // simplified, same for all
		default:
			http.Error(w, "not found", 404)
		}
	})
	coll, ch, _, _ := newTestPVECollector(t, handler)
	coll.config.IsCluster = true

	err := coll.Collect(context.Background())
	require.NoError(t, err)

	snap := ch.Snapshot()
	// Guests should be keyed by cluster name, not instance
	require.Contains(t, snap.Guests, "homelab-cluster")
}

func TestPVE_Collect_SkipsDiskPollWhenNotDue(t *testing.T) {
	var diskListCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes":
			fmt.Fprint(w, `{"data": [{"node": "pve", "status": "online"}]}`)
		case "/api2/json/nodes/pve/status":
			fmt.Fprint(w, nodeStatusJSON)
		case "/api2/json/nodes/pve/lxc":
			fmt.Fprint(w, lxcListJSON)
		case "/api2/json/nodes/pve/qemu":
			fmt.Fprint(w, qemuListJSON)
		case "/api2/json/nodes/pve/disks/list":
			diskListCalled = true
			fmt.Fprint(w, diskListJSON)
		case "/api2/json/nodes/pve/disks/smart":
			fmt.Fprint(w, smartSDAJSON)
		default:
			http.Error(w, "not found", 404)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)
	// Pretend disks were polled recently
	coll.lastDiskPoll = time.Now()

	err := coll.Collect(context.Background())
	require.NoError(t, err)
	assert.False(t, diskListCalled, "disk list should not be called when not due")
}

// ---------------------------------------------------------------------------
// Name and Interval
// ---------------------------------------------------------------------------

func TestPVE_NameAndInterval(t *testing.T) {
	coll := &PVECollector{config: PVEConfig{Name: "prod", PollInterval: 15 * time.Second}}
	assert.Equal(t, "pve:prod", coll.Name())
	assert.Equal(t, 15*time.Second, coll.Interval())
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

func TestPVE_discoverNodes_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "server error")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.discoverNodes(context.Background())
	require.Error(t, err)
}

func TestPVE_discoverNodes_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.discoverNodes(context.Background())
	require.Error(t, err)
}

func TestPVE_discoverNodes_InvalidDataField(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data": "not an array"}`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.discoverNodes(context.Background())
	require.Error(t, err)
}

func TestPVE_collectNodeStatus_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectNodeStatus(context.Background(), "pve")
	require.Error(t, err)
}

func TestPVE_collectNodeStatus_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectNodeStatus(context.Background(), "pve")
	require.Error(t, err)
}

func TestPVE_collectGuestType_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectGuestType(context.Background(), "pve", "lxc")
	require.Error(t, err)
}

func TestPVE_collectGuestType_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectGuestType(context.Background(), "pve", "lxc")
	require.Error(t, err)
}

func TestPVE_collectGuestType_InvalidDataField(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data": "not an array"}`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectGuestType(context.Background(), "pve", "lxc")
	require.Error(t, err)
}

func TestPVE_collectDisks_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectDisks(context.Background(), "pve")
	require.Error(t, err)
}

func TestPVE_collectDisks_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectDisks(context.Background(), "pve")
	require.Error(t, err)
}

func TestPVE_collectDisks_InvalidDataField(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data": "not an array"}`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.collectDisks(context.Background(), "pve")
	require.Error(t, err)
}

func TestPVE_collectDisks_SMARTFails_SetsInternalError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/nodes/pve/disks/list" {
			fmt.Fprint(w, `{"data": [{"devpath": "/dev/sda", "model": "Test", "serial": "", "wwn": "wwn123", "size": 100, "type": "ssd"}]}`)
		} else {
			// SMART endpoint fails
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "smart error")
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disks, err := coll.collectDisks(context.Background(), "pve")
	require.NoError(t, err) // collectDisks itself doesn't fail
	require.Len(t, disks, 1)
	assert.Equal(t, 16, disks[0].Status) // StatusInternalError
}

func TestPVE_detectCluster_APIError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.detectCluster(context.Background())
	require.Error(t, err)
}

func TestPVE_detectCluster_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.detectCluster(context.Background())
	require.Error(t, err)
}

func TestPVE_detectCluster_InvalidDataField(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data": "not an array"}`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.detectCluster(context.Background())
	require.Error(t, err)
}

func TestPVE_Collect_DiscoverNodesFails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	err := coll.Collect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovering nodes")
}

func TestPVE_Collect_ClusterDetectionFails_FallsBack(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes":
			fmt.Fprint(w, `{"data": [{"node": "pve", "status": "online"}]}`)
		case "/api2/json/cluster/status":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "forbidden")
		case "/api2/json/nodes/pve/status":
			fmt.Fprint(w, nodeStatusJSON)
		case "/api2/json/nodes/pve/lxc":
			fmt.Fprint(w, lxcListJSON)
		case "/api2/json/nodes/pve/qemu":
			fmt.Fprint(w, qemuListJSON)
		default:
			http.Error(w, "not found", 404)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)
	coll.config.IsCluster = true
	// Force disk poll to be skipped
	coll.lastDiskPoll = time.Now()

	err := coll.Collect(context.Background())
	require.NoError(t, err)
	// Should fall back to instance name
	assert.Equal(t, "test-pve", coll.clusterID)
}

func TestPVE_apiGet_RetryableOnNetworkError(t *testing.T) {
	// Server that's immediately closed
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	ts.Close()

	c := cache.New()
	dbPath := t.TempDir() + "/test.db"
	s, err := store.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })

	pool := NewWorkerPool(4)
	cfg := PVEConfig{Name: "test", Host: ts.URL, TokenID: "t", TokenSecret: "s", PollInterval: 30 * time.Second, DiskPollInterval: 5 * time.Minute}
	coll := NewPVECollector(cfg, pool, c, s)

	_, err = coll.apiGet(context.Background(), "/test")
	require.Error(t, err)
	var re *RetryableError
	assert.ErrorAs(t, err, &re)
}

func TestPVE_collectSMART_InvalidJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `not json`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disk := &model.Disk{DevPath: "/dev/sda"}
	err := coll.collectSMART(context.Background(), "pve", disk)
	require.Error(t, err)
}

func TestPVE_collectSMART_InvalidDataField(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data": "not an object"}`)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disk := &model.Disk{DevPath: "/dev/sda"}
	err := coll.collectSMART(context.Background(), "pve", disk)
	require.Error(t, err)
}

func TestPVE_collectSMART_NullWearout(t *testing.T) {
	resp := `{"data": {"health": "PASSED", "type": "ata", "wearout": null, "attributes": []}}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, resp)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	disk := &model.Disk{DevPath: "/dev/sda"}
	err := coll.collectSMART(context.Background(), "pve", disk)
	require.NoError(t, err)
	assert.Nil(t, disk.Wearout)
}

func TestPVE_collectGuests_BothFail(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "error")
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	guests, err := coll.collectGuests(context.Background(), "pve")
	// collectGuests logs warnings but does not return error
	require.NoError(t, err)
	assert.Empty(t, guests)
}

func TestPVE_collectGuests_LXCFailsQemuSucceeds(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/nodes/pve/lxc" {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "lxc error")
		} else {
			fmt.Fprint(w, `{"data": [{"vmid": 100, "name": "vm1", "status": "running", "cpu": 0.5, "cpus": 4, "mem": 1024, "maxmem": 4096, "disk": 0, "maxdisk": 0, "netin": 0, "netout": 0, "uptime": 1000}]}`)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)
	coll.clusterID = "test"

	guests, err := coll.collectGuests(context.Background(), "pve")
	require.NoError(t, err)
	require.Len(t, guests, 1)
	assert.Equal(t, "qemu", guests[0].Type)
}

func TestPVE_Collect_NodeStatusFails_ContinuesOtherWork(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes":
			fmt.Fprint(w, `{"data": [{"node": "pve", "status": "online"}]}`)
		case "/api2/json/nodes/pve/status":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "error")
		case "/api2/json/nodes/pve/lxc":
			fmt.Fprint(w, lxcListJSON)
		case "/api2/json/nodes/pve/qemu":
			fmt.Fprint(w, qemuListJSON)
		default:
			http.Error(w, "not found", 404)
		}
	})
	coll, _, _, _ := newTestPVECollector(t, handler)
	coll.lastDiskPoll = time.Now()

	// Should not error — node status failure is logged but not fatal
	err := coll.Collect(context.Background())
	require.NoError(t, err)
}

func TestPVEApiGet_CancelledContext(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second) // simulate slow server
		w.WriteHeader(http.StatusOK)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := coll.apiGet(ctx, "/api2/json/nodes")
	assert.Error(t, err)
}

func TestPVEApiGet_Non200Status(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	coll, _, _, _ := newTestPVECollector(t, handler)

	_, err := coll.apiGet(context.Background(), "/api2/json/nodes")
	assert.Error(t, err)

	var apiErr *APIError
	assert.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}
