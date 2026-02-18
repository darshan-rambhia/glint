package store

const schema = `
-- Registered PVE instances from config
CREATE TABLE IF NOT EXISTS pve_instances (
    name        TEXT PRIMARY KEY,
    host        TEXT NOT NULL,
    is_cluster  INTEGER NOT NULL DEFAULT 0,
    cluster_id  TEXT
);

-- Discovered nodes (refreshed on each poll)
CREATE TABLE IF NOT EXISTS nodes (
    instance    TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL,
    cpu_model   TEXT,
    cpu_cores   INTEGER,
    cpu_threads INTEGER,
    cpu_sockets INTEGER,
    pve_version TEXT,
    kernel_ver  TEXT,
    PRIMARY KEY (instance, name),
    FOREIGN KEY (instance) REFERENCES pve_instances(name)
);

-- Registered PBS instances from config
CREATE TABLE IF NOT EXISTS pbs_instances (
    name        TEXT PRIMARY KEY,
    host        TEXT NOT NULL
);

-- Known disks (identified by WWN, persistent across reboots)
CREATE TABLE IF NOT EXISTS disks (
    wwn         TEXT PRIMARY KEY,
    instance    TEXT NOT NULL,
    node        TEXT NOT NULL,
    dev_path    TEXT NOT NULL,
    model       TEXT,
    serial      TEXT,
    disk_type   TEXT NOT NULL,
    protocol    TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    first_seen  INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL
);

-- Host/node metrics (48h retention)
CREATE TABLE IF NOT EXISTS node_snapshots (
    ts          INTEGER NOT NULL,
    instance    TEXT NOT NULL,
    node        TEXT NOT NULL,
    cpu_pct     REAL    NOT NULL,
    mem_used    INTEGER NOT NULL,
    mem_total   INTEGER NOT NULL,
    swap_used   INTEGER NOT NULL,
    swap_total  INTEGER NOT NULL,
    rootfs_used INTEGER NOT NULL,
    rootfs_total INTEGER NOT NULL,
    load_1m     REAL    NOT NULL,
    load_5m     REAL    NOT NULL,
    load_15m    REAL    NOT NULL,
    io_wait     REAL    NOT NULL,
    uptime_secs INTEGER NOT NULL,
    cpu_temp    REAL,
    PRIMARY KEY (ts, instance, node)
) WITHOUT ROWID;

-- Guest metrics (48h retention)
CREATE TABLE IF NOT EXISTS guest_snapshots (
    ts          INTEGER NOT NULL,
    instance    TEXT NOT NULL,
    vmid        INTEGER NOT NULL,
    node        TEXT NOT NULL,
    cluster_id  TEXT,
    guest_type  TEXT NOT NULL,
    name        TEXT    NOT NULL,
    status      TEXT    NOT NULL,
    cpu_pct     REAL    NOT NULL,
    cpus        INTEGER NOT NULL,
    mem_used    INTEGER NOT NULL,
    mem_total   INTEGER NOT NULL,
    disk_used   INTEGER NOT NULL,
    disk_total  INTEGER NOT NULL,
    net_in      INTEGER NOT NULL,
    net_out     INTEGER NOT NULL,
    PRIMARY KEY (ts, instance, vmid)
) WITHOUT ROWID;

-- Disk SMART snapshots (30d retention)
CREATE TABLE IF NOT EXISTS smart_snapshots (
    ts              INTEGER NOT NULL,
    wwn             TEXT    NOT NULL,
    health          TEXT    NOT NULL,
    status          INTEGER NOT NULL,
    temperature     INTEGER,
    power_on_hours  INTEGER,
    wearout         INTEGER,
    attributes_json TEXT,
    PRIMARY KEY (ts, wwn)
) WITHOUT ROWID;

-- PBS backup snapshots (7d retention)
CREATE TABLE IF NOT EXISTS backup_snapshots (
    ts             INTEGER NOT NULL,
    pbs_instance   TEXT    NOT NULL,
    datastore      TEXT    NOT NULL,
    backup_type    TEXT    NOT NULL,
    backup_id      TEXT    NOT NULL,
    backup_time    INTEGER NOT NULL,
    size_bytes     INTEGER,
    verified       INTEGER,
    PRIMARY KEY (ts, pbs_instance, backup_id, backup_time)
) WITHOUT ROWID;

-- PBS datastore usage (7d retention)
CREATE TABLE IF NOT EXISTS datastore_snapshots (
    ts              INTEGER NOT NULL,
    pbs_instance    TEXT    NOT NULL,
    store_name      TEXT    NOT NULL,
    total_bytes     INTEGER,
    used_bytes      INTEGER,
    avail_bytes     INTEGER,
    dedup_ratio     REAL,
    est_full_date   INTEGER,
    PRIMARY KEY (ts, pbs_instance, store_name)
) WITHOUT ROWID;

-- Alert log (30d retention)
CREATE TABLE IF NOT EXISTS alert_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts          INTEGER NOT NULL,
    alert_type  TEXT    NOT NULL,
    instance    TEXT,
    subject     TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    severity    TEXT    NOT NULL
);

-- Secondary indexes
CREATE INDEX IF NOT EXISTS idx_guest_vmid ON guest_snapshots(instance, vmid, ts);
CREATE INDEX IF NOT EXISTS idx_smart_wwn ON smart_snapshots(wwn, ts);
CREATE INDEX IF NOT EXISTS idx_alert_ts ON alert_log(ts);
`
