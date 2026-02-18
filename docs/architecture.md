# Architecture

Glint is a single Go binary that polls Proxmox APIs, caches results in memory, persists history to SQLite, and serves a server-rendered dashboard via htmx.

## System Overview

```
                      Browser (htmx polling every 15s)
                              |
                        HTTPS (reverse proxy)
                              |
                  +-----------+-----------+
                  |         Glint         |
                  |   Go binary (Docker)  |
                  |                       |
                  |  templ HTML handlers  |
                  |         |             |
                  |  Collector Engine     |
                  |  (worker pool)        |
                  |         |             |
                  |  In-Memory Cache      |
                  |  (sync.RWMutex)       |
                  |         |             |
                  |  SQLite               |
                  |  (metadata + history) |
                  +-----------+-----------+
                   /          |          \
        PVE Instance 1   PVE Instance 2   PBS Instance 1
        ├─ Node A        ├─ Node C        ├─ Datastore X
        │  ├─ CTs/VMs    │  ├─ CTs/VMs    └─ Datastore Y
        │  └─ Disks      │  └─ Disks
        └─ Node B        └─ Node D
                                              ntfy (optional)
```

---

## Data Flow

1. **Collector goroutines** (one per PVE/PBS instance) submit API calls to a **bounded worker pool**
2. Each collector updates the **in-memory cache** and appends snapshots to **SQLite**
3. **HTTP handlers** read from cache snapshots (lock-free for consumers)
4. **htmx** polls fragment endpoints every 15s, swapping HTML in-place
5. **Alerter goroutine** evaluates cache state against rules, sends to ntfy with deduplication

---

## Key Design Decisions

### From Pulse

- **Instance + Node hierarchy** --- each PVE config entry is an "instance" (standalone or cluster). Nodes are auto-discovered via `GET /nodes`. Guests are scoped by cluster name to prevent duplicates.
- **Per-instance collectors with worker pool** --- all API calls go through a bounded worker pool (`chan struct{}` semaphore, default 4 workers) to cap memory and concurrent HTTP connections.
- **Snapshot-based cache** --- the cache exposes deep-copy snapshots so HTTP handlers never hold locks. Uses `sync.RWMutex` (not `sync.Map`) for atomic cross-key consistency.

### From Scrutiny

- **WWN-based disk identity** --- disks are identified by World Wide Name (globally unique, survives reboots and cable changes), not `/dev/sdX`.
- **Backblaze-derived SMART thresholds** --- individual attributes are evaluated against real-world failure rate data from Backblaze's public hard drive statistics.
- **Protocol-aware SMART parsing** --- ATA, NVMe, and SCSI each have different data models, parsed separately.

---

## Project Structure

```
cmd/glint/main.go              Entry point, wiring, signal handling
internal/
  api/                         HTTP handlers + htmx fragments
    handlers.go                Route registration + fragment handlers
    middleware.go              Logging, recovery
  collector/                   Data collection from Proxmox APIs
    collector.go               Collector interface + runner loop
    backup.go                  BackupCollector interface
    errors.go                  RetryableError, behavior-based error types
    pve.go                     PVE client (nodes, guests, disks, SMART)
    pbs.go                     PBS client (datastores, snapshots, tasks)
    temperature.go             Optional SSH-based temp polling
  smart/                       S.M.A.R.T. health assessment
    evaluate.go                Attribute status evaluation
    thresholds.go              Backblaze failure rate lookup tables
    ata.go                     ATA attribute parsing
    nvme.go                    NVMe text field parsing
  store/                       SQLite persistence
    store.go                   Repository (insert, query, migrate)
    migrations.go              Embedded schema
    pruner.go                  Retention cleanup
  cache/                       Thread-safe in-memory state
    cache.go                   Multi-instance cache with snapshots
  alerter/                     Alert rule engine
    alerter.go                 Rule evaluation + deduplication
    templates.go               Default message templates
  notify/                      Notification providers
    provider.go                Provider interface + Notification struct
    ntfy.go                    ntfy provider
    webhook.go                 Generic webhook provider
  config/                      Configuration loading
    config.go                  YAML + env var parsing
  model/                       Shared domain types
    model.go                   Node, Guest, Disk, Backup, etc.
templates/                     templ HTML templates
  layout.templ                 Base HTML shell
  dashboard.templ              Main page
  nodes.templ                  Node cards
  guests.templ                 Guest table
  backups.templ                PBS backup panel
  disks.templ                  Disk health table
  disk_detail.templ            Expanded SMART attributes
  components/                  Reusable UI components
static/                        CSS + htmx.min.js
```

