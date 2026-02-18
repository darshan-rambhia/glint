package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RetentionConfig defines how long to keep data in each table.
type RetentionConfig struct {
	NodeSnapshots      time.Duration // default 48h
	GuestSnapshots     time.Duration // default 48h
	SMARTSnapshots     time.Duration // default 30d
	BackupSnapshots    time.Duration // default 7d
	DatastoreSnapshots time.Duration // default 7d
	AlertLog           time.Duration // default 30d
}

// DefaultRetention returns the default retention periods.
func DefaultRetention() RetentionConfig {
	return RetentionConfig{
		NodeSnapshots:      48 * time.Hour,
		GuestSnapshots:     48 * time.Hour,
		SMARTSnapshots:     30 * 24 * time.Hour,
		BackupSnapshots:    7 * 24 * time.Hour,
		DatastoreSnapshots: 7 * 24 * time.Hour,
		AlertLog:           30 * 24 * time.Hour,
	}
}

// Pruner periodically removes old data from the store.
type Pruner struct {
	store     *Store
	retention RetentionConfig
	interval  time.Duration
}

// NewPruner creates a pruner with the given retention config.
func NewPruner(store *Store, retention RetentionConfig) *Pruner {
	return &Pruner{
		store:     store,
		retention: retention,
		interval:  1 * time.Hour,
	}
}

// Run starts the pruner loop. It blocks until the context is cancelled.
func (p *Pruner) Run(ctx context.Context) error {
	slog.Info("pruner started", "interval", p.interval)

	// Run once at startup
	p.prune()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("pruner stopped")
			return ctx.Err()
		case <-ticker.C:
			p.prune()
		}
	}
}

func (p *Pruner) prune() {
	now := time.Now().Unix()
	tables := []struct {
		name      string
		retention time.Duration
	}{
		{"node_snapshots", p.retention.NodeSnapshots},
		{"guest_snapshots", p.retention.GuestSnapshots},
		{"smart_snapshots", p.retention.SMARTSnapshots},
		{"backup_snapshots", p.retention.BackupSnapshots},
		{"datastore_snapshots", p.retention.DatastoreSnapshots},
		{"alert_log", p.retention.AlertLog},
	}

	for _, t := range tables {
		cutoff := now - int64(t.retention.Seconds())
		result, err := p.store.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE ts < ?", t.name), cutoff)
		if err != nil {
			slog.Error("pruning failed", "table", t.name, "error", err)
			continue
		}
		rows, _ := result.RowsAffected()
		if rows > 0 {
			slog.Info("pruned old data", "table", t.name, "rows", rows)
		}
	}
}
