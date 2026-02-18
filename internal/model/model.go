// Package model defines all shared domain types for Glint.
package model

import "time"

// CPUInfo contains CPU hardware details for a node.
type CPUInfo struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"cpus"` // PVE calls threads "cpus"
	Sockets int    `json:"sockets"`
}

// MemUsage represents memory or swap usage.
type MemUsage struct {
	Used  int64 `json:"used"`
	Total int64 `json:"total"`
}

// DiskUsage represents filesystem usage.
type DiskUsage struct {
	Used  int64 `json:"used"`
	Total int64 `json:"total"`
}

// Node represents a discovered Proxmox VE node.
type Node struct {
	Instance    string     `json:"instance"`
	Name        string     `json:"name"`
	Status      string     `json:"status"` // "online", "offline"
	CPU         float64    `json:"cpu"`    // 0.0-1.0
	CPUInfo     CPUInfo    `json:"cpuinfo"`
	Memory      MemUsage   `json:"memory"`
	Swap        MemUsage   `json:"swap"`
	RootFS      DiskUsage  `json:"rootfs"`
	LoadAvg     [3]float64 `json:"loadavg"`
	Uptime      int64      `json:"uptime"`
	IOWait      float64    `json:"iowait"`
	PVEVersion  string     `json:"pveversion"`
	KernelVer   string     `json:"kversion"`
	Temperature *float64   `json:"temperature,omitempty"`
}

// Guest represents an LXC container or QEMU VM.
type Guest struct {
	Instance  string  `json:"instance"`
	Node      string  `json:"node"`
	ClusterID string  `json:"cluster_id"`
	Type      string  `json:"type"` // "lxc" or "qemu"
	VMID      int     `json:"vmid"`
	Name      string  `json:"name"`
	Status    string  `json:"status"` // "running", "stopped", "paused"
	CPU       float64 `json:"cpu"`    // fraction
	CPUs      int     `json:"cpus"`
	Mem       int64   `json:"mem"`
	MaxMem    int64   `json:"maxmem"`
	Disk      int64   `json:"disk"`
	MaxDisk   int64   `json:"maxdisk"`
	NetIn     int64   `json:"netin"`
	NetOut    int64   `json:"netout"`
	Uptime    int64   `json:"uptime"`
}

// SMART status bitfield values.
const (
	StatusPassed         = 0
	StatusFailedSmart    = 1
	StatusWarnScrutiny   = 2
	StatusFailedScrutiny = 4
	StatusUnknown        = 8
	StatusInternalError  = 16
)

// SMARTAttribute represents a single SMART attribute.
type SMARTAttribute struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Value       int64    `json:"value"`
	Worst       int64    `json:"worst"`
	Threshold   int64    `json:"threshold"`
	RawValue    int64    `json:"raw_value"`
	RawString   string   `json:"raw_string"`
	Status      int      `json:"status"`
	FailureRate *float64 `json:"failure_rate,omitempty"`
}

// Disk represents a physical disk with SMART data.
type Disk struct {
	Instance     string           `json:"instance"`
	Node         string           `json:"node"`
	WWN          string           `json:"wwn"`
	DevPath      string           `json:"dev_path"`
	Model        string           `json:"model"`
	Serial       string           `json:"serial"`
	DiskType     string           `json:"disk_type"` // "hdd", "ssd", "nvme"
	Protocol     string           `json:"protocol"`  // "ata", "nvme", "scsi"
	SizeBytes    int64            `json:"size_bytes"`
	Health       string           `json:"health"` // "PASSED", "FAILED"
	Status       int              `json:"status"` // bitfield
	Temperature  *int             `json:"temperature,omitempty"`
	PowerOnHours *int             `json:"power_on_hours,omitempty"`
	Wearout      *int             `json:"wearout,omitempty"`
	Attributes   []SMARTAttribute `json:"attributes,omitempty"`
	FirstSeen    time.Time        `json:"first_seen"`
	LastSeen     time.Time        `json:"last_seen"`
}

// Backup represents a PBS backup snapshot.
type Backup struct {
	PBSInstance string `json:"pbs_instance"`
	Datastore   string `json:"datastore"`
	BackupType  string `json:"backup_type"` // "ct", "vm", "host"
	BackupID    string `json:"backup_id"`   // guest VMID or hostname
	BackupTime  int64  `json:"backup_time"` // unix epoch
	SizeBytes   *int64 `json:"size_bytes,omitempty"`
	Verified    *bool  `json:"verified,omitempty"`
}

// PBSTask represents a PBS task (backup, verify, prune, gc).
type PBSTask struct {
	PBSInstance string `json:"pbs_instance"`
	UPID        string `json:"upid"`
	Type        string `json:"type"` // "backup", "verify", "prune", "garbage_collection"
	ID          string `json:"id"`   // VMID or datastore
	StartTime   int64  `json:"start_time"`
	EndTime     *int64 `json:"end_time,omitempty"`
	Status      string `json:"status"` // "OK", "WARNINGS:...", "Error:...", ""
	User        string `json:"user"`
}

// DatastoreStatus represents the usage status of a PBS datastore.
type DatastoreStatus struct {
	PBSInstance string   `json:"pbs_instance"`
	Name        string   `json:"name"`
	TotalBytes  *int64   `json:"total_bytes,omitempty"`
	UsedBytes   *int64   `json:"used_bytes,omitempty"`
	AvailBytes  *int64   `json:"avail_bytes,omitempty"`
	DedupRatio  *float64 `json:"dedup_ratio,omitempty"`
	EstFullDate *int64   `json:"est_full_date,omitempty"`
	Error       *string  `json:"error,omitempty"`
}

// NodeSnapshot is a time-series record of node metrics.
type NodeSnapshot struct {
	Timestamp  int64    `json:"ts"`
	Instance   string   `json:"instance"`
	Node       string   `json:"node"`
	CPUPct     float64  `json:"cpu_pct"`
	MemUsed    int64    `json:"mem_used"`
	MemTotal   int64    `json:"mem_total"`
	SwapUsed   int64    `json:"swap_used"`
	SwapTotal  int64    `json:"swap_total"`
	RootUsed   int64    `json:"rootfs_used"`
	RootTotal  int64    `json:"rootfs_total"`
	Load1m     float64  `json:"load_1m"`
	Load5m     float64  `json:"load_5m"`
	Load15m    float64  `json:"load_15m"`
	IOWait     float64  `json:"io_wait"`
	UptimeSecs int64    `json:"uptime_secs"`
	CPUTemp    *float64 `json:"cpu_temp,omitempty"`
}

// GuestSnapshot is a time-series record of guest metrics.
type GuestSnapshot struct {
	Timestamp int64   `json:"ts"`
	Instance  string  `json:"instance"`
	VMID      int     `json:"vmid"`
	Node      string  `json:"node"`
	ClusterID string  `json:"cluster_id"`
	GuestType string  `json:"guest_type"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPUPct    float64 `json:"cpu_pct"`
	CPUs      int     `json:"cpus"`
	MemUsed   int64   `json:"mem_used"`
	MemTotal  int64   `json:"mem_total"`
	DiskUsed  int64   `json:"disk_used"`
	DiskTotal int64   `json:"disk_total"`
	NetIn     int64   `json:"net_in"`
	NetOut    int64   `json:"net_out"`
}

// SparklinePoint is a single data point for sparkline rendering.
type SparklinePoint struct {
	Timestamp int64   `json:"ts"`
	Value     float64 `json:"value"`
}

// Notification represents a structured alert message.
type Notification struct {
	AlertType string            `json:"alert_type"`
	Severity  string            `json:"severity"` // "info", "warning", "critical"
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Instance  string            `json:"instance"`
	Subject   string            `json:"subject"`
	Timestamp time.Time         `json:"timestamp"`
	Resolved  bool              `json:"resolved"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
