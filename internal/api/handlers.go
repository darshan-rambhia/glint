// Package api provides HTTP handlers for the Glint dashboard.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/darshan-rambhia/glint/internal/cache"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/darshan-rambhia/glint/internal/store"
	"github.com/darshan-rambhia/glint/templates"
	"github.com/darshan-rambhia/glint/templates/components"
	httpSwagger "github.com/swaggo/http-swagger"

	_ "github.com/darshan-rambhia/glint/docs/swagger"
)

// Server is the HTTP server for Glint.
type Server struct {
	cache  *cache.Cache
	store  *store.Store
	mux    *http.ServeMux
	server *http.Server
}

// NewServer creates a new HTTP server.
func NewServer(addr string, c *cache.Cache, s *store.Store) *Server {
	srv := &Server{
		cache: c,
		store: s,
		mux:   http.NewServeMux(),
	}

	srv.registerRoutes()

	srv.server = &http.Server{
		Addr:         addr,
		Handler:      SecurityHeadersMiddleware(RecoveryMiddleware(LoggingMiddleware(srv.mux))),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv
}

// Run starts the HTTP server. It blocks until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	slog.Info("HTTP server starting", "addr", s.server.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("HTTP server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) registerRoutes() {
	// Static files
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Full page
	s.mux.HandleFunc("GET /", s.handleDashboard)

	// htmx fragment endpoints
	s.mux.HandleFunc("GET /fragments/nodes", s.handleNodesFragment)
	s.mux.HandleFunc("GET /fragments/guests", s.handleGuestsFragment)
	s.mux.HandleFunc("GET /fragments/backups", s.handleBackupsFragment)
	s.mux.HandleFunc("GET /fragments/events", s.handleEventsFragment)
	s.mux.HandleFunc("GET /fragments/disks", s.handleDisksFragment)
	s.mux.HandleFunc("GET /fragments/disk/{wwn}", s.handleDiskDetailFragment)

	// SVG sparkline fragment endpoints (for htmx)
	s.mux.HandleFunc("GET /fragments/sparkline/node/{instance}/{node}", s.handleNodeSparklineSVG)
	s.mux.HandleFunc("GET /fragments/sparkline/guest/{instance}/{vmid}", s.handleGuestSparklineSVG)

	// API endpoints (JSON)
	s.mux.HandleFunc("GET /api/sparkline/node/{instance}/{node}", s.handleNodeSparkline)
	s.mux.HandleFunc("GET /api/sparkline/guest/{instance}/{vmid}", s.handleGuestSparkline)

	// Health check
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Dashboard widget summary
	s.mux.HandleFunc("GET /api/widget", s.handleWidget)

	// Swagger UI
	s.mux.Handle("GET /swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))
}

// renderHTML renders a templ component to a buffer first, then writes the
// buffer to the response. This ensures rendering errors can be returned as a
// proper 500 before any bytes reach the client.
func renderHTML(w http.ResponseWriter, r *http.Request, component templ.Component) {
	var buf bytes.Buffer
	if err := component.Render(r.Context(), &buf); err != nil {
		slog.Error("rendering component", "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := buf.WriteTo(w); err != nil {
		// Client disconnected after headers sent — nothing to recover.
		slog.Debug("writing HTML response", "path", r.URL.Path, "error", err)
	}
}

// writeJSON marshals v to JSON into a buffer first, then writes it to the
// response. This ensures marshalling errors can be returned as a proper 500.
func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("encoding JSON response", "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		slog.Debug("writing JSON response", "path", r.URL.Path, "error", err)
	}
}

// @Summary Dashboard page
// @Description Full HTML dashboard page
// @Produce html
// @Success 200 {string} string "HTML page"
// @Router / [get]
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	snap := s.cache.Snapshot()
	renderHTML(w, r, templates.Dashboard(snap))
}

// @Summary Node cards fragment
// @Description Returns HTML fragment of node status cards for htmx
// @Produce html
// @Success 200 {string} string "HTML fragment"
// @Router /fragments/nodes [get]
func (s *Server) handleNodesFragment(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	renderHTML(w, r, templates.NodesFragment(snap))
}

// @Summary Guest table fragment
// @Description Returns HTML fragment of guest (LXC/QEMU) table for htmx
// @Produce html
// @Success 200 {string} string "HTML fragment"
// @Router /fragments/guests [get]
func (s *Server) handleGuestsFragment(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	renderHTML(w, r, templates.GuestsFragment(snap))
}

// @Summary Backup status fragment
// @Description Returns HTML fragment of PBS backup status for htmx
// @Produce html
// @Success 200 {string} string "HTML fragment"
// @Router /fragments/backups [get]
func (s *Server) handleBackupsFragment(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	renderHTML(w, r, templates.BackupsFragment(snap))
}

