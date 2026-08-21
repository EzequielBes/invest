// analysis/runner/runner.go
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"analysis/internal/agents"
	"analysis/internal/llm"
	"analysis/internal/ranking"
	"analysis/internal/storage"
)

// Run executes one analysis run against the given assets/timeframe/agent
// selection, using client for narration. assetNames maps a symbol to its
// full name for the news agent (e.g. "BTC" -> "Bitcoin"); nil or a missing
// entry falls back to the symbol itself. Exported so both cmd/analysis and
// other modules (the MCP server) can call it directly.
func Run(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, client llm.Client, assets []string, assetNames map[string]string, timeframe string, requestedAgents []string) (runID string, successCount int, err error) {
	runID = uuid.NewString()
	if err := store.CreateRun(ctx, storage.Run{ID: runID, StartedAt: time.Now().UTC(), Timeframe: timeframe}); err != nil {
		return runID, 0, fmt.Errorf("create run: %w", err)
	}
	recordOne := func(agentType, asset string, out agents.Output, agentErr error) error {
		succeeded, err := record(ctx, store, runID, agentType, asset, out, agentErr)
		if succeeded {
			successCount++
		}
		return err
	}
	requested := make(map[string]bool, len(requestedAgents))
	for _, agentType := range requestedAgents {
		requested[agentType] = true
	}
	// Fixed stages make macro/committee independent of caller ordering and
	// guarantee committee is the final LLM role in every cycle.
	if requested["technical"] {
		for _, asset := range assets {
			out, agentErr := agents.Technical(ctx, store, client, asset, timeframe)
			if err := recordOne("technical", asset, out, agentErr); err != nil {
				return abortRun(ctx, store, runID, successCount, err)
			}
		}
	}
	if requested["derivatives"] {
		for _, asset := range assets {
			out, agentErr := agents.Derivatives(ctx, store, client, asset)
			if err := recordOne("derivatives", asset, out, agentErr); err != nil {
				return abortRun(ctx, store, runID, successCount, err)
			}
		}
	}
	if requested["news"] {
		for _, asset := range assets {
			name := assetNames[asset]
			if name == "" {
				name = asset
			}
			out, agentErr := agents.News(ctx, store, client, asset, name)
			if err := recordOne("news", asset, out, agentErr); err != nil {
				return abortRun(ctx, store, runID, successCount, err)
			}
		}
	}
	if requested["risk_context"] {
		out, agentErr := agents.RiskContext(ctx, riskStore, client)
		if err := recordOne("risk_context", "", out, agentErr); err != nil {
			return abortRun(ctx, store, runID, successCount, err)
		}
	}
	if requested["macro"] {
		sources, sourceErr := cycleNarratives(ctx, store, runID)
		if sourceErr != nil {
			return abortRun(ctx, store, runID, successCount, sourceErr)
		}
		out, agentErr := agents.Macro(ctx, client, assets, sources)
		if err := recordOne("macro", "", out, agentErr); err != nil {
			return abortRun(ctx, store, runID, successCount, err)
		}
	}
	if requested["committee"] {
		if err := runCommittee(ctx, store, riskStore, client, runID, assets, recordOne, &successCount); err != nil {
			return abortRun(ctx, store, runID, successCount, err)
		}
	}
	if successCount == 0 {
		return abortRun(ctx, store, runID, successCount, fmt.Errorf("all agents failed"))
	}
	if finishErr := store.FinishRun(ctx, runID, nil); finishErr != nil {
		return abortRun(ctx, store, runID, successCount, fmt.Errorf("finish run: %w", finishErr))
	}
	return runID, successCount, nil
}

func cycleNarratives(ctx context.Context, store *storage.Store, runID string) ([]agents.CycleNarrative, error) {
	results, err := store.ResultsForRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("read cycle narratives: %w", err)
	}
	sources := make([]agents.CycleNarrative, len(results))
	for i, result := range results {
		sources[i] = agents.CycleNarrative{AgentType: result.AgentType, Asset: result.Asset, Narrative: result.Narrative}
	}
	return sources, nil
}

