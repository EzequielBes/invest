// Package runner exposes analysis's staged, provider-neutral workflow.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"analysis/internal/agents"
	"analysis/internal/ranking"
	"analysis/internal/storage"
)

type PrepareRequest struct {
	Assets     []string
	AssetNames map[string]string
	Timeframe  string
}

// ContextItem is deterministic source material for a subscription client.
type ContextItem struct {
	AgentType  string
	Asset      string
	Indicators json.RawMessage
}

type PreparedRun struct {
	RunID      string
	Context    []ContextItem
	Exclusions []risk.EligibilityResult
}

type NarrativeSubmission struct {
	Asset     string
	Narrative string
}

type Evidence = agents.Evidence
type CommitteeAssessment = agents.CommitteeAssessment

const stalePendingTimeout = 20 * time.Minute

const stalePendingError = "analysis run timed out waiting for staged submissions"

// CleanupStalePendingWithDSN fails pending runs left unfinished for 20 minutes.
func CleanupStalePendingWithDSN(ctx context.Context, dsn string) error {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	if _, err := store.FailPendingBefore(ctx, time.Now().UTC().Add(-stalePendingTimeout), stalePendingError); err != nil {
		return fmt.Errorf("fail stale pending analysis runs: %w", err)
	}
	return nil
}

// PrepareWithDSN collects and persists deterministic context in a pending run.
func PrepareWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, request PrepareRequest) (PreparedRun, error) {
	assets, err := validateRequest(request)
	if err != nil {
		return PreparedRun{}, err
	}
	if riskStore == nil {
		return PreparedRun{}, fmt.Errorf("risk store is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return PreparedRun{}, fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	assets, exclusions, err := eligibleAssets(ctx, riskStore, assets)
	if err != nil {
		return PreparedRun{}, err
	}
	if len(assets) == 0 {
		return PreparedRun{Exclusions: exclusions}, nil
	}

	runID := uuid.NewString()
	if err := store.CreateRun(ctx, storage.Run{ID: runID, StartedAt: time.Now().UTC(), Timeframe: request.Timeframe}); err != nil {
		return PreparedRun{}, fmt.Errorf("create pending run: %w", err)
	}
	if err := prepare(ctx, store, riskStore, runID, assets, request.AssetNames, request.Timeframe); err != nil {
		_ = store.FinishRun(context.WithoutCancel(ctx), runID, err)
		return PreparedRun{RunID: runID}, fmt.Errorf("prepare analysis context: %w", err)
	}
	preparedRun, err := prepared(ctx, store, runID)
	preparedRun.Exclusions = exclusions
	return preparedRun, err
}

func eligibleAssets(ctx context.Context, riskStore *riskstorage.Store, assets []string) ([]string, []risk.EligibilityResult, error) {
	eligible := make([]string, 0, len(assets))
	exclusions := make([]risk.EligibilityResult, 0)
	for _, asset := range assets {
		exchange := risk.ExchangeFor(asset)
		timeframe := "1m"
		metrics, err := riskStore.EligibilityMetrics(ctx, asset, exchange, timeframe)
		if err != nil {
			return nil, nil, fmt.Errorf("read eligibility metrics for %s: %w", asset, err)
		}
		result := risk.AssessEligibility(asset, metrics, time.Now().UTC(), exchange, timeframe)
		if result.Eligible {
			eligible = append(eligible, asset)
		} else {
			exclusions = append(exclusions, result)
		}
	}
	return eligible, exclusions, nil
}

// GetContextWithDSN retrieves deterministic context for a pending analysis run.
func GetContextWithDSN(ctx context.Context, dsn, runID string) (PreparedRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return PreparedRun{}, fmt.Errorf("analysis run ID is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return PreparedRun{}, fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	if pending, err := store.PendingRun(ctx, runID); err != nil {
		return PreparedRun{}, fmt.Errorf("read analysis run: %w", err)
	} else if !pending {
		return PreparedRun{}, fmt.Errorf("analysis run %s is not pending", runID)
	}
	return prepared(ctx, store, runID)
}

func prepare(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, runID string, assets []string, names map[string]string, timeframe string) error {
	for _, asset := range assets {
		output, err := agents.Technical(ctx, store, asset, timeframe)
		if err != nil {
			return err
		}
		if err := saveOutput(ctx, store, runID, "technical", asset, output); err != nil {
			return err
		}
		output, err = agents.Derivatives(ctx, store, asset)
		if err != nil {
			return err
		}
		if err := saveOutput(ctx, store, runID, "derivatives", asset, output); err != nil {
			return err
		}
		name := names[asset]
		if name == "" {
			name = asset
		}
		assetClass := "crypto"
		if !risk.IsCrypto(asset) {
			assetClass = "stock"
		}
		output, err = agents.News(ctx, store, asset, name, assetClass)
		if err != nil {
			return err
		}
		if err := saveOutput(ctx, store, runID, "news", asset, output); err != nil {
			return err
		}
	}
	output, err := agents.RiskContext(ctx, riskStore)
	if err != nil {
		return err
	}
	if err := saveOutput(ctx, store, runID, "risk_context", "", output); err != nil {
		return err
	}

	output, err = agents.Macro(ctx, store)
	if err != nil {
		return err
	}
	return saveOutput(ctx, store, runID, "macro", "", output)
}

// SubmitNarrativesWithDSN stores one validated narrative stage. Retrying the
// same stage is safe and replaces only that stage's narrative.
func SubmitNarrativesWithDSN(ctx context.Context, dsn, runID, stage string, submissions []NarrativeSubmission) error {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	if pending, err := store.PendingRun(ctx, runID); err != nil {
		return fmt.Errorf("read analysis run: %w", err)
	} else if !pending {
		return fmt.Errorf("analysis run %s is not pending", runID)
	}
	results, err := store.ResultsForRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("read analysis context: %w", err)
	}
	if err := validateNarratives(stage, submissions, results); err != nil {
		return err
	}
	for _, submission := range submissions {
		indicators := indicatorsFor(results, stage, submission.Asset)
		if err := store.SaveResult(ctx, storage.Result{ID: uuid.NewString(), RunID: runID, AgentType: stage, Asset: submission.Asset, Indicators: indicators, Narrative: strings.TrimSpace(submission.Narrative), CreatedAt: time.Now().UTC()}); err != nil {
			return fmt.Errorf("save %s narrative: %w", stage, err)
		}
	}
	return nil
}

// CompleteWithCommitteeWithDSN validates structured committee output,
// persists it, computes deterministic rankings, and completes the run.
func CompleteWithCommitteeWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, runID string, assessments []CommitteeAssessment) error {
	if riskStore == nil {
		return fmt.Errorf("risk store is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	if pending, err := store.PendingRun(ctx, runID); err != nil {
		return fmt.Errorf("read analysis run: %w", err)
	} else if !pending {
		return fmt.Errorf("analysis run %s is not pending", runID)
	}
	results, err := store.ResultsForRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("read analysis context: %w", err)
	}
	if !hasNarrative(results, "macro", "") {
		return fmt.Errorf("macro narrative must be submitted before committee")
	}
	assets := assetsFromResults(results)
	sources := narratives(results)
	expected := agents.EligibleAssets(assets, sources)
	raw, _ := json.Marshal(struct {
		Assessments []CommitteeAssessment `json:"assessments"`
	}{assessments})
	validated, err := agents.ParseCommitteeResponse(string(raw), expected)
	if err != nil {
		return err
	}
	if err := saveCommittee(ctx, store, riskStore, runID, validated); err != nil {
		return err
	}
	if err := store.FinishRun(ctx, runID, nil); err != nil {
		return fmt.Errorf("complete analysis run: %w", err)
	}
	return nil
}

