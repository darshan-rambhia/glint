package collector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/store"
)

// PBSConfig holds configuration for a single PBS instance.
type PBSConfig struct {
	Name         string
	Host         string
	TokenID      string
	TokenSecret  string
	Insecure     bool
	Datastores   []string // which datastores to monitor (empty = all)
	PollInterval time.Duration
}

// PBSCollector polls a single Proxmox Backup Server instance.
type PBSCollector struct {
	config PBSConfig
	client *http.Client
	pool   *WorkerPool
	cache  *cache.Cache
	store  *store.Store
}

// NewPBSCollector creates a new PBS collector.
func NewPBSCollector(cfg PBSConfig, pool *WorkerPool, c *cache.Cache, s *store.Store) *PBSCollector {
	if cfg.Insecure {
		slog.Warn("TLS certificate verification disabled — connection is vulnerable to MITM attacks",
			"instance", cfg.Name, "host", cfg.Host)
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure}, //nolint:gosec // user opt-in, warned above
	}
	return &PBSCollector{
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

func (c *PBSCollector) Name() string            { return "pbs:" + c.config.Name }
func (c *PBSCollector) Interval() time.Duration { return c.config.PollInterval }

// Collect performs a full PBS poll cycle.
func (c *PBSCollector) Collect(ctx context.Context) error {
	now := time.Now()
	ts := now.Unix()

	slog.Debug("PBS collection starting", "instance", c.config.Name, "host", c.config.Host)

	// 1. Get datastore usage.
	// When specific datastores are configured, query each one directly via the
	// per-datastore status endpoint — this only requires datastore-scoped
	// permissions. Fall back to the system-wide listing when no filter is set,
	// which requires broader system permissions.
	//
	// If the status endpoint returns a permission error (403), we still add a
	// stub entry so that snapshot collection proceeds — backup data is more
	// useful than nothing. Capacity fields will be nil (unknown).
	var datastores map[string]*model.DatastoreStatus
	if len(c.config.Datastores) > 0 {
		datastores = make(map[string]*model.DatastoreStatus, len(c.config.Datastores))
		for _, dsName := range c.config.Datastores {
			ds, err := c.collectDatastoreStatus(ctx, dsName)
			if err != nil {
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == 403 {
					slog.Warn("PBS token lacks Datastore.Audit permission; capacity unknown — grant DatastoreAudit role on /datastore/"+dsName,
						"pbs", c.config.Name, "datastore", dsName)
					datastores[dsName] = &model.DatastoreStatus{PBSInstance: c.config.Name, Name: dsName}
				} else {
					slog.Error("collecting datastore status", "pbs", c.config.Name, "datastore", dsName, "error", err)
				}
				continue
			}
			datastores[dsName] = ds
		}
	} else {
		var err error
		datastores, err = c.collectAllDatastoreUsage(ctx)
		if err != nil {
			return fmt.Errorf("collecting datastore usage for %s: %w", c.config.Name, err)
		}
	}
	slog.Debug("PBS datastores ready", "instance", c.config.Name, "count", len(datastores))

	// 2. Get snapshots for each datastore
	backups := make(map[string]*model.Backup)
	for dsName := range datastores {
		snaps, err := c.collectSnapshots(ctx, dsName)
		if err != nil {
			slog.Error("collecting snapshots", "pbs", c.config.Name, "datastore", dsName, "error", err)
			continue
		}
		slog.Debug("PBS snapshots found", "instance", c.config.Name, "datastore", dsName, "count", len(snaps))
		// Keep latest backup per (datastore, backup_id) so that the same VMID
		// backed up in multiple datastores is tracked independently.
		for _, snap := range snaps {
			key := snap.Datastore + "/" + snap.BackupID
			if existing, ok := backups[key]; !ok || snap.BackupTime > existing.BackupTime {
				backups[key] = snap
			}
		}
	}

	// 3. Get recent tasks.
	// Requires Sys.Audit on /nodes/localhost. Datastore-scoped tokens typically
	// don't have this; a 403 is logged as a warning with the fix described.
	tasks, err := c.collectTasks(ctx)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 403 {
			slog.Warn("PBS token lacks Sys.Audit permission; task history unavailable — grant Sys.Audit role on /nodes/localhost",
				"pbs", c.config.Name)
		} else {
			slog.Error("collecting PBS tasks", "pbs", c.config.Name, "error", err)
		}
	}
	slog.Debug("PBS tasks found", "instance", c.config.Name, "count", len(tasks))

	// Update cache
	c.cache.UpdateDatastores(c.config.Name, datastores)
	c.cache.UpdateBackups(c.config.Name, backups)
	if tasks != nil {
		c.cache.UpdateTasks(c.config.Name, tasks)
	}

	// Write to store
	for _, ds := range datastores {
		if err := c.store.InsertDatastoreSnapshot(ts, ds); err != nil {
			slog.Error("storing datastore snapshot", "pbs", c.config.Name, "store", ds.Name, "error", err)
		}
	}
	for _, b := range backups {
		if err := c.store.InsertBackupSnapshot(ts, b); err != nil {
			slog.Error("storing backup snapshot", "pbs", c.config.Name, "id", b.BackupID, "error", err)
		}
	}

	c.cache.SetLastPoll(c.Name(), now)
	slog.Debug("PBS collection complete", "instance", c.config.Name, "datastores", len(datastores), "backups", len(backups))
	return nil
}

