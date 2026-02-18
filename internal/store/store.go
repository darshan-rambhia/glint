// Package store provides SQLite persistence for Glint.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/darshan-rambhia/glint/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

// Store wraps a SQLite database for Glint data persistence.
type Store struct {
	db *sql.DB
}

// New opens or creates a SQLite database at the given path and runs migrations.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbPath, err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// InsertNodeSnapshot records a point-in-time node metric snapshot.
func (s *Store) InsertNodeSnapshot(snap model.NodeSnapshot) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO node_snapshots
		(ts, instance, node, cpu_pct, mem_used, mem_total, swap_used, swap_total,
		 rootfs_used, rootfs_total, load_1m, load_5m, load_15m, io_wait, uptime_secs, cpu_temp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.Timestamp, snap.Instance, snap.Node, snap.CPUPct,
		snap.MemUsed, snap.MemTotal, snap.SwapUsed, snap.SwapTotal,
		snap.RootUsed, snap.RootTotal, snap.Load1m, snap.Load5m, snap.Load15m,
		snap.IOWait, snap.UptimeSecs, snap.CPUTemp,
	)
	if err != nil {
		return fmt.Errorf("inserting node snapshot: %w", err)
	}
	return nil
}

// InsertGuestSnapshot records a point-in-time guest metric snapshot.
func (s *Store) InsertGuestSnapshot(snap model.GuestSnapshot) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO guest_snapshots
		(ts, instance, vmid, node, cluster_id, guest_type, name, status,
		 cpu_pct, cpus, mem_used, mem_total, disk_used, disk_total, net_in, net_out)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.Timestamp, snap.Instance, snap.VMID, snap.Node, snap.ClusterID,
		snap.GuestType, snap.Name, snap.Status, snap.CPUPct, snap.CPUs,
		snap.MemUsed, snap.MemTotal, snap.DiskUsed, snap.DiskTotal,
		snap.NetIn, snap.NetOut,
	)
	if err != nil {
		return fmt.Errorf("inserting guest snapshot: %w", err)
	}
	return nil
}

