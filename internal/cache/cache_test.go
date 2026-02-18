package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	c := New()
	assert.NotNil(t, c.Nodes)
	assert.NotNil(t, c.Guests)
	assert.NotNil(t, c.Disks)
	assert.NotNil(t, c.Datastores)
	assert.NotNil(t, c.Backups)
	assert.NotNil(t, c.Tasks)
	assert.NotNil(t, c.LastPoll)
}

func TestUpdateNodes(t *testing.T) {
	c := New()
	nodes := map[string]*model.Node{
		"pve1": {Instance: "main", Name: "pve1", Status: "online", CPU: 0.25},
	}
	c.UpdateNodes("main", nodes)

	snap := c.Snapshot()
	assert.Len(t, snap.Nodes["main"], 1)
	assert.Equal(t, "pve1", snap.Nodes["main"]["pve1"].Name)
	assert.Equal(t, 0.25, snap.Nodes["main"]["pve1"].CPU)
}

func TestUpdateGuests(t *testing.T) {
	c := New()
	guests := map[int]*model.Guest{
		101: {VMID: 101, Name: "network-services", Status: "running"},
		304: {VMID: 304, Name: "immich", Status: "running"},
	}
	c.UpdateGuests("main", guests)

	snap := c.Snapshot()
	assert.Len(t, snap.Guests["main"], 2)
	assert.Equal(t, "immich", snap.Guests["main"][304].Name)
}

func TestUpdateDisksMerge(t *testing.T) {
	c := New()
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-1": {WWN: "wwn-1", Model: "Samsung 870"},
		"wwn-2": {WWN: "wwn-2", Model: "WD Red"},
	})
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-1": {WWN: "wwn-1", Model: "Samsung 870 EVO"},
	})

	snap := c.Snapshot()
	assert.Len(t, snap.Disks, 2)
	assert.Equal(t, "Samsung 870 EVO", snap.Disks["wwn-1"].Model)
	assert.Equal(t, "WD Red", snap.Disks["wwn-2"].Model)
}

func TestUpdateDatastores(t *testing.T) {
	c := New()
	total := int64(1000000)
	used := int64(500000)
	ds := map[string]*model.DatastoreStatus{
		"backup1": {Name: "backup1", TotalBytes: &total, UsedBytes: &used},
	}
	c.UpdateDatastores("pbs1", ds)

	snap := c.Snapshot()
	assert.Len(t, snap.Datastores["pbs1"], 1)
	assert.Equal(t, int64(500000), *snap.Datastores["pbs1"]["backup1"].UsedBytes)
}

func TestUpdateBackups(t *testing.T) {
	c := New()
	backups := map[string]*model.Backup{
		"101": {BackupID: "101", BackupType: "ct", BackupTime: 1700000000},
	}
	c.UpdateBackups("pbs1", backups)

	snap := c.Snapshot()
	assert.Equal(t, "ct", snap.Backups["pbs1"]["101"].BackupType)
}

func TestUpdateTasks(t *testing.T) {
	c := New()
	tasks := []*model.PBSTask{
		{UPID: "u1", Type: "backup", Status: "OK"},
		{UPID: "u2", Type: "verify", Status: "OK"},
	}
	c.UpdateTasks("pbs1", tasks)

	snap := c.Snapshot()
	assert.Len(t, snap.Tasks["pbs1"], 2)
}

func TestSetLastPoll(t *testing.T) {
	c := New()
	now := time.Now()
	c.SetLastPoll("pve-poller", now)

	snap := c.Snapshot()
	assert.Equal(t, now, snap.LastPoll["pve-poller"])
}

func TestSnapshotIsIndependent(t *testing.T) {
	c := New()
	c.UpdateNodes("main", map[string]*model.Node{
		"pve1": {Name: "pve1", CPU: 0.10},
	})

	snap := c.Snapshot()

	// Mutate the cache after taking the snapshot.
	c.UpdateNodes("main", map[string]*model.Node{
		"pve1": {Name: "pve1", CPU: 0.99},
		"pve2": {Name: "pve2", CPU: 0.50},
	})

	// Snapshot must be unchanged.
	assert.Len(t, snap.Nodes["main"], 1)
	assert.Equal(t, 0.10, snap.Nodes["main"]["pve1"].CPU)
}

func TestSnapshotDeepCopyDisk(t *testing.T) {
	c := New()
	c.UpdateDisks(map[string]*model.Disk{
		"wwn-1": {
			WWN:        "wwn-1",
			Attributes: []model.SMARTAttribute{{ID: 5, Name: "Reallocated", Value: 100}},
		},
	})

	snap := c.Snapshot()

	// Mutate the original disk attributes.
	c.mu.Lock()
	c.Disks["wwn-1"].Attributes[0].Value = 0
	c.mu.Unlock()

	// Snapshot must retain the original value.
	assert.Equal(t, int64(100), snap.Disks["wwn-1"].Attributes[0].Value)
}

func TestUpdateNodeTemperature(t *testing.T) {
	c := New()
	c.UpdateNodes("main", map[string]*model.Node{
		"pve1": {Instance: "main", Name: "pve1", Status: "online", CPU: 0.25},
	})

	c.UpdateNodeTemperature("main", "pve1", 45.5)

	snap := c.Snapshot()
	require.NotNil(t, snap.Nodes["main"]["pve1"].Temperature)
	assert.Equal(t, 45.5, *snap.Nodes["main"]["pve1"].Temperature)
}

func TestUpdateNodeTemperature_NonExistentInstance(t *testing.T) {
	c := New()
	// Should not panic when instance doesn't exist
	c.UpdateNodeTemperature("nonexistent", "pve1", 45.5)
}

func TestUpdateNodeTemperature_NonExistentNode(t *testing.T) {
	c := New()
	c.UpdateNodes("main", map[string]*model.Node{
		"pve1": {Instance: "main", Name: "pve1", Status: "online"},
	})

	// Should not panic when node doesn't exist within a valid instance
	c.UpdateNodeTemperature("main", "nonexistent", 45.5)
}

func TestConcurrentReadWrite(t *testing.T) {
	c := New()
	var wg sync.WaitGroup

	// Writers
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			nodes := map[string]*model.Node{
				"pve1": {Name: "pve1", CPU: float64(n) / 10.0},
			}
			c.UpdateNodes("main", nodes)
			c.UpdateGuests("main", map[int]*model.Guest{
				100 + n: {VMID: 100 + n, Name: "test"},
			})
			c.SetLastPoll("writer", time.Now())
		}(i)
	}

	// Readers
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap := c.Snapshot()
			// Just access fields to trigger any race.
			_ = len(snap.Nodes)
			_ = len(snap.Guests)
			_ = len(snap.LastPoll)
		}()
	}

	wg.Wait()
}