// @Summary Events fragment
// @Description Returns HTML fragment of PBS server-side task events for htmx
// @Produce html
// @Success 200 {string} string "HTML fragment"
// @Router /fragments/events [get]
func (s *Server) handleEventsFragment(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	renderHTML(w, r, templates.EventsFragment(snap))
}

// @Summary Disk health fragment
// @Description Returns HTML fragment of disk health table for htmx
// @Produce html
// @Success 200 {string} string "HTML fragment"
// @Router /fragments/disks [get]
func (s *Server) handleDisksFragment(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	renderHTML(w, r, templates.DisksFragment(snap))
}

// @Summary Disk detail fragment
// @Description Returns HTML fragment with SMART details for a specific disk
// @Produce html
// @Param wwn path string true "Disk WWN identifier"
// @Success 200 {string} string "HTML fragment"
// @Failure 404 {string} string "Disk not found"
// @Router /fragments/disk/{wwn} [get]
func (s *Server) handleDiskDetailFragment(w http.ResponseWriter, r *http.Request) {
	wwn := r.PathValue("wwn")
	snap := s.cache.Snapshot()
	disk, ok := snap.Disks[wwn]
	if !ok {
		http.NotFound(w, r)
		return
	}
	renderHTML(w, r, templates.DiskDetail(disk))
}

// @Summary Node sparkline data
// @Description Returns JSON array of time-series data points for a node metric
// @Produce json
// @Param instance path string true "PVE instance name"
// @Param node path string true "Node name"
// @Param hours query int false "Hours of history (1-168)" default(24)
// @Param metric query string false "Metric name (cpu, memory)" default(cpu)
// @Success 200 {array} model.SparklinePoint
// @Failure 500 {string} string "Internal Server Error"
// @Router /api/sparkline/node/{instance}/{node} [get]
func (s *Server) handleNodeSparkline(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	node := r.PathValue("node")
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 && v <= 168 {
			hours = v
		}
	}

	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "cpu"
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	points, err := s.store.QueryNodeSparkline(instance, node, metric, since)
	if err != nil {
		slog.Error("querying node sparkline", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, r, points)
}