func (c *PBSCollector) apiGet(ctx context.Context, op, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	url := strings.TrimRight(c.config.Host, "/") + path
	slog.Debug("PBS API request", "instance", c.config.Name, "op", op, "path", path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: creating request for %s: %w", op, path, err)
	}
	req.Header.Set("Authorization", "PBSAPIToken="+c.config.TokenID+":"+c.config.TokenSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, NewRetryableError(fmt.Errorf("%s: requesting %s: %w", op, path, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return nil, fmt.Errorf("%s: reading response from %s: %w", op, path, err)
	}

	slog.Debug("PBS API response", "instance", c.config.Name, "op", op, "path", path, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Endpoint:   path,
		}
	}
	return body, nil
}

// collectAllDatastoreUsage fetches all datastores via the system-wide endpoint.
// Requires system-level permissions; used when no specific datastores are configured.
func (c *PBSCollector) collectAllDatastoreUsage(ctx context.Context) (map[string]*model.DatastoreStatus, error) {
	body, err := c.apiGet(ctx, "collectDatastoreUsage", "/api2/json/status/datastore-usage")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []struct {
			Store string  `json:"store"`
			Total *int64  `json:"total"`
			Used  *int64  `json:"used"`
			Avail *int64  `json:"avail"`
			Error *string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing datastore usage: %w", err)
	}

	datastores := make(map[string]*model.DatastoreStatus, len(resp.Data))
	for _, ds := range resp.Data {
		datastores[ds.Store] = &model.DatastoreStatus{
			PBSInstance: c.config.Name,
			Name:        ds.Store,
			TotalBytes:  ds.Total,
			UsedBytes:   ds.Used,
			AvailBytes:  ds.Avail,
			Error:       ds.Error,
		}
	}
	return datastores, nil
}

// collectDatastoreStatus fetches status for a single named datastore.
// Only requires datastore-scoped permissions on /datastore/{name}.
func (c *PBSCollector) collectDatastoreStatus(ctx context.Context, datastore string) (*model.DatastoreStatus, error) {
	body, err := c.apiGet(ctx, "collectDatastoreStatus", fmt.Sprintf("/api2/json/admin/datastore/%s/status", datastore))
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Total *int64  `json:"total"`
			Used  *int64  `json:"used"`
			Avail *int64  `json:"avail"`
			Error *string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing datastore status: %w", err)
	}

	return &model.DatastoreStatus{
		PBSInstance: c.config.Name,
		Name:        datastore,
		TotalBytes:  resp.Data.Total,
		UsedBytes:   resp.Data.Used,
		AvailBytes:  resp.Data.Avail,
		Error:       resp.Data.Error,
	}, nil
}

func (c *PBSCollector) collectSnapshots(ctx context.Context, datastore string) ([]*model.Backup, error) {
	body, err := c.apiGet(ctx, "collectSnapshots", fmt.Sprintf("/api2/json/admin/datastore/%s/snapshots", datastore))
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []struct {
			BackupType   string `json:"backup-type"` // "ct", "vm", "host"
			BackupID     string `json:"backup-id"`
			BackupTime   int64  `json:"backup-time"`
			Size         *int64 `json:"size"`
			Verification *struct {
				State string `json:"state"` // "ok", "failed"
			} `json:"verification"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing snapshots: %w", err)
	}

	backups := make([]*model.Backup, 0, len(resp.Data))
	for _, s := range resp.Data {
		b := &model.Backup{
			PBSInstance: c.config.Name,
			Datastore:   datastore,
			BackupType:  s.BackupType,
			BackupID:    s.BackupID,
			BackupTime:  s.BackupTime,
			SizeBytes:   s.Size,
		}
		if s.Verification != nil {
			v := s.Verification.State == "ok"
			b.Verified = &v
		}
		backups = append(backups, b)
	}
	return backups, nil
}

func (c *PBSCollector) collectTasks(ctx context.Context) ([]*model.PBSTask, error) {
	since := time.Now().Add(-7 * 24 * time.Hour).Unix()
	body, err := c.apiGet(ctx, "collectTasks", fmt.Sprintf("/api2/json/nodes/localhost/tasks?since=%d&limit=200", since))
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []struct {
			UPID      string `json:"upid"`
			Type      string `json:"worker_type"` // "backup", "verificationjob", "prune", "garbage_collection"
			ID        string `json:"worker_id"`
			StartTime int64  `json:"starttime"`
			EndTime   *int64 `json:"endtime"`
			Status    string `json:"status"`
			User      string `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing tasks: %w", err)
	}

	tasks := make([]*model.PBSTask, 0, len(resp.Data))
	for _, t := range resp.Data {
		taskType := t.Type
		switch taskType {
		case "verificationjob":
			taskType = "verify"
		case "garbage_collection":
			taskType = "gc"
		}

		tasks = append(tasks, &model.PBSTask{
			PBSInstance: c.config.Name,
			UPID:        t.UPID,
			Type:        taskType,
			ID:          t.ID,
			StartTime:   t.StartTime,
			EndTime:     t.EndTime,
			Status:      t.Status,
			User:        t.User,
		})
	}
	return tasks, nil
}
