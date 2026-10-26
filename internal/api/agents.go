package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/darshan-rambhia/glint/internal/agentauth"
	"github.com/darshan-rambhia/glint/internal/hostagent"
	"github.com/darshan-rambhia/glint/internal/model"
)

// maxReportBytes bounds an agent request body. A full report with the
// package list capped at hostagent.MaxPackages is well under this.
const maxReportBytes = 256 << 10

// EnableAgentIngest registers the agent report endpoint. intervals holds the
// expected report interval for each configured agent.
func (s *Server) EnableAgentIngest(v *agentauth.Verifier, intervals map[string]time.Duration) {
	s.verifier = v
	s.agentIntervals = intervals
	s.mux.HandleFunc("POST "+hostagent.ReportPath, s.handleAgentReport)
}

// @Summary Ingest a host agent report
// @Description Accepts a signed report from `glint agent`. Requests must carry X-Glint-Agent, X-Glint-Timestamp, X-Glint-Nonce and X-Glint-Signature headers (HMAC-SHA256, see docs/agent.md).
// @Accept json
// @Param report body model.HostReport true "Host report"
// @Success 204 "Accepted"
// @Failure 400 {string} string "Malformed report"
// @Failure 401 {string} string "Authentication failed"
// @Failure 413 {string} string "Report too large"
// @Router /api/v1/agents/report [post]
func (s *Server) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxReportBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "Report too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	agent, err := s.verifier.Verify(r, body)
	if err != nil {
		// One response for every auth failure so callers learn nothing about
		// which agent names exist.
		slog.Warn("rejected agent report",
			"agent", r.Header.Get(agentauth.HeaderAgent),
			"remote", r.RemoteAddr,
			"forwarded_for", r.Header.Get("X-Forwarded-For"),
			"reason", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var report model.HostReport
	if err := json.Unmarshal(body, &report); err != nil {
		http.Error(w, "Malformed report", http.StatusBadRequest)
		return
	}
	if report.Agent != agent {
		http.Error(w, "Report agent does not match signer", http.StatusBadRequest)
		return
	}
	if len(report.Updates.Packages) > hostagent.MaxPackages {
		report.Updates.Packages = report.Updates.Packages[:hostagent.MaxPackages]
	}

	now := time.Now().Unix()
	received := &model.HostReceived{At: now, Interval: int64(s.agentIntervals[agent].Seconds())}
	if report.Updates.Security > 0 {
		received.SecuritySince = now
		if prev := s.cache.Host(agent); prev != nil && prev.Received != nil && prev.Received.SecuritySince > 0 {
			received.SecuritySince = prev.Received.SecuritySince
		}
	}
	report.Received = received

	if err := s.store.SaveHostReport(&report); err != nil {
		slog.Error("storing agent report", "agent", agent, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Not recorded in LastPoll: agents report hourly, and the header's
	// "updated Ns ago" shows the oldest poll.
	s.cache.UpdateHost(&report)

	slog.Debug("agent report accepted", "agent", agent,
		"pending", report.Updates.Pending, "security", report.Updates.Security)
	w.WriteHeader(http.StatusNoContent)
}
