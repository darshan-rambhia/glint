package collector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/smart"
	"github.com/darshan-rambhia/glint/internal/store"
)

// PVEConfig holds configuration for a single PVE instance.
type PVEConfig struct {
	Name             string
	Host             string
	TokenID          string
	TokenSecret      string
	Insecure         bool
	IsCluster        bool
	PollInterval     time.Duration
	DiskPollInterval time.Duration
}

// PVECollector polls a single Proxmox VE instance.
type PVECollector struct {
	config       PVEConfig
	client       *http.Client
	pool         *WorkerPool
	cache        *cache.Cache
	store        *store.Store
	nodes        []string
	clusterID    string
	lastDiskPoll time.Time
}

// NewPVECollector creates a new PVE collector.
func NewPVECollector(cfg PVEConfig, pool *WorkerPool, c *cache.Cache, s *store.Store) *PVECollector {
	if cfg.Insecure {
		slog.Warn("TLS certificate verification disabled — connection is vulnerable to MITM attacks",
			"instance", cfg.Name, "host", cfg.Host)
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure}, //nolint:gosec // user opt-in, warned above
	}
	return &PVECollector{
		config: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		pool:  pool,
		cache: c,
		store: s,
	}
}

func (p *PVECollector) Name() string            { return "pve:" + p.config.Name }
func (p *PVECollector) Interval() time.Duration { return p.config.PollInterval }