// @Summary Guest sparkline data
// @Description Returns JSON array of CPU usage data points for a guest
// @Produce json
// @Param instance path string true "PVE instance name"
// @Param vmid path int true "Guest VMID"
// @Success 200 {array} model.SparklinePoint
// @Failure 400 {string} string "Invalid VMID"
// @Failure 500 {string} string "Internal Server Error"
// @Router /api/sparkline/guest/{instance}/{vmid} [get]
func (s *Server) handleGuestSparkline(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	vmidStr := r.PathValue("vmid")
	vmid, err := strconv.Atoi(vmidStr)
	if err != nil {
		http.Error(w, "Invalid VMID", http.StatusBadRequest)
		return
	}

	since := time.Now().Add(-24 * time.Hour).Unix()
	points, err := s.store.QueryGuestSparkline(instance, vmid, since)
	if err != nil {
		slog.Error("querying guest sparkline", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, r, points)
}

// @Summary Node sparkline SVG fragment
// @Description Returns HTML/SVG sparkline visualization for a node metric
// @Produce html
// @Param instance path string true "PVE instance name"
// @Param node path string true "Node name"
// @Param hours query int false "Hours of history (1-168)" default(24)
// @Param metric query string false "Metric name (cpu, memory)" default(cpu)
// @Success 200 {string} string "SVG sparkline HTML"
// @Failure 500 {string} string "Internal Server Error"
// @Router /fragments/sparkline/node/{instance}/{node} [get]
func (s *Server) handleNodeSparklineSVG(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	node := r.PathValue("node")
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && v > 0 && v <= 168 {
			hours = v
		}
	}

	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "cpu"
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	points, err := s.store.QueryNodeSparkline(instance, node, metric, since)
	if err != nil {
		slog.Error("querying node sparkline SVG", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	label := fmt.Sprintf("%s %dh", metric, hours)
	renderHTML(w, r, components.SparklineSVG(points, label))
}

// @Summary Guest sparkline SVG fragment
// @Description Returns HTML/SVG sparkline visualization for a guest's CPU usage
// @Produce html
// @Param instance path string true "PVE instance name"
// @Param vmid path int true "Guest VMID"
// @Success 200 {string} string "SVG sparkline HTML"
// @Failure 400 {string} string "Invalid VMID"
// @Failure 500 {string} string "Internal Server Error"
// @Router /fragments/sparkline/guest/{instance}/{vmid} [get]
func (s *Server) handleGuestSparklineSVG(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	vmidStr := r.PathValue("vmid")
	vmid, err := strconv.Atoi(vmidStr)
	if err != nil {
		http.Error(w, "Invalid VMID", http.StatusBadRequest)
		return
	}

	since := time.Now().Add(-24 * time.Hour).Unix()
	points, err := s.store.QueryGuestSparkline(instance, vmid, since)
	if err != nil {
		slog.Error("querying guest sparkline SVG", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	renderHTML(w, r, components.SparklineSVG(points, "cpu 24h"))
}

// widgetResponse is the response body for GET /api/widget.
type widgetResponse struct {
	Nodes   widgetNodeStats   `json:"nodes"`
	Guests  widgetGuestStats  `json:"guests"`
	CPU     widgetCPUStats    `json:"cpu"`
	Memory  widgetMemStats    `json:"memory"`
	Disks   widgetDiskStats   `json:"disks"`
	Backups widgetBackupStats `json:"backups"`
}

type widgetNodeStats struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
}

type widgetGuestStats struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
	VMs     int `json:"vms"`
	LXC     int `json:"lxc"`
}

type widgetCPUStats struct {
	UsagePct float64 `json:"usage_pct"`
}

type widgetMemStats struct {
	UsedBytes  int64   `json:"used_bytes"`
	TotalBytes int64   `json:"total_bytes"`
	UsagePct   float64 `json:"usage_pct"`
}

type widgetDiskStats struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Warning int `json:"warning"`
	Unknown int `json:"unknown"`
}

type widgetBackupStats struct {
	Total          int   `json:"total"`
	LastBackupTime int64 `json:"last_backup_time"`
}

// @Summary Dashboard widget data
// @Description Returns cluster summary statistics for homepage-style dashboard widgets (Homepage, Glance, Dashy, etc.)
// @Produce json
// @Success 200 {object} widgetResponse
// @Router /api/widget [get]
func (s *Server) handleWidget(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	var resp widgetResponse

	// Nodes and cluster-level CPU / memory (online nodes only).
	var cpuSum float64
	var cpuCount int
	for _, nodes := range snap.Nodes {
		for _, n := range nodes {
			resp.Nodes.Total++
			if n.Status == "online" {
				resp.Nodes.Online++
				cpuSum += n.CPU * 100
				cpuCount++
				resp.Memory.UsedBytes += n.Memory.Used
				resp.Memory.TotalBytes += n.Memory.Total
			} else {
				resp.Nodes.Offline++
			}
		}
	}
	if cpuCount > 0 {
		resp.CPU.UsagePct = cpuSum / float64(cpuCount)
	}
	if resp.Memory.TotalBytes > 0 {
		resp.Memory.UsagePct = float64(resp.Memory.UsedBytes) / float64(resp.Memory.TotalBytes) * 100
	}

	// Guests.
	for _, guests := range snap.Guests {
		for _, g := range guests {
			resp.Guests.Total++
			switch g.Status {
			case "running":
				resp.Guests.Running++
			case "stopped":
				resp.Guests.Stopped++
			}
			switch g.Type {
			case "qemu":
				resp.Guests.VMs++
			case "lxc":
				resp.Guests.LXC++
			}
		}
	}

	// Disks — categorise by SMART status bitfield.
	for _, d := range snap.Disks {
		resp.Disks.Total++
		switch {
		case d.Status == model.StatusPassed:
			resp.Disks.Passed++
		case d.Status&(model.StatusFailedSmart|model.StatusFailedScrutiny) != 0:
			resp.Disks.Failed++
		case d.Status&model.StatusWarnScrutiny != 0:
			resp.Disks.Warning++
		default:
			resp.Disks.Unknown++
		}
	}

	// Backups — total count and most recent timestamp.
	for _, backups := range snap.Backups {
		for _, b := range backups {
			resp.Backups.Total++
			if b.BackupTime > resp.Backups.LastBackupTime {
				resp.Backups.LastBackupTime = b.BackupTime
			}
		}
	}

	writeJSON(w, r, resp)
}

// @Summary Health check
// @Description Returns service health status and collector poll times
// @Produce json
// @Success 200 {object} map[string]interface{} "Health status"
// @Router /healthz [get]
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	snap := s.cache.Snapshot()
	healthy := len(snap.LastPoll) > 0

	status := "ok"
	if !healthy {
		status = "no_data"
	}

	collectors := make(map[string]string, len(snap.LastPoll))
	for k, v := range snap.LastPoll {
		collectors[k] = fmt.Sprintf("%ds ago", int(time.Since(v).Seconds()))
	}
	writeJSON(w, r, map[string]any{
		"status":     status,
		"timestamp":  time.Now().Unix(),
		"collectors": collectors,
	})
}
