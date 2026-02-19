package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/darshan-rambhia/glint/internal/alerter"
	"github.com/darshan-rambhia/glint/internal/api"
	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/collector"
	"github.com/darshan-rambhia/glint/internal/config"
	"github.com/darshan-rambhia/glint/internal/notify"
	"github.com/darshan-rambhia/glint/internal/store"
	"golang.org/x/sync/errgroup"
)

// @title Glint API
// @version 1.0
// @description Lightweight Proxmox monitoring dashboard API
// @host localhost:3800
// @BasePath /

var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// buildInfo returns version, commit, build time, and VCS details from the
// embedded Go build info. ldflags-injected values take priority; VCS info
// from debug.ReadBuildInfo fills in anything left as default.
func buildInfo() (ver, sha, built, dirty string) {
	ver = version
	sha = commit
	built = buildTime
	dirty = "clean"

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if sha == "none" {
				sha = s.Value
			}
		case "vcs.time":
			if built == "unknown" {
				built = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "dirty"
			}
		}
	}

	return
}

func main() {
	configPath := flag.String("config", "", "path to glint.yml config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	ver, sha, built, dirty := buildInfo()

	if *showVersion {
		fmt.Printf("glint %s\n  commit:    %s (%s)\n  built:     %s\n  go:        %s\n  platform:  %s/%s\n",
			ver, sha, dirty, built, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, config.ErrConfigFileNotFound) {
			fmt.Fprintf(os.Stderr, "error: %s\n\n", err)
			fmt.Fprintf(os.Stderr, "Copy the example config to get started:\n")
			fmt.Fprintf(os.Stderr, "  cp glint.example.yml %s\n\n", *configPath)
			fmt.Fprintf(os.Stderr, "Docs: https://darshan-rambhia.github.io/glint/configuration/\n")
		} else {
			fmt.Fprintf(os.Stderr, "error: loading config (%s): %s\n", *configPath, err)
		}
		os.Exit(1)
	}

	// Configure logging
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: logLevel}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))

	slog.Info("starting glint",
		"version", ver,
		"commit", sha,
		"built", built,
		"dirty", dirty,
		"go", runtime.Version(),
		"listen", cfg.Listen,
	)

	// Initialize store
	st, err := store.New(cfg.DBPath)
	if err != nil {
		slog.Error("opening database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// Initialize cache
	c := cache.New()

	// Initialize worker pool
	pool := collector.NewWorkerPool(cfg.WorkerPoolSize)

	// Setup context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// Start PVE collectors
	for _, pveCfg := range cfg.PVE {
		collCfg := collector.PVEConfig{
			Name:             pveCfg.Name,
			Host:             pveCfg.Host,
			TokenID:          pveCfg.TokenID,
			TokenSecret:      pveCfg.TokenSecret,
			Insecure:         pveCfg.Insecure,
			PollInterval:     pveCfg.PollInterval.Duration,
			DiskPollInterval: pveCfg.DiskPollInterval.Duration,
		}
		if collCfg.PollInterval == 0 {
			collCfg.PollInterval = 15 * time.Second
		}
		if collCfg.DiskPollInterval == 0 {
			collCfg.DiskPollInterval = 1 * time.Hour
		}

		// Register PVE instance
		if err := st.UpsertPVEInstance(pveCfg.Name, pveCfg.Host, false, ""); err != nil {
			slog.Error("registering PVE instance", "name", pveCfg.Name, "error", err)
		}

		pveCollector := collector.NewPVECollector(collCfg, pool, c, st)
		g.Go(func() error { return collector.Run(ctx, pveCollector) })

		// Start temperature collector if SSH configured
		if pveCfg.SSH != nil {
			sshCfg := collector.SSHConfig{
				Host:    pveCfg.SSH.Host,
				User:    pveCfg.SSH.User,
				KeyPath: pveCfg.SSH.KeyPath,
			}
			// Temperature collector runs for the first discovered node
			// In a real multi-node setup, we'd create one per node
			tempCollector, err := collector.NewTempCollector(pveCfg.Name, pveCfg.Name, sshCfg, c)
			if err != nil {
				slog.Error("failed to create temperature collector", "instance", pveCfg.Name, "error", err)
			} else {
				g.Go(func() error { return collector.Run(ctx, tempCollector) })
			}
		}
	}

	// Start PBS collectors
	for _, pbsCfg := range cfg.PBS {
		collCfg := collector.PBSConfig{
			Name:         pbsCfg.Name,
			Host:         pbsCfg.Host,
			TokenID:      pbsCfg.TokenID,
			TokenSecret:  pbsCfg.TokenSecret,
			Insecure:     pbsCfg.Insecure,
			Datastores:   pbsCfg.Datastores,
			PollInterval: pbsCfg.PollInterval.Duration,
		}
		if collCfg.PollInterval == 0 {
			collCfg.PollInterval = 5 * time.Minute
		}

		// Register PBS instance
		if err := st.UpsertPBSInstance(pbsCfg.Name, pbsCfg.Host); err != nil {
			slog.Error("registering PBS instance", "name", pbsCfg.Name, "error", err)
		}

		pbsCollector := collector.NewPBSCollector(collCfg, pool, c, st)
		g.Go(func() error { return collector.Run(ctx, pbsCollector) })
	}

	// Start pruner
	pruner := store.NewPruner(st, store.DefaultRetention())
	g.Go(func() error { return pruner.Run(ctx) })

	// Build notification providers
	var providers []notify.Provider
	for _, ncfg := range cfg.Notifications {
		switch ncfg.Type {
		case "ntfy":
			providers = append(providers, notify.NewNtfy(ncfg.URL, ncfg.Topic))
		case "webhook":
			method := ncfg.Method
			if method == "" {
				method = "POST"
			}
			providers = append(providers, notify.NewWebhook(ncfg.URL, method, ncfg.Headers))
		}
	}

	// Start alerter
	alertCfg := alerter.DefaultAlertConfig()
	if cfg.Alerts.NodeCPUHigh != nil {
		alertCfg.NodeCPUHigh.Threshold = cfg.Alerts.NodeCPUHigh.Threshold
		alertCfg.NodeCPUHigh.Duration = cfg.Alerts.NodeCPUHigh.Duration.Duration
		if cfg.Alerts.NodeCPUHigh.Severity != "" {
			alertCfg.NodeCPUHigh.Severity = cfg.Alerts.NodeCPUHigh.Severity
		}
	}
	if cfg.Alerts.GuestDown != nil {
		alertCfg.GuestDown.GracePeriod = cfg.Alerts.GuestDown.GracePeriod.Duration
		if cfg.Alerts.GuestDown.Severity != "" {
			alertCfg.GuestDown.Severity = cfg.Alerts.GuestDown.Severity
		}
	}
	if cfg.Alerts.BackupStale != nil {
		alertCfg.BackupStale.MaxAge = cfg.Alerts.BackupStale.MaxAge.Duration
		if cfg.Alerts.BackupStale.Severity != "" {
			alertCfg.BackupStale.Severity = cfg.Alerts.BackupStale.Severity
		}
	}
	if cfg.Alerts.DatastoreFull != nil {
		alertCfg.DatastoreFull.Threshold = cfg.Alerts.DatastoreFull.Threshold
		if cfg.Alerts.DatastoreFull.Severity != "" {
			alertCfg.DatastoreFull.Severity = cfg.Alerts.DatastoreFull.Severity
		}
	}

	a := alerter.NewAlerter(c, st, providers, alertCfg)
	g.Go(func() error { return a.Run(ctx) })

	// Start HTTP server
	server := api.NewServer(cfg.Listen, c, st)
	g.Go(func() error { return server.Run(ctx) })

	slog.Info("all components started",
		"pve_instances", len(cfg.PVE),
		"pbs_instances", len(cfg.PBS),
		"notifications", len(providers),
	)

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("fatal error", "error", err)
	}

	slog.Info("glint stopped gracefully")
}