func prepared(ctx context.Context, store *storage.Store, runID string) (PreparedRun, error) {
	results, err := store.ResultsForRun(ctx, runID)
	if err != nil {
		return PreparedRun{}, fmt.Errorf("read prepared context: %w", err)
	}
	context := make([]ContextItem, len(results))
	for i, result := range results {
		context[i] = ContextItem{AgentType: result.AgentType, Asset: result.Asset, Indicators: result.Indicators}
	}
	return PreparedRun{RunID: runID, Context: context}, nil
}

func saveOutput(ctx context.Context, store *storage.Store, runID, agentType, asset string, output agents.Output) error {
	return store.SaveResult(ctx, storage.Result{ID: uuid.NewString(), RunID: runID, AgentType: agentType, Asset: asset, Indicators: output.Indicators, CreatedAt: time.Now().UTC()})
}

func validateRequest(request PrepareRequest) ([]string, error) {
	if strings.TrimSpace(request.Timeframe) == "" {
		return nil, fmt.Errorf("timeframe is required")
	}
	seen := make(map[string]bool, len(request.Assets))
	assets := make([]string, 0, len(request.Assets))
	for _, asset := range request.Assets {
		asset = strings.TrimSpace(asset)
		if asset == "" || seen[asset] {
			return nil, fmt.Errorf("assets must be non-empty and unique")
		}
		seen[asset] = true
		assets = append(assets, asset)
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("at least one asset is required")
	}
	return assets, nil
}