func runCommittee(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, client llm.Client, runID string, assets []string, recordOne func(string, string, agents.Output, error) error, successCount *int) error {
	sources, err := cycleNarratives(ctx, store, runID)
	if err != nil {
		return err
	}
	assessments, err := agents.Committee(ctx, client, assets, sources)
	if err != nil {
		// Committee failures are intentionally isolated like every existing
		// analysis agent: prior analysis stays usable without a ranking.
		fmt.Fprintf(os.Stderr, "committee: %v\n", err)
		return nil
	}
	inputs := make([]ranking.Input, 0, len(assessments))
	persisted := make(map[string]agents.CommitteeAssessment, len(assessments))
	for _, assessment := range assessments {
		indicators := agents.CommitteeIndicators{
			Thesis: assessment.Thesis, Confidence: assessment.Confidence, OpportunityScore: assessment.OpportunityScore,
			Evidence: assessment.Evidence,
		}
		quality, found, qualityErr := store.QualityForRanking(ctx, risk.ReferenceExchange, assessment.Asset)
		if qualityErr != nil {
			fmt.Fprintf(os.Stderr, "committee/%s: data quality failed: %v\n", assessment.Asset, qualityErr)
		}
		if qualityErr == nil && found {
			indicators.Quality = quality
		} else if qualityErr == nil {
			fmt.Fprintf(os.Stderr, "committee/%s: insufficient 1m data quality window, skipping ranking\n", assessment.Asset)
		}
		if err := recordOne("committee", assessment.Asset, agents.Output{Indicators: indicators, Narrative: assessment.Narrative}, nil); err != nil {
			return err
		}
		*successCount = *successCount + 1
		if qualityErr != nil || !found {
			continue
		}
		inputs = append(inputs, ranking.Input{Asset: assessment.Asset, OpportunityScore: assessment.OpportunityScore, DataAgeMinutes: quality.DataAgeMinutes, Liquidity: quality.Liquidity, Volatility: quality.Volatility})
		persisted[assessment.Asset] = assessment
	}
	if len(inputs) == 0 {
		return nil
	}
	limits, err := riskStore.GetLimits(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "committee: read risk limits failed: %v\n", err)
		return nil
	}
	computed, err := ranking.Compute(inputs, ranking.Limits{MaxDataAgeMinutes: limits.MaxDataAgeMinutes, MinLiquidity: limits.MinLiquidity, MaxVolatility: limits.MaxVolatility})
	if err != nil {
		fmt.Fprintf(os.Stderr, "committee: compute ranking failed: %v\n", err)
		return nil
	}
	rankings := make([]storage.Ranking, 0, len(computed))
	for _, result := range computed {
		assessment := persisted[result.Asset]
		evidence, marshalErr := json.Marshal(assessment.Evidence)
		if marshalErr != nil {
			return fmt.Errorf("marshal committee evidence: %w", marshalErr)
		}
		rankings = append(rankings, storage.Ranking{RunID: runID, Asset: result.Asset, Rank: result.Rank, CompositeScore: result.CompositeScore, OpportunityScoreRaw: assessment.OpportunityScore, Thesis: assessment.Thesis, Confidence: assessment.Confidence, Evidence: evidence, ComputedAt: time.Now().UTC()})
	}
	if err := store.SaveRankings(ctx, rankings); err != nil {
		return fmt.Errorf("save rankings: %w", err)
	}
	return nil
}

type resultSaver interface {
	SaveResult(context.Context, storage.Result) error
}

func record(ctx context.Context, store resultSaver, runID, agentType, asset string, out agents.Output, agentErr error) (bool, error) {
	if agentErr != nil {
		fmt.Fprintf(os.Stderr, "%s/%s: data collection failed: %v\n", agentType, asset, agentErr)
		return false, nil
	}
	if err := store.SaveResult(ctx, storage.Result{ID: uuid.NewString(), RunID: runID, AgentType: agentType, Asset: asset, Indicators: out.Indicators, Narrative: out.Narrative, CreatedAt: time.Now().UTC()}); err != nil {
		return false, fmt.Errorf("save %s/%s result: %w", agentType, asset, err)
	}
	if out.Err != nil {
		fmt.Fprintf(os.Stderr, "%s/%s: sem narrativa: %v\n", agentType, asset, out.Err)
		return false, nil
	}
	fmt.Fprintf(os.Stderr, "%s/%s: %s\n", agentType, asset, out.Narrative)
	return true, nil
}

func abortRun(ctx context.Context, store *storage.Store, runID string, successCount int, runErr error) (string, int, error) {
	wrappedRunErr := fmt.Errorf("analysis run %s: %w", runID, runErr)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if finishErr := store.FinishRun(cleanupCtx, runID, runErr); finishErr != nil {
		return runID, successCount, errors.Join(wrappedRunErr, fmt.Errorf("finish failed run: %w", finishErr))
	}
	return runID, successCount, wrappedRunErr
}

// RunWithDSN connects its own storage using dsn and calls Run — this
// indirection exists so callers outside this module (e.g. the MCP
// server) never need to import analysis/internal/storage or
// analysis/internal/llm directly, which Go's internal-package
// visibility rule forbids across module boundaries (a `replace`
// directive only affects version resolution, not import-path
// visibility). riskStore is still supplied by the caller since
// risk-engine/storage is a public package, legal to import from
// anywhere.
func RunWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, assets []string, assetNames map[string]string, timeframe string, requestedAgents []string) (runID string, successCount int, results []storage.AgentResult, err error) {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return "", 0, nil, fmt.Errorf("connect analysis storage: %w", err)
	}
	defer store.Close()
	client, err := llm.NewClient()
	if err != nil {
		return "", 0, nil, err
	}
	runID, successCount, err = Run(ctx, store, riskStore, client, assets, assetNames, timeframe, requestedAgents)
	if err != nil {
		return runID, successCount, nil, err
	}
	results, resErr := store.ResultsForRun(ctx, runID)
	if resErr != nil {
		return runID, successCount, nil, fmt.Errorf("read back results: %w", resErr)
	}
	return runID, successCount, results, nil
}