// Collect performs a full poll cycle: discover nodes, collect metrics.
func (p *PVECollector) Collect(ctx context.Context) error {
	if err := p.discoverNodes(ctx); err != nil {
		return fmt.Errorf("discovering nodes for %s: %w", p.config.Name, err)
	}

	if p.config.IsCluster && p.clusterID == "" {
		if err := p.detectCluster(ctx); err != nil {
			slog.Warn("cluster detection failed, using instance name", "instance", p.config.Name, "error", err)
			p.clusterID = p.config.Name
		}
	} else if p.clusterID == "" {
		p.clusterID = p.config.Name
	}

	now := time.Now()
	pollDisks := now.Sub(p.lastDiskPoll) >= p.config.DiskPollInterval

	var wg sync.WaitGroup
	var mu sync.Mutex

	nodeMap := make(map[string]*model.Node)
	guestMap := make(map[int]*model.Guest)
	var diskList []*model.Disk

	for _, nodeName := range p.nodes {
		wg.Add(1)

		if err := p.pool.Submit(ctx, func() {
			defer wg.Done()

			// Collect node status
			node, err := p.collectNodeStatus(ctx, nodeName)
			if err != nil {
				slog.Error("collecting node status", "instance", p.config.Name, "node", nodeName, "error", err)
				return
			}
			mu.Lock()
			nodeMap[nodeName] = node
			mu.Unlock()

			// Collect guests (LXC + QEMU)
			guests, err := p.collectGuests(ctx, nodeName)
			if err != nil {
				slog.Error("collecting guests", "instance", p.config.Name, "node", nodeName, "error", err)
			} else {
				mu.Lock()
				for _, g := range guests {
					guestMap[g.VMID] = g
				}
				mu.Unlock()
			}

			// Collect disks if due
			if pollDisks {
				disks, err := p.collectDisks(ctx, nodeName)
				if err != nil {
					slog.Error("collecting disks", "instance", p.config.Name, "node", nodeName, "error", err)
				} else {
					mu.Lock()
					diskList = append(diskList, disks...)
					mu.Unlock()
				}
			}
		}); err != nil {
			wg.Done()
			return fmt.Errorf("submitting node collection for %s: %w", nodeName, err)
		}
	}

	wg.Wait()

	// Update cache
	p.cache.UpdateNodes(p.config.Name, nodeMap)
	p.cache.UpdateGuests(p.clusterID, guestMap)

	if pollDisks {
		diskMap := make(map[string]*model.Disk, len(diskList))
		for _, d := range diskList {
			diskMap[d.WWN] = d
		}
		p.cache.UpdateDisks(diskMap)
		p.lastDiskPoll = now
	}

	// Write snapshots to store
	ts := now.Unix()
	for _, node := range nodeMap {
		snap := model.NodeSnapshot{
			Timestamp:  ts,
			Instance:   p.config.Name,
			Node:       node.Name,
			CPUPct:     node.CPU * 100,
			MemUsed:    node.Memory.Used,
			MemTotal:   node.Memory.Total,
			SwapUsed:   node.Swap.Used,
			SwapTotal:  node.Swap.Total,
			RootUsed:   node.RootFS.Used,
			RootTotal:  node.RootFS.Total,
			Load1m:     node.LoadAvg[0],
			Load5m:     node.LoadAvg[1],
			Load15m:    node.LoadAvg[2],
			IOWait:     node.IOWait,
			UptimeSecs: node.Uptime,
			CPUTemp:    node.Temperature,
		}
		if err := p.store.InsertNodeSnapshot(snap); err != nil {
			slog.Error("storing node snapshot", "instance", p.config.Name, "node", node.Name, "error", err)
		}
	}

	for _, guest := range guestMap {
		cpuPct := float64(0)
		if guest.CPUs > 0 {
			cpuPct = guest.CPU / float64(guest.CPUs) * 100
		}
		snap := model.GuestSnapshot{
			Timestamp: ts,
			Instance:  p.config.Name,
			VMID:      guest.VMID,
			Node:      guest.Node,
			ClusterID: guest.ClusterID,
			GuestType: guest.Type,
			Name:      guest.Name,
			Status:    guest.Status,
			CPUPct:    cpuPct,
			CPUs:      guest.CPUs,
			MemUsed:   guest.Mem,
			MemTotal:  guest.MaxMem,
			DiskUsed:  guest.Disk,
			DiskTotal: guest.MaxDisk,
			NetIn:     guest.NetIn,
			NetOut:    guest.NetOut,
		}
		if err := p.store.InsertGuestSnapshot(snap); err != nil {
			slog.Error("storing guest snapshot", "instance", p.config.Name, "vmid", guest.VMID, "error", err)
		}
	}

	for _, disk := range diskList {
		if err := p.store.UpsertDisk(disk); err != nil {
			slog.Error("storing disk", "wwn", disk.WWN, "error", err)
		}
		if err := p.store.InsertSMARTSnapshot(ts, disk); err != nil {
			slog.Error("storing SMART snapshot", "wwn", disk.WWN, "error", err)
		}
	}

	p.cache.SetLastPoll(p.Name(), now)
	slog.Debug("PVE collection complete", "instance", p.config.Name, "nodes", len(nodeMap), "guests", len(guestMap))
	return nil
}

func (p *PVECollector) apiGet(ctx context.Context, op, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	url := strings.TrimRight(p.config.Host, "/") + path
	slog.Debug("PVE API request", "instance", p.config.Name, "op", op, "path", path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: creating request for %s: %w", op, path, err)
	}
	req.Header.Set("Authorization", "PVEAPIToken="+p.config.TokenID+"="+p.config.TokenSecret)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, NewRetryableError(fmt.Errorf("%s: requesting %s: %w", op, path, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return nil, fmt.Errorf("%s: reading response from %s: %w", op, path, err)
	}

	slog.Debug("PVE API response", "instance", p.config.Name, "op", op, "path", path, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Endpoint:   path,
		}
	}
	return body, nil
}

// pveResponse wraps the standard PVE API response envelope.
type pveResponse struct {
	Data json.RawMessage `json:"data"`
}

// pveNormalizeSentinel converts PVE sentinel strings like "unknown", "none",
// or "-" to an empty string so callers can use a simple empty-check.
func pveNormalizeSentinel(s string) string {
	switch strings.TrimSpace(s) {
	case "", "unknown", "none", "-":
		return ""
	}
	return s
}

