// analysis/runner/runner.go
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	riskstorage "risk-engine/storage"

	"analysis/internal/agents"
	"analysis/internal/llm"
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
	for _, agentType := range requestedAgents {
		switch agentType {
		case "technical":
			for _, asset := range assets {
				out, agentErr := agents.Technical(ctx, store, client, asset, timeframe)
				if err := recordOne(agentType, asset, out, agentErr); err != nil {
					return abortRun(ctx, store, runID, successCount, err)
				}
			}
		case "derivatives":
			for _, asset := range assets {
				out, agentErr := agents.Derivatives(ctx, store, client, asset)
				if err := recordOne(agentType, asset, out, agentErr); err != nil {
					return abortRun(ctx, store, runID, successCount, err)
				}
			}
		case "news":
			for _, asset := range assets {
				name := assetNames[asset]
				if name == "" {
					name = asset
				}
				out, agentErr := agents.News(ctx, store, client, asset, name)
				if err := recordOne(agentType, asset, out, agentErr); err != nil {
					return abortRun(ctx, store, runID, successCount, err)
				}
			}
		case "risk_context":
			out, agentErr := agents.RiskContext(ctx, riskStore, client)
			if err := recordOne(agentType, "", out, agentErr); err != nil {
				return abortRun(ctx, store, runID, successCount, err)
			}
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