---

## Collector Engine

Each PVE/PBS instance gets its own collector goroutine. All collectors share a single worker pool that bounds concurrent API calls.

### Worker Pool

```go
type WorkerPool struct {
    sem chan struct{}  // buffered channel as semaphore
}
```

Default: 4 workers. Configurable via `worker_pool_size`. When polling 4 nodes in parallel, only 4 HTTP requests fly at once --- prevents unbounded goroutine spawning.

### PVE Poll Cycle

```
1. GET /nodes → discover/update node list
2. For each online node (fan out via worker pool):
   a. GET /nodes/{node}/status → host metrics
   b. GET /nodes/{node}/lxc → containers
   c. GET /nodes/{node}/qemu → VMs
   d. If disk poll due (>1h since last):
      - GET /nodes/{node}/disks/list → disk inventory
      - For each disk: GET /nodes/{node}/disks/smart → SMART data
3. Merge results, dedup guests by cluster_id
4. Update cache + write to SQLite
```

### PBS Poll Cycle

```
1. GET /status/datastore-usage → all datastores
2. For each monitored datastore:
   a. GET /admin/datastore/{store}/snapshots → backup snapshots
3. GET /nodes/localhost/tasks → recent tasks
4. Update cache + write to SQLite
```

### Polling Intervals

| Data | Interval | Notes |
|------|----------|-------|
| Node + guest metrics | 15s | Fan out across nodes in parallel |
| S.M.A.R.T. disk data | 1h | Slow operation (1-5s per disk) |
| PBS backups + tasks | 5m | |
| Node discovery | 5m | Within PVE collector |
| SSH temperatures | 60s | Graceful fallback if unavailable |

---

## In-Memory Cache

The cache stores the latest state from all collectors. HTTP handlers read **snapshots** (deep copies) so they never hold locks during template rendering.

```go
type Cache struct {
    mu sync.RWMutex

    Nodes      map[string]map[string]*Node        // [instance][node]
    Guests     map[string]map[int]*Guest           // [cluster_id][vmid]
    Disks      map[string]*Disk                    // [wwn]
    Datastores map[string]map[string]*DatastoreStatus
    Backups    map[string]map[string]*Backup
    Tasks      map[string][]*PBSTask
    LastPoll   map[string]time.Time
}
```

Why `sync.RWMutex` over `sync.Map`: rendering a dashboard page requires consistent data across nodes, guests, and disks simultaneously. `sync.Map` provides no cross-key consistency guarantees.

---

## SQLite Schema

### Time-Series Tables

All time-series tables use `WITHOUT ROWID` with `ts`-leading composite primary keys. This makes them clustered B-trees ordered by time --- ideal for range queries and pruning.

| Table | Retention | Primary Key |
|-------|-----------|-------------|
| `node_snapshots` | 48h | `(ts, instance, node)` |
| `guest_snapshots` | 48h | `(ts, instance, vmid)` |
| `smart_snapshots` | 30d | `(ts, wwn)` |
| `backup_snapshots` | 7d | `(ts, pbs_instance, backup_id, backup_time)` |
| `datastore_snapshots` | 7d | `(ts, pbs_instance, store_name)` |
| `alert_log` | 30d | `(id)` autoincrement |

### Pruner