func (p *PVECollector) discoverNodes(ctx context.Context) error {
	body, err := p.apiGet(ctx, "discoverNodes", "/api2/json/nodes")
	if err != nil {
		return err
	}

	var resp pveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing nodes response: %w", err)
	}

	var nodeList []struct {
		Node   string `json:"node"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Data, &nodeList); err != nil {
		return fmt.Errorf("parsing node list: %w", err)
	}

	nodes := make([]string, 0, len(nodeList))
	for _, n := range nodeList {
		if n.Status == "online" {
			nodes = append(nodes, n.Node)
		}
	}
	p.nodes = nodes
	return nil
}

func (p *PVECollector) detectCluster(ctx context.Context) error {
	body, err := p.apiGet(ctx, "detectCluster", "/api2/json/cluster/status")
	if err != nil {
		return err
	}

	var resp pveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}

	var entries []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Data, &entries); err != nil {
		return err
	}

	for _, e := range entries {
		if e.Type == "cluster" {
			p.clusterID = e.Name
			return nil
		}
	}
	return fmt.Errorf("no cluster entry found")
}

func (p *PVECollector) collectNodeStatus(ctx context.Context, nodeName string) (*model.Node, error) {
	body, err := p.apiGet(ctx, "collectNodeStatus", fmt.Sprintf("/api2/json/nodes/%s/status", nodeName))
	if err != nil {
		return nil, err
	}

	var resp pveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing node status response: %w", err)
	}

	return parseNodeStatus(p.config.Name, nodeName, resp.Data)
}

func parseNodeStatus(instance, nodeName string, data json.RawMessage) (*model.Node, error) {
	var raw struct {
		CPU     float64 `json:"cpu"`
		CPUInfo struct {
			Model   string `json:"model"`
			Cores   int    `json:"cores"`
			CPUs    int    `json:"cpus"`
			Sockets int    `json:"sockets"`
		} `json:"cpuinfo"`
		Memory struct {
			Used  int64 `json:"used"`
			Total int64 `json:"total"`
		} `json:"memory"`
		Swap struct {
			Used  int64 `json:"used"`
			Total int64 `json:"total"`
		} `json:"swap"`
		RootFS struct {
			Used  int64 `json:"used"`
			Total int64 `json:"total"`
		} `json:"rootfs"`
		LoadAvg    any     `json:"loadavg"` // can be []float64 or []string
		Uptime     int64   `json:"uptime"`
		Wait       float64 `json:"wait"`
		PVEVersion string  `json:"pveversion"`
		KVersion   string  `json:"kversion"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing node data: %w", err)
	}

	node := &model.Node{
		Instance: instance,
		Name:     nodeName,
		Status:   "online",
		CPU:      raw.CPU,
		CPUInfo: model.CPUInfo{
			Model:   raw.CPUInfo.Model,
			Cores:   raw.CPUInfo.Cores,
			Threads: raw.CPUInfo.CPUs,
			Sockets: raw.CPUInfo.Sockets,
		},
		Memory:     model.MemUsage{Used: raw.Memory.Used, Total: raw.Memory.Total},
		Swap:       model.MemUsage{Used: raw.Swap.Used, Total: raw.Swap.Total},
		RootFS:     model.DiskUsage{Used: raw.RootFS.Used, Total: raw.RootFS.Total},
		Uptime:     raw.Uptime,
		IOWait:     raw.Wait,
		PVEVersion: raw.PVEVersion,
		KernelVer:  raw.KVersion,
	}

	// Parse loadavg - PVE may return as strings or floats
	node.LoadAvg = parseLoadAvg(raw.LoadAvg)

	return node, nil
}

func parseLoadAvg(v any) [3]float64 {
	var result [3]float64
	if la, ok := v.([]any); ok {
		for i := 0; i < 3 && i < len(la); i++ {
			switch val := la[i].(type) {
			case float64:
				result[i] = val
			case string:
				f, _ := strconv.ParseFloat(val, 64)
				result[i] = f
			}
		}
	}
	return result
}

