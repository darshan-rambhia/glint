// Package alerter evaluates alert rules against cached state.
package alerter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/notify"
	"github.com/darshan-rambhia/glint/internal/store"
)

// AlertConfig holds configuration for alert rules.
type AlertConfig struct {
	NodeCPUHigh     *ThresholdAlert `yaml:"node_cpu_high"`
	NodeMemHigh     *ThresholdAlert `yaml:"node_mem_high"`
	GuestDown       *GuestAlert     `yaml:"guest_down"`
	BackupStale     *BackupAlert    `yaml:"backup_stale"`
	DiskSmartFailed *SimpleAlert    `yaml:"disk_smart_failed"`
	DatastoreFull   *ThresholdAlert `yaml:"datastore_full"`
}

// ThresholdAlert triggers when a value exceeds a threshold.
type ThresholdAlert struct {
	Threshold float64       `yaml:"threshold"`
	Duration  time.Duration `yaml:"duration"`
	Severity  string        `yaml:"severity"`
	Cooldown  time.Duration `yaml:"cooldown"`
}

// GuestAlert triggers when a guest is down for too long.
type GuestAlert struct {
	GracePeriod time.Duration `yaml:"grace_period"`
	Severity    string        `yaml:"severity"`
	Cooldown    time.Duration `yaml:"cooldown"`
}

// BackupAlert triggers when a backup is stale.
type BackupAlert struct {
	MaxAge   time.Duration `yaml:"max_age"`
	Severity string        `yaml:"severity"`
	Cooldown time.Duration `yaml:"cooldown"`
}

// SimpleAlert triggers on a boolean condition.
type SimpleAlert struct {
	Severity string        `yaml:"severity"`
	Cooldown time.Duration `yaml:"cooldown"`
}

// DefaultAlertConfig returns sensible alert defaults.
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		NodeCPUHigh: &ThresholdAlert{
			Threshold: 90, Duration: 5 * time.Minute, Severity: "warning", Cooldown: 1 * time.Hour,
		},
		NodeMemHigh: &ThresholdAlert{
			Threshold: 90, Duration: 5 * time.Minute, Severity: "warning", Cooldown: 1 * time.Hour,
		},
		GuestDown: &GuestAlert{
			GracePeriod: 2 * time.Minute, Severity: "critical", Cooldown: 30 * time.Minute,
		},
		BackupStale: &BackupAlert{
			MaxAge: 36 * time.Hour, Severity: "warning", Cooldown: 6 * time.Hour,
		},
		DiskSmartFailed: &SimpleAlert{
			Severity: "critical", Cooldown: 6 * time.Hour,
		},
		DatastoreFull: &ThresholdAlert{
			Threshold: 85, Severity: "warning", Cooldown: 6 * time.Hour,
		},
	}
}

// Alerter evaluates rules and sends notifications.
type Alerter struct {
	cache     *cache.Cache
	store     *store.Store
	providers []notify.Provider
	config    AlertConfig
	interval  time.Duration

	// Deduplication: maps alert key → last fired time
	lastFired map[string]time.Time

	// Track sustained conditions: maps alert key → first observed time
	sustained map[string]time.Time
}

// NewAlerter creates a new alerter.
func NewAlerter(c *cache.Cache, s *store.Store, providers []notify.Provider, cfg AlertConfig) *Alerter {
	return &Alerter{
		cache:     c,
		store:     s,
		providers: providers,
		config:    cfg,
		interval:  30 * time.Second,
		lastFired: make(map[string]time.Time),
		sustained: make(map[string]time.Time),
	}
}

// Run starts the alerter evaluation loop.
func (a *Alerter) Run(ctx context.Context) error {
	slog.Info("alerter started", "interval", a.interval)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("alerter stopped")
			return ctx.Err()
		case <-ticker.C:
			a.evaluate(ctx)
		}
	}
}

func (a *Alerter) cleanup(now time.Time) {
	const maxAge = 6 * time.Hour
	for key, t := range a.lastFired {
		if now.Sub(t) > maxAge {
			delete(a.lastFired, key)
		}
	}
	for key, t := range a.sustained {
		if now.Sub(t) > maxAge {
			delete(a.sustained, key)
		}
	}
}

