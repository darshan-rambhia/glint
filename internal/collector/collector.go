// Package collector provides the data collection framework for Glint.
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Collector is the interface for all data collectors.
type Collector interface {
	Name() string
	Collect(ctx context.Context) error
	Interval() time.Duration
}

// WorkerPool bounds concurrent API calls across all collectors.
type WorkerPool struct {
	sem chan struct{}
}

// NewWorkerPool creates a worker pool with the given max concurrent workers.
func NewWorkerPool(maxWorkers int) *WorkerPool {
	return &WorkerPool{sem: make(chan struct{}, maxWorkers)}
}

// Submit runs fn in the pool, blocking if all workers are busy.
// Returns ctx.Err() if context is cancelled while waiting.
func (p *WorkerPool) Submit(ctx context.Context, fn func()) error {
	select {
	case p.sem <- struct{}{}:
		go func() {
			defer func() { <-p.sem }()
			fn()
		}()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run starts a collector loop that calls Collect at the configured interval.
// It blocks until the context is cancelled.
func Run(ctx context.Context, c Collector) error {
	name := c.Name()
	interval := c.Interval()
	slog.Info("collector started", "name", name, "interval", interval)

	// Collect immediately on startup
	if err := c.Collect(ctx); err != nil {
		slog.Error("collection failed", "collector", name, "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("collector stopped", "name", name)
			return ctx.Err()
		case <-ticker.C:
			if err := c.Collect(ctx); err != nil {
				slog.Error("collection failed", "collector", name, "error", err)
			}
		}
	}
}

// BackupCollector is the interface for backup monitoring providers.
type BackupCollector interface {
	Name() string
	Collect(ctx context.Context) ([]BackupResult, error)
	Interval() time.Duration
}

// BackupResult groups backup data from a single collection cycle.
type BackupResult struct {
	Datastores []DatastoreResult
	Tasks      []TaskResult
}

// DatastoreResult wraps datastore status with its backups.
type DatastoreResult struct {
	Status  DatastoreInfo
	Backups []BackupInfo
}

// DatastoreInfo is the collector-level datastore status (before mapping to model).
type DatastoreInfo struct {
	Name       string
	TotalBytes *int64
	UsedBytes  *int64
	AvailBytes *int64
	DedupRatio *float64
	Error      *string
}

// BackupInfo is the collector-level backup info.
type BackupInfo struct {
	Datastore  string
	BackupType string
	BackupID   string
	BackupTime int64
	SizeBytes  *int64
	Verified   *bool
}

// TaskResult is the collector-level task info.
type TaskResult struct {
	UPID      string
	Type      string
	ID        string
	StartTime int64
	EndTime   *int64
	Status    string
	User      string
}

// RetryableError wraps an error that can be retried.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string     { return e.Err.Error() }
func (e *RetryableError) Unwrap() error     { return e.Err }
func (e *RetryableError) IsRetryable() bool { return true }

// NewRetryableError creates a new retryable error.
func NewRetryableError(err error) *RetryableError {
	return &RetryableError{Err: err}
}

// APIError represents an HTTP API error from PVE or PBS.
type APIError struct {
	StatusCode int
	Body       string
	Endpoint   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d from %s: %s", e.StatusCode, e.Endpoint, e.Body)
}

func (e *APIError) IsRetryable() bool {
	return e.StatusCode >= 500 || e.StatusCode == 429
}