func (p *PVECollector) collectGuests(ctx context.Context, nodeName string) ([]*model.Guest, error) {
	var guests []*model.Guest

	// Collect LXC containers
	lxcGuests, err := p.collectGuestType(ctx, nodeName, "lxc")
	if err != nil {
		slog.Warn("collecting LXC guests failed", "node", nodeName, "error", err)
	} else {
		guests = append(guests, lxcGuests...)
	}

	// Collect QEMU VMs
	qemuGuests, err := p.collectGuestType(ctx, nodeName, "qemu")
	if err != nil {
		slog.Warn("collecting QEMU guests failed", "node", nodeName, "error", err)
	} else {
		guests = append(guests, qemuGuests...)
	}

	return guests, nil
}

func (p *PVECollector) collectGuestType(ctx context.Context, nodeName, guestType string) ([]*model.Guest, error) {
	body, err := p.apiGet(ctx, "collectGuests:"+guestType, fmt.Sprintf("/api2/json/nodes/%s/%s", nodeName, guestType))
	if err != nil {
		return nil, err
	}

	var resp pveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing %s list: %w", guestType, err)
	}

	var rawGuests []struct {
		VMID    int     `json:"vmid"`
		Name    string  `json:"name"`
		Status  string  `json:"status"`
		CPU     float64 `json:"cpu"`
		CPUs    int     `json:"cpus"`
		Mem     int64   `json:"mem"`
		MaxMem  int64   `json:"maxmem"`
		Disk    int64   `json:"disk"`
		MaxDisk int64   `json:"maxdisk"`
		NetIn   int64   `json:"netin"`
		NetOut  int64   `json:"netout"`
		Uptime  int64   `json:"uptime"`
	}
	if err := json.Unmarshal(resp.Data, &rawGuests); err != nil {
		return nil, fmt.Errorf("parsing %s data: %w", guestType, err)
	}

	guests := make([]*model.Guest, 0, len(rawGuests))
	for _, rg := range rawGuests {
		guests = append(guests, &model.Guest{
			Instance:  p.config.Name,
			Node:      nodeName,
			ClusterID: p.clusterID,
			Type:      guestType,
			VMID:      rg.VMID,
			Name:      rg.Name,
			Status:    rg.Status,
			CPU:       rg.CPU,
			CPUs:      rg.CPUs,
			Mem:       rg.Mem,
			MaxMem:    rg.MaxMem,
			Disk:      rg.Disk,
			MaxDisk:   rg.MaxDisk,
			NetIn:     rg.NetIn,
			NetOut:    rg.NetOut,
			Uptime:    rg.Uptime,
		})
	}
	return guests, nil
}