func (a *Alerter) evaluate(ctx context.Context) {
	snap := a.cache.Snapshot()
	now := time.Now()

	a.cleanup(now)

	// Node CPU/Memory alerts
	for instance, nodes := range snap.Nodes {
		for _, node := range nodes {
			if a.config.NodeCPUHigh != nil {
				a.checkSustainedThreshold(ctx, now,
					fmt.Sprintf("node_cpu:%s/%s", instance, node.Name),
					node.CPU*100,
					a.config.NodeCPUHigh,
					model.Notification{
						AlertType: "node_cpu_high",
						Severity:  a.config.NodeCPUHigh.Severity,
						Title:     fmt.Sprintf("Node CPU High: %s/%s", instance, node.Name),
						Message:   fmt.Sprintf("[%s/%s] CPU at %.0f%% for 5+ minutes", instance, node.Name, node.CPU*100),
						Instance:  instance,
						Subject:   node.Name,
						Timestamp: now,
						Metadata:  map[string]string{"value": fmt.Sprintf("%.0f", node.CPU*100)},
					},
				)
			}

			if a.config.NodeMemHigh != nil && node.Memory.Total > 0 {
				memPct := float64(node.Memory.Used) / float64(node.Memory.Total) * 100
				a.checkSustainedThreshold(ctx, now,
					fmt.Sprintf("node_mem:%s/%s", instance, node.Name),
					memPct,
					a.config.NodeMemHigh,
					model.Notification{
						AlertType: "node_mem_high",
						Severity:  a.config.NodeMemHigh.Severity,
						Title:     fmt.Sprintf("Node Memory High: %s/%s", instance, node.Name),
						Message:   fmt.Sprintf("[%s/%s] Memory at %.0f%% for 5+ minutes", instance, node.Name, memPct),
						Instance:  instance,
						Subject:   node.Name,
						Timestamp: now,
						Metadata:  map[string]string{"value": fmt.Sprintf("%.0f", memPct)},
					},
				)
			}
		}
	}

	// Guest down alerts
	if a.config.GuestDown != nil {
		for clusterID, guests := range snap.Guests {
			for _, guest := range guests {
				key := fmt.Sprintf("guest_down:%s/%d", clusterID, guest.VMID)
				if guest.Status != "running" {
					if first, ok := a.sustained[key]; ok {
						if now.Sub(first) >= a.config.GuestDown.GracePeriod {
							a.fire(ctx, now, key, a.config.GuestDown.Cooldown, model.Notification{
								AlertType: "guest_down",
								Severity:  a.config.GuestDown.Severity,
								Title:     fmt.Sprintf("Guest Down: %s (%d)", guest.Name, guest.VMID),
								Message:   fmt.Sprintf("[%s] %s (ID %d) is %s", guest.Instance, guest.Name, guest.VMID, guest.Status),
								Instance:  guest.Instance,
								Subject:   guest.Name,
								Timestamp: now,
								Metadata: map[string]string{
									"vmid":   fmt.Sprintf("%d", guest.VMID),
									"status": guest.Status,
								},
							})
						}
					} else {
						a.sustained[key] = now
					}
				} else {
					delete(a.sustained, key)
				}
			}
		}
	}

	// Backup stale alerts
	if a.config.BackupStale != nil {
		for pbsInstance, backups := range snap.Backups {
			for id, backup := range backups {
				age := time.Since(time.Unix(backup.BackupTime, 0))
				if age > a.config.BackupStale.MaxAge {
					key := fmt.Sprintf("backup_stale:%s/%s", pbsInstance, id)
					a.fire(ctx, now, key, a.config.BackupStale.Cooldown, model.Notification{
						AlertType: "backup_stale",
						Severity:  a.config.BackupStale.Severity,
						Title:     fmt.Sprintf("Backup Stale: %s/%s %s", backup.BackupType, backup.BackupID, pbsInstance),
						Message:   fmt.Sprintf("[%s] %s/%s last backup %.0fh ago", pbsInstance, backup.BackupType, backup.BackupID, age.Hours()),
						Instance:  pbsInstance,
						Subject:   backup.BackupID,
						Timestamp: now,
						Metadata: map[string]string{
							"age":         fmt.Sprintf("%.0fh", age.Hours()),
							"backup_type": backup.BackupType,
						},
					})
				}
			}
		}
	}

	// Disk SMART alerts
	if a.config.DiskSmartFailed != nil {
		for wwn, disk := range snap.Disks {
			if disk.Health == "FAILED" || disk.Status&model.StatusFailedSmart != 0 {
				key := fmt.Sprintf("disk_smart:%s", wwn)
				a.fire(ctx, now, key, a.config.DiskSmartFailed.Cooldown, model.Notification{
					AlertType: "disk_smart_failed",
					Severity:  a.config.DiskSmartFailed.Severity,
					Title:     fmt.Sprintf("Disk SMART Failed: %s", disk.DevPath),
					Message:   fmt.Sprintf("[%s/%s] %s (%s) SMART health: %s", disk.Instance, disk.Node, disk.DevPath, disk.Model, disk.Health),
					Instance:  disk.Instance,
					Subject:   disk.DevPath,
					Timestamp: now,
					Metadata: map[string]string{
						"wwn":   wwn,
						"model": disk.Model,
					},
				})
			}
			if disk.Status&model.StatusWarnScrutiny != 0 || disk.Status&model.StatusFailedScrutiny != 0 {
				key := fmt.Sprintf("disk_scrutiny:%s", wwn)
				a.fire(ctx, now, key, a.config.DiskSmartFailed.Cooldown, model.Notification{
					AlertType: "disk_scrutiny_warning",
					Severity:  "warning",
					Title:     fmt.Sprintf("Disk Scrutiny Warning: %s", disk.DevPath),
					Message:   fmt.Sprintf("[%s/%s] %s (%s) has elevated SMART risk indicators", disk.Instance, disk.Node, disk.DevPath, disk.Model),
					Instance:  disk.Instance,
					Subject:   disk.DevPath,
					Timestamp: now,
					Metadata:  map[string]string{"wwn": wwn, "model": disk.Model},
				})
			}
		}
	}

	// Datastore full alerts
	if a.config.DatastoreFull != nil {
		for pbsInstance, datastores := range snap.Datastores {
			for _, ds := range datastores {
				if ds.TotalBytes != nil && ds.UsedBytes != nil && *ds.TotalBytes > 0 {
					pct := float64(*ds.UsedBytes) / float64(*ds.TotalBytes) * 100
					if pct >= a.config.DatastoreFull.Threshold {
						key := fmt.Sprintf("ds_full:%s/%s", pbsInstance, ds.Name)
						a.fire(ctx, now, key, a.config.DatastoreFull.Cooldown, model.Notification{
							AlertType: "datastore_full",
							Severity:  a.config.DatastoreFull.Severity,
							Title:     fmt.Sprintf("Datastore Full: %s/%s", pbsInstance, ds.Name),
							Message:   fmt.Sprintf("[%s] Datastore %s at %.0f%% capacity", pbsInstance, ds.Name, pct),
							Instance:  pbsInstance,
							Subject:   ds.Name,
							Timestamp: now,
							Metadata:  map[string]string{"usage_pct": fmt.Sprintf("%.0f", pct)},
						})
					}
				}
				if ds.Error != nil {
					key := fmt.Sprintf("ds_offline:%s/%s", pbsInstance, ds.Name)
					a.fire(ctx, now, key, 1*time.Hour, model.Notification{
						AlertType: "datastore_offline",
						Severity:  "critical",
						Title:     fmt.Sprintf("Datastore Offline: %s/%s", pbsInstance, ds.Name),
						Message:   fmt.Sprintf("[%s] Datastore %s error: %s", pbsInstance, ds.Name, *ds.Error),
						Instance:  pbsInstance,
						Subject:   ds.Name,
						Timestamp: now,
					})
				}
			}
		}
	}
}

