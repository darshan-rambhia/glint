package collector

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure},
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

	// 1. Get datastore usage
	datastores, err := c.collectDatastoreUsage(ctx)
	if err != nil {
		return fmt.Errorf("collecting datastore usage for %s: %w", c.config.Name, err)
	}

	// Filter to configured datastores
	if len(c.config.Datastores) > 0 {
		allowed := make(map[string]bool, len(c.config.Datastores))
		for _, ds := range c.config.Datastores {
			allowed[ds] = true
		}
		filtered := make(map[string]*model.DatastoreStatus)
		for name, ds := range datastores {
			if allowed[name] {
				filtered[name] = ds
			}
		}
		datastores = filtered
	}

	// 2. Get snapshots for each datastore
	backups := make(map[string]*model.Backup)
	for dsName := range datastores {
		snaps, err := c.collectSnapshots(ctx, dsName)
		if err != nil {
			slog.Error("collecting snapshots", "pbs", c.config.Name, "datastore", dsName, "error", err)
			continue
		}
		// Keep only latest backup per backup_id
		for _, snap := range snaps {
			key := snap.BackupID
			if existing, ok := backups[key]; !ok || snap.BackupTime > existing.BackupTime {
				backups[key] = snap
			}
		}
	}

	// 3. Get recent tasks
	tasks, err := c.collectTasks(ctx)
	if err != nil {
		slog.Error("collecting PBS tasks", "pbs", c.config.Name, "error", err)
	}

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

func (c *PBSCollector) apiGet(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	url := strings.TrimRight(c.config.Host, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", path, err)
	}
	req.Header.Set("Authorization", "PBSAPIToken="+c.config.TokenID+":"+c.config.TokenSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, NewRetryableError(fmt.Errorf("requesting %s: %w", path, err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB max
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Endpoint:   path,
		}
	}
	return body, nil
}

func (c *PBSCollector) collectDatastoreUsage(ctx context.Context) (map[string]*model.DatastoreStatus, error) {
	body, err := c.apiGet(ctx, "/api2/json/status/datastore-usage")
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

func (c *PBSCollector) collectSnapshots(ctx context.Context, datastore string) ([]*model.Backup, error) {
	body, err := c.apiGet(ctx, fmt.Sprintf("/api2/json/admin/datastore/%s/snapshots", datastore))
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
	since := time.Now().Add(-24 * time.Hour).Unix()
	body, err := c.apiGet(ctx, fmt.Sprintf("/api2/json/nodes/localhost/tasks?since=%d&limit=50", since))
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
