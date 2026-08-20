package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"web-api/internal/storage"
)

type fakeStore struct {
	decisions       []storage.Decision
	decisionsErr    error
	riskState       storage.RiskStateResponse
	riskStateErr    error
	analysisRuns    []storage.AnalysisRun
	analysisErr     error
	analysisDetail  storage.AnalysisRunDetail
	backtests       []storage.BacktestRun
	backtestsErr    error
	backtestDetail  storage.BacktestDetail
	backtestErr     error
	equitySnapshots []storage.EquityPoint
	equityErr       error
	news            []storage.NewsItem
	newsErr         error
	lastLimit       int
}

func (f *fakeStore) RecentDecisions(_ context.Context, limit int) ([]storage.Decision, error) {
	f.lastLimit = limit
	return f.decisions, f.decisionsErr
}
func (f *fakeStore) LiveRiskState(context.Context) (storage.RiskStateResponse, error) {
	return f.riskState, f.riskStateErr
}
func (f *fakeStore) RecentAnalysisRuns(_ context.Context, limit int) ([]storage.AnalysisRun, error) {
	f.lastLimit = limit
	return f.analysisRuns, f.analysisErr
}
func (f *fakeStore) AnalysisRunDetail(context.Context, string) (storage.AnalysisRunDetail, error) {
	return f.analysisDetail, f.analysisErr
}
func (f *fakeStore) RecentBacktests(_ context.Context, limit int) ([]storage.BacktestRun, error) {
	f.lastLimit = limit
	return f.backtests, f.backtestsErr
}
func (f *fakeStore) BacktestDetail(context.Context, string) (storage.BacktestDetail, error) {
	return f.backtestDetail, f.backtestErr
}
func (f *fakeStore) RecentEquitySnapshots(_ context.Context, limit int) ([]storage.EquityPoint, error) {
	f.lastLimit = limit
	return f.equitySnapshots, f.equityErr
}
func (f *fakeStore) RecentNews(_ context.Context, limit int) ([]storage.NewsItem, error) {
	f.lastLimit = limit
	return f.news, f.newsErr
}

// newTestServer builds a server for tests that never hit POST
// /api/backtests (the only handler that uses dsn/riskStore) — a nil
// riskStore there would panic, which is exactly the assertion we want
// if a test ever accidentally exercises that route without setting one
// up for real.
func newTestServer(store dataStore) http.Handler {
	return NewServer(store, "", nil, "")
}

func TestHandleDecisionsReturnsJSONList(t *testing.T) {
	server := newTestServer(&fakeStore{decisions: []storage.Decision{{ID: "d1", Asset: "BTC"}}})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/decisions", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"BTC"`) {
		t.Fatalf("status/body = %d/%s, want 200 JSON list", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
}

func TestParseLimitDefaultInvalidAndClamped(t *testing.T) {
	for name, test := range map[string]struct {
		path string
		want int
	}{
		"default": {"/api/decisions", defaultLimit},
		"invalid": {"/api/decisions?limit=nope", defaultLimit},
		"zero":    {"/api/decisions?limit=0", defaultLimit},
		"clamped": {"/api/decisions?limit=999999", maxLimit},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			newTestServer(store).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, test.path, nil))
			if store.lastLimit != test.want {
				t.Errorf("limit = %d, want %d", store.lastLimit, test.want)
			}
		})
	}
}

func TestDetailEndpointsReturnNotFound(t *testing.T) {
	for _, path := range []string{"/api/analysis-runs/missing", "/api/backtests/missing"} {
		t.Run(path, func(t *testing.T) {
			store := &fakeStore{analysisErr: storage.ErrNotFound, backtestErr: storage.ErrNotFound}
			recorder := httptest.NewRecorder()
			newTestServer(store).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", recorder.Code)
			}
		})
	}
}

func TestHandlerErrorsAreGeneric(t *testing.T) {
	server := newTestServer(&fakeStore{decisionsErr: errors.New("database password leaked")})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/decisions", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "database password leaked") {
		t.Error("response leaked internal error")
	}
}

func TestRiskStateEndpointReturnsJSON(t *testing.T) {
	server := newTestServer(&fakeStore{riskState: storage.RiskStateResponse{State: storage.RiskState{Status: "normal"}}})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/risk-state", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"normal"`) {
		t.Fatalf("status/body = %d/%s, want risk state", recorder.Code, recorder.Body.String())
	}
}

func TestEquitySnapshotsEndpointReturnsJSON(t *testing.T) {
	server := newTestServer(&fakeStore{equitySnapshots: []storage.EquityPoint{{TotalEquity: 1234.5}}})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/equity-snapshots", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "1234.5") {
		t.Fatalf("status/body = %d/%s, want 200 with the snapshot", recorder.Code, recorder.Body.String())
	}
}

func TestNewsEndpointReturnsJSON(t *testing.T) {
	server := newTestServer(&fakeStore{news: []storage.NewsItem{{Title: "Bitcoin hits new high"}}})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/news", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Bitcoin hits new high") {
		t.Fatalf("status/body = %d/%s, want 200 with the news item", recorder.Code, recorder.Body.String())
	}
}

func TestConfigStatusEndpointReturnsJSON(t *testing.T) {
	server := newTestServer(&fakeStore{})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/config-status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}
