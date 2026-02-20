package cache

import (
	"maps"
	"sync"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
)

// Cache is a thread-safe in-memory store for all polled data.
type Cache struct {
	mu sync.RWMutex

	Nodes      map[string]map[string]*model.Node
	Guests     map[string]map[int]*model.Guest
	Disks      map[string]*model.Disk
	Datastores map[string]map[string]*model.DatastoreStatus
	Backups    map[string]map[string]*model.Backup
	Tasks      map[string][]*model.PBSTask
	LastPoll   map[string]time.Time
}

// CacheSnapshot is a read-only deep copy of the cache state.
type CacheSnapshot struct {
	Nodes      map[string]map[string]*model.Node
	Guests     map[string]map[int]*model.Guest
	Disks      map[string]*model.Disk
	Datastores map[string]map[string]*model.DatastoreStatus
	Backups    map[string]map[string]*model.Backup
	Tasks      map[string][]*model.PBSTask
	LastPoll   map[string]time.Time
}

// New returns an initialized Cache.
func New() *Cache {
	return &Cache{
		Nodes:      make(map[string]map[string]*model.Node),
		Guests:     make(map[string]map[int]*model.Guest),
		Disks:      make(map[string]*model.Disk),
		Datastores: make(map[string]map[string]*model.DatastoreStatus),
		Backups:    make(map[string]map[string]*model.Backup),
		Tasks:      make(map[string][]*model.PBSTask),
		LastPoll:   make(map[string]time.Time),
	}
}

// Snapshot returns a deep copy of the cache contents.
func (c *Cache) Snapshot() CacheSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snap := CacheSnapshot{
		Nodes:      make(map[string]map[string]*model.Node, len(c.Nodes)),
		Guests:     make(map[string]map[int]*model.Guest, len(c.Guests)),
		Disks:      make(map[string]*model.Disk, len(c.Disks)),
		Datastores: make(map[string]map[string]*model.DatastoreStatus, len(c.Datastores)),
		Backups:    make(map[string]map[string]*model.Backup, len(c.Backups)),
		Tasks:      make(map[string][]*model.PBSTask, len(c.Tasks)),
		LastPoll:   make(map[string]time.Time, len(c.LastPoll)),
	}

	for inst, nodes := range c.Nodes {
		m := make(map[string]*model.Node, len(nodes))
		for k, v := range nodes {
			cp := *v
			m[k] = &cp
		}
		snap.Nodes[inst] = m
	}

	for cid, guests := range c.Guests {
		m := make(map[int]*model.Guest, len(guests))
		for k, v := range guests {
			cp := *v
			m[k] = &cp
		}
		snap.Guests[cid] = m
	}

	for wwn, d := range c.Disks {
		cp := *d
		if d.Attributes != nil {
			cp.Attributes = make([]model.SMARTAttribute, len(d.Attributes))
			copy(cp.Attributes, d.Attributes)
		}
		snap.Disks[wwn] = &cp
	}

	for inst, stores := range c.Datastores {
		m := make(map[string]*model.DatastoreStatus, len(stores))
		for k, v := range stores {
			cp := *v
			m[k] = &cp
		}
		snap.Datastores[inst] = m
	}

	for inst, backups := range c.Backups {
		m := make(map[string]*model.Backup, len(backups))
		for k, v := range backups {
			cp := *v
			m[k] = &cp
		}
		snap.Backups[inst] = m
	}

	for inst, tasks := range c.Tasks {
		sl := make([]*model.PBSTask, len(tasks))
		for i, t := range tasks {
			cp := *t
			sl[i] = &cp
		}
		snap.Tasks[inst] = sl
	}

	maps.Copy(snap.LastPoll, c.LastPoll)

	return snap
}

// UpdateNodes replaces all nodes for the given instance.
func (c *Cache) UpdateNodes(instance string, nodes map[string]*model.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Nodes[instance] = nodes
}

// UpdateGuests replaces all guests for the given cluster ID.
func (c *Cache) UpdateGuests(clusterID string, guests map[int]*model.Guest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Guests[clusterID] = guests
}

// UpdateDisks merges disk updates into the cache without removing existing disks
// that were not included in this poll.
func (c *Cache) UpdateDisks(disks map[string]*model.Disk) {
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.Copy(c.Disks, disks)
}

// UpdateDatastores replaces all datastores for the given PBS instance.
func (c *Cache) UpdateDatastores(pbsInstance string, datastores map[string]*model.DatastoreStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Datastores[pbsInstance] = datastores
}

// UpdateBackups replaces all backups for the given PBS instance.
func (c *Cache) UpdateBackups(pbsInstance string, backups map[string]*model.Backup) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Backups[pbsInstance] = backups
}

// UpdateTasks replaces all tasks for the given PBS instance.
func (c *Cache) UpdateTasks(pbsInstance string, tasks []*model.PBSTask) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Tasks[pbsInstance] = tasks
}

// UpdateNodeTemperature updates the temperature for a specific node.
func (c *Cache) UpdateNodeTemperature(instance, node string, temp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if nodes, ok := c.Nodes[instance]; ok {
		if n, ok := nodes[node]; ok {
			n.Temperature = &temp
		}
	}
}

// SetLastPoll records the last poll time for a collector.
func (c *Cache) SetLastPoll(collectorID string, t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastPoll[collectorID] = t
}

// NodeData is an alias for convenience when used externally.
type NodeData = model.Node