func validateNarratives(stage string, submissions []NarrativeSubmission, results []storage.AgentResult) error {
	if stage != "technical" && stage != "derivatives" && stage != "news" && stage != "risk_context" && stage != "macro" {
		return fmt.Errorf("invalid narrative stage %q", stage)
	}
	expected := make(map[string]bool)
	if stage == "macro" {
		if !allBaseNarratives(results) {
			return fmt.Errorf("technical, derivatives, news, and risk_context narratives must be submitted before macro")
		}
		expected[""] = true
	} else {
		for _, result := range results {
			if result.AgentType == stage {
				expected[result.Asset] = true
			}
		}
	}
	if len(submissions) != len(expected) {
		return fmt.Errorf("%s requires %d narratives, got %d", stage, len(expected), len(submissions))
	}
	seen := make(map[string]bool, len(submissions))
	for _, submission := range submissions {
		if !expected[submission.Asset] || seen[submission.Asset] || strings.TrimSpace(submission.Narrative) == "" {
			return fmt.Errorf("invalid %s narrative submission for asset %q", stage, submission.Asset)
		}
		seen[submission.Asset] = true
	}
	return nil
}

func indicatorsFor(results []storage.AgentResult, agentType, asset string) json.RawMessage {
	for _, result := range results {
		if result.AgentType == agentType && result.Asset == asset {
			return result.Indicators
		}
	}
	return nil
}

func allBaseNarratives(results []storage.AgentResult) bool {
	for _, result := range results {
		if (result.AgentType == "technical" || result.AgentType == "derivatives" || result.AgentType == "news" || result.AgentType == "risk_context") && strings.TrimSpace(result.Narrative) == "" {
			return false
		}
	}
	return true
}

func hasNarrative(results []storage.AgentResult, agentType, asset string) bool {
	for _, result := range results {
		if result.AgentType == agentType && result.Asset == asset {
			return strings.TrimSpace(result.Narrative) != ""
		}
	}
	return false
}

func assetsFromResults(results []storage.AgentResult) []string {
	assets := make([]string, 0)
	for _, result := range results {
		if result.AgentType == "technical" {
			assets = append(assets, result.Asset)
		}
	}
	return assets
}

func narratives(results []storage.AgentResult) []agents.CycleNarrative {
	sources := make([]agents.CycleNarrative, len(results))
	for i, result := range results {
		sources[i] = agents.CycleNarrative{AgentType: result.AgentType, Asset: result.Asset, Narrative: result.Narrative}
	}
	return sources
}

func saveCommittee(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, runID string, assessments []agents.CommitteeAssessment) error {
	inputs := make([]ranking.Input, 0, len(assessments))
	persisted := make(map[string]agents.CommitteeAssessment, len(assessments))
	for _, assessment := range assessments {
		quality, found, err := store.QualityForRanking(ctx, risk.ExchangeFor(assessment.Asset), assessment.Asset)
		if err != nil {
			return fmt.Errorf("committee/%s data quality: %w", assessment.Asset, err)
		}
		indicators := agents.CommitteeIndicators{Thesis: assessment.Thesis, Confidence: assessment.Confidence, OpportunityScore: assessment.OpportunityScore, Evidence: assessment.Evidence}
		if found {
			indicators.Quality = quality
			inputs = append(inputs, ranking.Input{Asset: assessment.Asset, OpportunityScore: assessment.OpportunityScore, DataAgeMinutes: quality.DataAgeMinutes, Liquidity: quality.Liquidity, Volatility: quality.Volatility})
			persisted[assessment.Asset] = assessment
		}
		if err := saveOutput(ctx, store, runID, "committee", assessment.Asset, agents.Output{Indicators: indicators, Narrative: assessment.Narrative}); err != nil {
			return fmt.Errorf("save committee/%s: %w", assessment.Asset, err)
		}
	}
	if len(inputs) == 0 {
		return nil
	}
	limits, err := riskStore.GetLimits(ctx)
	if err != nil {
		return fmt.Errorf("read risk limits: %w", err)
	}
	computed, err := ranking.Compute(inputs, ranking.Limits{MaxDataAgeMinutes: limits.MaxDataAgeMinutes, MinLiquidity: limits.MinLiquidity, MaxVolatility: limits.MaxVolatility})
	if err != nil {
		return fmt.Errorf("compute rankings: %w", err)
	}
	rankings := make([]storage.Ranking, 0, len(computed))
	for _, result := range computed {
		assessment := persisted[result.Asset]
		evidence, err := json.Marshal(assessment.Evidence)
		if err != nil {
			return fmt.Errorf("marshal committee evidence: %w", err)
		}
		rankings = append(rankings, storage.Ranking{RunID: runID, Asset: result.Asset, Rank: result.Rank, CompositeScore: result.CompositeScore, OpportunityScoreRaw: assessment.OpportunityScore, Thesis: assessment.Thesis, Confidence: assessment.Confidence, Evidence: evidence, ComputedAt: time.Now().UTC()})
	}
	if err := store.SaveRankings(ctx, rankings); err != nil {
		return fmt.Errorf("save rankings: %w", err)
	}
	return nil
}