func (p *PVECollector) collectDisks(ctx context.Context, nodeName string) ([]*model.Disk, error) {
	// skipsmart=1 tells PVE to skip its own internal smartctl scan when listing disks.
	// Without it, PVE runs smartctl for every disk during the list call, which can cause
	// timeouts on drives that are slow to respond (e.g. spinning HDDs in standby).
	// We collect SMART data ourselves in a separate per-disk call below.
	body, err := p.apiGet(ctx, "collectDisks", fmt.Sprintf("/api2/json/nodes/%s/disks/list?skipsmart=1", nodeName))
	if err != nil {
		return nil, err
	}

	var resp pveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing disk list: %w", err)
	}

	var rawDisks []struct {
		DevPath string `json:"devpath"`
		Model   string `json:"model"`
		Serial  string `json:"serial"`
		WWN     string `json:"wwn"`
		Size    int64  `json:"size"`
		Type    string `json:"type"` // "hdd", "ssd", "nvme", or "unknown"
		RPM     int    `json:"rpm"`  // rotation speed; 0 = solid-state
	}
	if err := json.Unmarshal(resp.Data, &rawDisks); err != nil {
		return nil, fmt.Errorf("parsing disk data: %w", err)
	}

	slog.Debug("PVE disk list raw", "instance", p.config.Name, "node", nodeName, "count", len(rawDisks))
	var disks []*model.Disk
	now := time.Now()
	for _, rd := range rawDisks {
		slog.Debug("PVE disk entry", "node", nodeName, "devpath", rd.DevPath,
			"model", rd.Model, "serial", rd.Serial, "wwn", rd.WWN, "type", rd.Type, "rpm", rd.RPM, "size", rd.Size)
		// PVE returns "unknown" (literal string) for WWN/type when the drive
		// doesn't expose one — treat these sentinel values as empty so the
		// fallback chain below picks a proper unique identity.
		wwn := pveNormalizeSentinel(rd.WWN)
		if wwn == "" {
			wwn = pveNormalizeSentinel(rd.Serial) // fallback to serial
		}
		if wwn == "" {
			wwn = rd.DevPath // last resort — stable enough for non-hot-swap homelab drives
		}
		if wwn == "" {
			slog.Warn("PVE disk skipped: no identity", "node", nodeName, "model", rd.Model)
			continue // skip disks with no identity
		}

		diskType := pveNormalizeSentinel(rd.Type)
		protocol := "ata"
		if strings.HasPrefix(rd.DevPath, "/dev/nvme") {
			protocol = "nvme"
			diskType = "nvme"
		}
		if diskType == "" {
			// PVE sources RPM from the kernel's /sys/block/*/queue/rotational.
			// rpm > 0: spinning disk with known RPM; rpm == -1: rotational but
			// RPM unknown (PVE's sentinel); rpm == 0: solid-state / NVMe.
			if rd.RPM != 0 {
				diskType = "hdd"
			} else {
				diskType = "ssd"
			}
		}

		disk := &model.Disk{
			Instance:  p.config.Name,
			Node:      nodeName,
			WWN:       wwn,
			DevPath:   rd.DevPath,
			Model:     rd.Model,
			Serial:    rd.Serial,
			DiskType:  diskType,
			Protocol:  protocol,
			SizeBytes: rd.Size,
			FirstSeen: now,
			LastSeen:  now,
		}

		// Collect SMART data for this disk
		if err := p.collectSMART(ctx, nodeName, disk); err != nil {
			slog.Warn("collecting SMART data", "disk", rd.DevPath, "node", nodeName, "error", err)
			disk.Status = model.StatusInternalError
		}

		disks = append(disks, disk)
	}

	slog.Debug("PVE disks collected", "instance", p.config.Name, "node", nodeName,
		"listed", len(rawDisks), "collected", len(disks))
	return disks, nil
}

func (p *PVECollector) collectSMART(ctx context.Context, nodeName string, disk *model.Disk) error {
	return p.collectSMARTWithType(ctx, nodeName, disk, "")
}