// InsertSMARTSnapshot records a disk SMART snapshot.
func (s *Store) InsertSMARTSnapshot(ts int64, disk *model.Disk) error {
	attrsJSON, err := json.Marshal(disk.Attributes)
	if err != nil {
		return fmt.Errorf("marshaling SMART attributes: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO smart_snapshots
		(ts, wwn, health, status, temperature, power_on_hours, wearout, attributes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, disk.WWN, disk.Health, disk.Status,
		disk.Temperature, disk.PowerOnHours, disk.Wearout, string(attrsJSON),
	)
	if err != nil {
		return fmt.Errorf("inserting SMART snapshot: %w", err)
	}
	return nil
}

// InsertBackupSnapshot records a PBS backup snapshot.
func (s *Store) InsertBackupSnapshot(ts int64, b *model.Backup) error {
	var verified *int
	if b.Verified != nil {
		v := 0
		if *b.Verified {
			v = 1
		}
		verified = &v
	}
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO backup_snapshots
		(ts, pbs_instance, datastore, backup_type, backup_id, backup_time, size_bytes, verified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, b.PBSInstance, b.Datastore, b.BackupType, b.BackupID,
		b.BackupTime, b.SizeBytes, verified,
	)
	if err != nil {
		return fmt.Errorf("inserting backup snapshot: %w", err)
	}
	return nil
}

// InsertDatastoreSnapshot records a PBS datastore usage snapshot.
func (s *Store) InsertDatastoreSnapshot(ts int64, ds *model.DatastoreStatus) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO datastore_snapshots
		(ts, pbs_instance, store_name, total_bytes, used_bytes, avail_bytes, dedup_ratio, est_full_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, ds.PBSInstance, ds.Name, ds.TotalBytes, ds.UsedBytes,
		ds.AvailBytes, ds.DedupRatio, ds.EstFullDate,
	)
	if err != nil {
		return fmt.Errorf("inserting datastore snapshot: %w", err)
	}
	return nil
}

// InsertAlert logs an alert.
func (s *Store) InsertAlert(ts int64, alertType, instance, subject, message, severity string) error {
	_, err := s.db.Exec(`
		INSERT INTO alert_log (ts, alert_type, instance, subject, message, severity)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ts, alertType, instance, subject, message, severity,
	)
	if err != nil {
		return fmt.Errorf("inserting alert: %w", err)
	}
	return nil
}

// UpsertDisk inserts or updates a disk metadata record.
func (s *Store) UpsertDisk(d *model.Disk) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`
		INSERT INTO disks (wwn, instance, node, dev_path, model, serial, disk_type, protocol, size_bytes, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(wwn) DO UPDATE SET
			instance = excluded.instance,
			node = excluded.node,
			dev_path = excluded.dev_path,
			model = excluded.model,
			serial = excluded.serial,
			last_seen = excluded.last_seen`,
		d.WWN, d.Instance, d.Node, d.DevPath, d.Model, d.Serial,
		d.DiskType, d.Protocol, d.SizeBytes, now, now,
	)
	if err != nil {
		return fmt.Errorf("upserting disk %s: %w", d.WWN, err)
	}
	return nil
}

// QueryNodeSparkline returns CPU or memory data points for sparkline rendering.
func (s *Store) QueryNodeSparkline(instance, node, metric string, since int64) ([]model.SparklinePoint, error) {
	var col string
	switch metric {
	case "cpu":
		col = "cpu_pct"
	case "memory":
		col = "CAST(mem_used AS REAL) / mem_total * 100"
	default:
		return nil, fmt.Errorf("unknown metric %q", metric)
	}

	query := fmt.Sprintf(`
		SELECT ts, %s FROM node_snapshots
		WHERE instance = ? AND node = ? AND ts >= ?
		ORDER BY ts ASC`, col)

	rows, err := s.db.Query(query, instance, node, since)
	if err != nil {
		return nil, fmt.Errorf("querying node sparkline: %w", err)
	}
	defer rows.Close()

	var points []model.SparklinePoint
	for rows.Next() {
		var p model.SparklinePoint
		if err := rows.Scan(&p.Timestamp, &p.Value); err != nil {
			return nil, fmt.Errorf("scanning sparkline point: %w", err)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// QueryGuestSparkline returns CPU data points for a specific guest.
func (s *Store) QueryGuestSparkline(instance string, vmid int, since int64) ([]model.SparklinePoint, error) {
	rows, err := s.db.Query(`
		SELECT ts, cpu_pct FROM guest_snapshots
		WHERE instance = ? AND vmid = ? AND ts >= ?
		ORDER BY ts ASC`, instance, vmid, since)
	if err != nil {
		return nil, fmt.Errorf("querying guest sparkline: %w", err)
	}
	defer rows.Close()

	var points []model.SparklinePoint
	for rows.Next() {
		var p model.SparklinePoint
		if err := rows.Scan(&p.Timestamp, &p.Value); err != nil {
			return nil, fmt.Errorf("scanning sparkline point: %w", err)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// UpsertPVEInstance inserts or updates a PVE instance record.
func (s *Store) UpsertPVEInstance(name, host string, isCluster bool, clusterID string) error {
	ic := 0
	if isCluster {
		ic = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO pve_instances (name, host, is_cluster, cluster_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			host = excluded.host,
			is_cluster = excluded.is_cluster,
			cluster_id = excluded.cluster_id`,
		name, host, ic, clusterID,
	)
	if err != nil {
		return fmt.Errorf("upserting PVE instance %s: %w", name, err)
	}
	return nil
}

// UpsertPBSInstance inserts or updates a PBS instance record.
func (s *Store) UpsertPBSInstance(name, host string) error {
	_, err := s.db.Exec(`
		INSERT INTO pbs_instances (name, host)
		VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET host = excluded.host`,
		name, host,
	)
	if err != nil {
		return fmt.Errorf("upserting PBS instance %s: %w", name, err)
	}
	return nil
}
