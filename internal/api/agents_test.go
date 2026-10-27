package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darshan-rambhia/glint/internal/agentauth"
	"github.com/darshan-rambhia/glint/internal/hostagent"
	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const agentSecret = "0123456789abcdef0123456789abcdef"

func newIngestServer(t *testing.T) (*Server, func(body []byte, agent string) *httptest.ResponseRecorder) {
	t.Helper()
	srv, _, _ := newTestServer(t)
	srv.EnableAgentIngest(
		agentauth.NewVerifier(map[string]string{"edge": agentSecret}),
		map[string]time.Duration{"edge": time.Hour},
	)
	post := func(body []byte, agent string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, hostagent.ReportPath, bytes.NewReader(body))
		require.NoError(t, agentauth.Sign(req, agent, agentSecret, body, time.Now()))
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		return rec
	}
	return srv, post
}

func reportJSON(t *testing.T, r model.HostReport) []byte {
	t.Helper()
	b, err := json.Marshal(r)
	require.NoError(t, err)
	return b
}

func TestAgentReport_Accepted(t *testing.T) {
	srv, post := newIngestServer(t)
	rec := post(reportJSON(t, model.HostReport{
		Agent:   "edge",
		Updates: model.HostUpdates{Pending: 3, Security: 1},
		// A client-supplied Received block must be ignored.
		Received: &model.HostReceived{At: 1, SecuritySince: 1},
	}), "edge")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	h := srv.cache.Host("edge")
	require.NotNil(t, h)
	assert.Equal(t, 3, h.Updates.Pending)
	assert.WithinDuration(t, time.Now(), time.Unix(h.Received.At, 0), 5*time.Second)
	assert.Equal(t, h.Received.At, h.Received.SecuritySince)
	assert.Equal(t, int64(3600), h.Received.Interval)

	stored, err := srv.store.LoadHostReports()
	require.NoError(t, err)
	assert.Equal(t, 1, stored["edge"].Updates.Security)
}

func TestAgentReport_SecuritySinceCarriesOverAndClears(t *testing.T) {
	srv, post := newIngestServer(t)
	srv.cache.UpdateHost(&model.HostReport{
		Agent:    "edge",
		Updates:  model.HostUpdates{Security: 2},
		Received: &model.HostReceived{At: 100, SecuritySince: 100},
	})

	require.Equal(t, http.StatusNoContent, post(reportJSON(t, model.HostReport{
		Agent: "edge", Updates: model.HostUpdates{Security: 1},
	}), "edge").Code)
	assert.Equal(t, int64(100), srv.cache.Host("edge").Received.SecuritySince)

	require.Equal(t, http.StatusNoContent, post(reportJSON(t, model.HostReport{Agent: "edge"}), "edge").Code)
	assert.Zero(t, srv.cache.Host("edge").Received.SecuritySince)
}

func TestAgentReport_TruncatesPackages(t *testing.T) {
	srv, post := newIngestServer(t)
	r := model.HostReport{Agent: "edge"}
	for range hostagent.MaxPackages + 10 {
		r.Updates.Packages = append(r.Updates.Packages, model.HostPackage{Name: "p"})
	}
	require.Equal(t, http.StatusNoContent, post(reportJSON(t, r), "edge").Code)
	assert.Len(t, srv.cache.Host("edge").Updates.Packages, hostagent.MaxPackages)
}

func TestAgentReport_Rejections(t *testing.T) {
	_, post := newIngestServer(t)

	assert.Equal(t, http.StatusBadRequest, post([]byte("not json"), "edge").Code)
	assert.Equal(t, http.StatusBadRequest,
		post(reportJSON(t, model.HostReport{Agent: "someone-else"}), "edge").Code,
		"a signer may only report for itself")
	assert.Equal(t, http.StatusUnauthorized, post(reportJSON(t, model.HostReport{Agent: "ghost"}), "ghost").Code)
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		post(bytes.Repeat([]byte("a"), maxReportBytes+1), "edge").Code)
}

func TestAgentReport_Unsigned(t *testing.T) {
	srv, _ := newIngestServer(t)
	req := httptest.NewRequest(http.MethodPost, hostagent.ReportPath, strings.NewReader(`{"agent":"edge"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, srv.cache.Host("edge"))
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestAgentReport_BodyReadError(t *testing.T) {
	srv, _ := newIngestServer(t)
	req := httptest.NewRequest(http.MethodPost, hostagent.ReportPath, errReader{})
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAgentReport_StoreFailure(t *testing.T) {
	srv, post := newIngestServer(t)
	require.NoError(t, srv.store.Close())
	assert.Equal(t, http.StatusInternalServerError, post(reportJSON(t, model.HostReport{Agent: "edge"}), "edge").Code)
}

func TestAgentReport_NotRegisteredByDefault(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, hostagent.ReportPath, strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "only GET / matches when ingest is off")
}