func (p *PVECollector) collectSMARTWithType(ctx context.Context, nodeName string, disk *model.Disk, smartType string) error {
	u := fmt.Sprintf("/api2/json/nodes/%s/disks/smart?disk=%s", nodeName, url.QueryEscape(disk.DevPath))
	if smartType != "" {
		u += "&type=" + url.QueryEscape(smartType)
	}
	body, err := p.apiGet(ctx, "collectSMART", u)
	if err != nil {
		// Some PVE versions reject the "type" query parameter with a 400.
		// Cascade through the fallback chain before surfacing the error.
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
			switch smartType {
			case "sat":
				slog.Debug("PVE SMART sat rejected (400), retrying with scsi", "disk", disk.DevPath)
				return p.collectSMARTWithType(ctx, nodeName, disk, "scsi")
			case "scsi":
				slog.Debug("PVE SMART scsi also rejected (400), no type fallback available", "disk", disk.DevPath)
				return nil
			}
		}
		return err
	}

	var resp pveResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing SMART response: %w", err)
	}

	var smartData struct {
		Health     string           `json:"health"`
		Type       string           `json:"type"`
		Wearout    any              `json:"wearout"` // can be int or string or null
		Attributes []map[string]any `json:"attributes"`
		Text       string           `json:"text"` // raw smartctl output (NVMe)
	}
	if err := json.Unmarshal(resp.Data, &smartData); err != nil {
		return fmt.Errorf("parsing SMART data: %w", err)
	}

	disk.Health = smartData.Health

	// Parse wearout (inconsistent types from PVE API)
	if smartData.Wearout != nil {
		switch w := smartData.Wearout.(type) {
		case float64:
			wv := int(w)
			disk.Wearout = &wv
		case string:
			if wv, err := strconv.Atoi(w); err == nil {
				disk.Wearout = &wv
			}
		}
	}

	// Parse SMART attributes using the smart package.
	var attrs []model.SMARTAttribute
	switch {
	case disk.Protocol == "nvme" && smartData.Text != "":
		parsed, err := smart.ParseNVMeText(smartData.Text)
		if err != nil {
			slog.Debug("parsing NVMe SMART text", "disk", disk.DevPath, "error", err)
		} else {
			attrs = parsed
		}
	case len(smartData.Attributes) > 0:
		parsed, err := smart.ParseATAAttributes(smartData.Attributes)
		if err != nil {
			slog.Debug("parsing ATA SMART attributes", "disk", disk.DevPath, "error", err)
		} else {
			attrs = parsed
		}
	case smartType == "scsi" && smartData.Text != "":
		// SCSI text output — parse for temperature/power-on hours.
		attrs = smart.ParseSCSIText(smartData.Text)
		slog.Debug("PVE SMART SCSI text parsed", "disk", disk.DevPath, "attrs", len(attrs))
	case disk.Protocol == "ata" && smartType == "":
		// PVE returned no structured data (common for SATA drives behind an HBA).
		// Try sat (SCSI-to-ATA Translation passthrough), then scsi.
		slog.Debug("PVE SMART retrying with sat", "disk", disk.DevPath)
		return p.collectSMARTWithType(ctx, nodeName, disk, "sat")
	case disk.Protocol == "ata" && smartType == "sat":
		// SAT also returned nothing — try SCSI mode sense (works for drives
		// that respond to SCSI commands rather than ATA commands).
		slog.Debug("PVE SMART retrying with scsi", "disk", disk.DevPath)
		return p.collectSMARTWithType(ctx, nodeName, disk, "scsi")
	}

	disk.Attributes = attrs
	smart.EvaluateDisk(disk)

	// Extract scalar metrics from parsed attributes.
	for i := range disk.Attributes {
		a := &disk.Attributes[i]
		switch disk.Protocol {
		case "nvme":
			switch a.ID {
			case smart.NVMeTemperature:
				t := safeInt(a.RawValue)
				disk.Temperature = &t
			case smart.NVMePowerOnHours:
				h := safeInt(a.RawValue)
				disk.PowerOnHours = &h
			case smart.NVMePercentageUsed:
				if disk.Wearout == nil {
					remaining := 100 - safeInt(a.RawValue)
					disk.Wearout = &remaining
				}
			}
		default: // ata (also handles SCSI pseudo-attrs from type=scsi fallback)
			switch a.ID {
			case 194: // ATA Temperature_Celsius
				t := safeInt(a.RawValue)
				disk.Temperature = &t
			case 190: // ATA Airflow_Temperature_Cel (fallback)
				if disk.Temperature == nil {
					t := safeInt(a.RawValue)
					disk.Temperature = &t
				}
			case 9: // ATA Power_On_Hours
				h := safeInt(a.RawValue)
				disk.PowerOnHours = &h
			case smart.SCSITemperature: // SCSI Current Drive Temperature
				t := safeInt(a.RawValue)
				disk.Temperature = &t
			case smart.SCSIPowerOnHours: // SCSI power-on hours
				h := safeInt(a.RawValue)
				disk.PowerOnHours = &h
			}
		}
	}

	return nil
}

// safeInt converts int64 to int, clamping to the platform int range to
// prevent silent truncation on 32-bit platforms.
func safeInt(v int64) int {
	if v > math.MaxInt {
		return math.MaxInt
	}
	if v < math.MinInt {
		return math.MinInt
	}
	return int(v)
}