func (a *Alerter) checkSustainedThreshold(ctx context.Context, now time.Time, key string, value float64, cfg *ThresholdAlert, notif model.Notification) {
	if value >= cfg.Threshold {
		if first, ok := a.sustained[key]; ok {
			if now.Sub(first) >= cfg.Duration {
				a.fire(ctx, now, key, cfg.Cooldown, notif)
			}
		} else {
			a.sustained[key] = now
		}
	} else {
		delete(a.sustained, key)
	}
}

func (a *Alerter) fire(ctx context.Context, now time.Time, key string, cooldown time.Duration, notif model.Notification) {
	if last, ok := a.lastFired[key]; ok && now.Sub(last) < cooldown {
		return // still in cooldown
	}
	a.lastFired[key] = now

	// Log to store
	if err := a.store.InsertAlert(now.Unix(), notif.AlertType, notif.Instance, notif.Subject, notif.Message, notif.Severity); err != nil {
		slog.Error("storing alert", "type", notif.AlertType, "error", err)
	}

	// Send to all providers
	for _, p := range a.providers {
		if err := p.Send(ctx, notif); err != nil {
			slog.Error("sending notification", "provider", p.Name(), "alert", notif.AlertType, "error", err)
		}
	}

	slog.Warn("alert fired",
		"type", notif.AlertType,
		"severity", notif.Severity,
		"instance", notif.Instance,
		"subject", notif.Subject,
		"title", notif.Title,
	)
}

// FormatSeverity returns an uppercase severity string for templates.
func FormatSeverity(s string) string {
	return strings.ToUpper(s)
}