An hourly goroutine deletes rows older than the retention period. The `ts`-leading PK means `DELETE FROM X WHERE ts < ?` is a fast range scan on the clustered index.

---

## S.M.A.R.T. Health Assessment

Disk health uses a multi-level evaluation inspired by Scrutiny:

### Status Bitfield

| Bit | Value | Meaning |
|-----|-------|---------|
| 0 | `StatusPassed` | All checks passed |
| 1 | `StatusFailedSmart` | Manufacturer SMART says `FAILING_NOW` |
| 2 | `StatusWarnScrutiny` | Backblaze data suggests elevated risk |
| 4 | `StatusFailedScrutiny` | Backblaze data suggests high failure probability |
| 8 | `StatusUnknown` | Disk disappeared / unreachable |
| 16 | `StatusInternalError` | Parse failure, API timeout |

### Evaluation Order

1. Manufacturer SMART says `FAILING_NOW` → `StatusFailedSmart`
2. `IN_THE_PAST` → `StatusWarnScrutiny`
3. Look up raw value in Backblaze thresholds:
    - Critical attribute (IDs 5, 10, 187, 188, 196, 197, 198) + failure rate >= 10% → `StatusFailedScrutiny`
    - Non-critical + failure rate >= 20% → `StatusFailedScrutiny`
    - Non-critical + failure rate >= 10% → `StatusWarnScrutiny`
4. Device status = bitwise OR of all attribute statuses

### NVMe Handling

NVMe drives don't return ATA-style attributes. Glint parses the raw `smartctl` text output for NVMe-specific fields (`critical_warning`, `available_spare`, `percentage_used`, `media_errors`, etc.) and applies NVMe-specific thresholds.

---

## HTTP Routes

| Route | Type | Refresh | Description |
|-------|------|---------|-------------|
| `GET /` | Full page | --- | Dashboard shell |
| `GET /fragments/nodes` | htmx | 15s | All node cards with sparklines |
| `GET /fragments/guests` | htmx | 15s | Guest table (all instances) |
| `GET /fragments/backups` | htmx | 60s | PBS backup status + tasks |
| `GET /fragments/disks` | htmx | 300s | S.M.A.R.T. health (all nodes) |
| `GET /fragments/disk/{wwn}` | htmx | on-click | Expanded attributes for one disk |
| `GET /api/sparkline/node/{instance}/{node}` | JSON | on-demand | Node sparkline data |
| `GET /api/sparkline/guest/{instance}/{vmid}` | JSON | on-demand | Guest sparkline data |
| `GET /healthz` | JSON | --- | Health check |

---

## Alerting

The alerter goroutine evaluates the cache state against configured rules on each poll cycle. Alerts are deduplicated with per-rule cooldowns to prevent notification storms.

### Built-in Rules

| Alert | Default Condition | Cooldown |
|-------|-------------------|----------|
| Node CPU high | > 90% for 5min | 1h |
| Node offline | status != "online" | 30min |
| Guest down | not running for 2min | 30min |
| Backup stale | last backup > 36h | 6h |
| Backup failed | PBS task error | 1h |
| Disk SMART failed | manufacturer failure | 6h |
| Datastore full | > 85% used | 6h |

### Notification Providers

Providers implement a common interface:

```go
type Provider interface {
    Name() string
    Send(ctx context.Context, n Notification) error
}
```

Built-in providers: **ntfy** (with priority mapping and tags) and **webhook** (generic JSON POST to any URL).

---

## Lifecycle Management

All goroutines accept a `context.Context` and exit when cancelled. The main function uses `errgroup` to manage the lifecycle:

```go
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return pveCollector.Run(ctx) })
g.Go(func() error { return pbsCollector.Run(ctx) })
g.Go(func() error { return pruner.Run(ctx) })
g.Go(func() error { return server.Run(ctx) })
```

Graceful shutdown on `SIGINT`/`SIGTERM`: collectors stop polling, in-flight requests complete, SQLite is closed cleanly.
