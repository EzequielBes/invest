package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"execution/paperstore"

	"web-api/internal/storage"
)

type fakeStore struct {
	decisions        []storage.Decision
	decisionsErr     error
	paperDecisions   []storage.Decision
	paperDecErr      error
	riskState        storage.RiskStateResponse
	riskStateErr     error
	analysisRuns     []storage.AnalysisRun
	analysisErr      error
	analysisDetail   storage.AnalysisRunDetail
	backtests        []storage.BacktestRun
	backtestsErr     error
	backtestDetail   storage.BacktestDetail
	backtestErr      error
	validationRuns   []storage.ValidationRun
	validationErr    error
	validationDetail storage.ValidationRunDetail
	equitySnapshots  []storage.EquityPoint
	equityErr        error
	news             []storage.NewsItem
	newsErr          error
	price            float64
	priceFound       bool
	priceErr         error
	lastLimit        int
}

func (f *fakeStore) RecentDecisions(_ context.Context, limit int) ([]storage.Decision, error) {
	f.lastLimit = limit
	return f.decisions, f.decisionsErr
}
func (f *fakeStore) RecentPaperDecisions(_ context.Context, limit int) ([]storage.Decision, error) {
	f.lastLimit = limit
	return f.paperDecisions, f.paperDecErr
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
func (f *fakeStore) RecentValidationRuns(_ context.Context, limit int) ([]storage.ValidationRun, error) {
	f.lastLimit = limit
	return f.validationRuns, f.validationErr
}
func (f *fakeStore) ValidationRunDetail(context.Context, string) (storage.ValidationRunDetail, error) {
	return f.validationDetail, f.validationErr
}
func (f *fakeStore) RecentEquitySnapshots(_ context.Context, limit int) ([]storage.EquityPoint, error) {
	f.lastLimit = limit
	return f.equitySnapshots, f.equityErr
}
func (f *fakeStore) RecentNews(_ context.Context, limit int) ([]storage.NewsItem, error) {
	f.lastLimit = limit
	return f.news, f.newsErr
}
func (f *fakeStore) LatestPrice(context.Context, string, string, string) (float64, bool, error) {
	return f.price, f.priceFound, f.priceErr
}

type fakePaperStore struct {
	enabled       bool
	enabledErr    error
	setEnabledErr error
	cash          float64
	positions     map[string]float64
	portfolioErr  error
	fills         []paperstore.Fill
	fillsErr      error
}

func (f *fakePaperStore) Enabled(context.Context) (bool, error) { return f.enabled, f.enabledErr }
func (f *fakePaperStore) SetEnabled(_ context.Context, enabled bool) error {
	f.enabled = enabled
	return f.setEnabledErr
}
func (f *fakePaperStore) Portfolio(context.Context) (float64, map[string]float64, error) {
	return f.cash, f.positions, f.portfolioErr
}
func (f *fakePaperStore) RecentFills(context.Context, int) ([]paperstore.Fill, error) {
	return f.fills, f.fillsErr
}

// newTestServer builds a server for tests that never hit POST
// /api/backtests (the only handler that uses dsn/riskStore) — a nil
// riskStore there would panic, which is exactly the assertion we want
// if a test ever accidentally exercises that route without setting one
// up for real. paper defaults to an empty fakePaperStore when the test
// doesn't care about the simulation endpoints.
func newTestServer(store dataStore) http.Handler {
	return NewServer(store, "", nil, &fakePaperStore{positions: map[string]float64{}}, "")
}

func newTestServerWithPaper(store dataStore, paper paperStore) http.Handler {
	return NewServer(store, "", nil, paper, "")
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
	for _, path := range []string{"/api/analysis-runs/missing", "/api/backtests/missing", "/api/validation-runs/missing"} {
		t.Run(path, func(t *testing.T) {
			store := &fakeStore{analysisErr: storage.ErrNotFound, backtestErr: storage.ErrNotFound, validationErr: storage.ErrNotFound}
			recorder := httptest.NewRecorder()
			newTestServer(store).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", recorder.Code)
			}
		})
	}
}

func TestValidationRunsEndpointsReturnReports(t *testing.T) {
	store := &fakeStore{
		validationRuns: []storage.ValidationRun{{ID: "vr1", Status: "completed", ConfigHash: "abc123"}},
		validationDetail: storage.ValidationRunDetail{
			Run:      storage.ValidationRun{ID: "vr1", Status: "completed", ConfigHash: "abc123"},
			Metrics:  []storage.ValidationMetric{{Name: "max_drawdown_pct", Value: 12.5, Unit: "pct", Segment: "backtest"}},
			Findings: []storage.ValidationFinding{{Severity: "warning", Rule: "declared_cost", Message: "costs were declared"}},
		},
	}
	server := newTestServer(store)
	for _, path := range []string{"/api/validation-runs?limit=1", "/api/validation-runs/vr1"} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "completed") {
			t.Fatalf("%s status/body = %d/%s, want validation report", path, recorder.Code, recorder.Body.String())
		}
	}
	if store.lastLimit != 1 {
		t.Errorf("limit = %d, want 1", store.lastLimit)
	}
}

func TestValidationRunDetailReturnsNotFoundForRunningRun(t *testing.T) {
	store := &fakeStore{validationErr: storage.ErrNotFound}
	recorder := httptest.NewRecorder()
	newTestServer(store).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/validation-runs/running", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
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

func TestSimulationStatus_ReturnsEnabledCashAndPositions(t *testing.T) {
	paper := &fakePaperStore{enabled: true, cash: 8500, positions: map[string]float64{"BTC": 0.1}}
	server := newTestServerWithPaper(&fakeStore{}, paper)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/simulation/status", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"enabled":true`) || !strings.Contains(recorder.Body.String(), "8500") {
		t.Fatalf("status/body = %d/%s, want 200 with enabled true and cash 8500", recorder.Code, recorder.Body.String())
	}
}

func TestSimulationToggle_FlipsEnabledAndReturnsStatus(t *testing.T) {
	paper := &fakePaperStore{enabled: false, positions: map[string]float64{}}
	server := newTestServerWithPaper(&fakeStore{}, paper)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"enabled":true}`)
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/simulation/toggle", body))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"enabled":true`) {
		t.Fatalf("status/body = %d/%s, want 200 with enabled true", recorder.Code, recorder.Body.String())
	}
	if !paper.enabled {
		t.Error("SetEnabled was not called with true")
	}
}

func TestPaperDecisionsEndpointReturnsJSON(t *testing.T) {
	store := &fakeStore{paperDecisions: []storage.Decision{{ID: "pd1", Asset: "ETH"}}}
	server := newTestServer(store)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/paper-decisions", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ETH"`) {
		t.Fatalf("status/body = %d/%s, want 200 with the paper decision", recorder.Code, recorder.Body.String())
	}
}
