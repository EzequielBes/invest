// strategist/runner/runner.go
package runner

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/internal/strategist"
)

// Run decides on every requested asset and persists the outcome. It
// returns an error only for a whole-run failure (can't read analysis
// results, can't price a held position, zero portfolio value); a single
// asset failing to decide is logged to stderr and skipped, not returned
// as an error. Exported so both cmd/strategist and other modules (the MCP
// server) can call it directly.
func Run(
	ctx context.Context,
	store *storage.Store,
	riskStore *riskstorage.Store,
	client llm.Client,
	runID string,
	assets []string,
	timeframe string,
	cash float64,
	positions map[string]float64,
	dailyLoss, weeklyLoss, drawdown float64,
	consecutiveLosses int,
) error {
	results, err := store.ResultsForRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("read analysis results: %w", err)
	}
	byAsset := make(map[string][]storage.AgentResult)
	var riskContext storage.AgentResult
	for _, r := range results {
		if r.AgentType == "risk_context" {
			riskContext = r
			continue
		}
		byAsset[r.Asset] = append(byAsset[r.Asset], r)
	}

	portfolio, portfolioValue, err := buildPortfolio(ctx, store, positions, cash, timeframe, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
	if err != nil {
		return fmt.Errorf("build portfolio: %w", err)
	}

	for _, asset := range assets {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, asset, timeframe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: read price: %v\n", asset, err)
			continue
		}
		if !found {
			fmt.Fprintf(os.Stderr, "%s: no price data, skipping\n", asset)
			continue
		}

		outcome, err := strategist.Decide(ctx, riskStore, client, asset, byAsset[asset], riskContext, portfolio, portfolioValue, price)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", asset, err)
			continue
		}
		if outcome.RiskErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", outcome.RiskErr)
		}
		if err := save(ctx, store, runID, asset, outcome); err != nil {
			fmt.Fprintf(os.Stderr, "%s: save decision: %v\n", asset, err)
			continue
		}
		report(asset, outcome)
	}
	return nil
}

// buildPortfolio prices every held position (fatal for the whole run if
// any is missing — an inaccurate portfolio valuation makes every
// risk.Evaluate call downstream meaningless) and totals portfolio value
// (cash + all position values) for sizing. A total <= 0 is also fatal —
// see sub-project 5's final review for why a zero-value portfolio must
// not silently size zero-quantity "approved" trades.
func buildPortfolio(ctx context.Context, store *storage.Store, positions map[string]float64, cash float64, timeframe string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) (risk.PortfolioState, float64, error) {
	riskPositions := make(map[string]risk.Position, len(positions))
	total := cash
	for symbol, qty := range positions {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, symbol, timeframe)
		if err != nil {
			return risk.PortfolioState{}, 0, fmt.Errorf("price for held position %s: %w", symbol, err)
		}
		if !found {
			return risk.PortfolioState{}, 0, fmt.Errorf("no price data for held position %s on %s", symbol, risk.ReferenceExchange)
		}
		value := qty * price
		riskPositions[symbol] = risk.Position{Asset: symbol, Quantity: qty, Value: value}
		total += value
	}
	portfolio := risk.PortfolioState{
		Positions:         riskPositions,
		Cash:              cash,
		DailyLoss:         dailyLoss,
		WeeklyLoss:        weeklyLoss,
		Drawdown:          drawdown,
		ConsecutiveLosses: consecutiveLosses,
	}
	if total <= 0 {
		return risk.PortfolioState{}, 0, fmt.Errorf("portfolio value is zero — set -cash and/or -positions")
	}
	return portfolio, total, nil
}

func save(ctx context.Context, store *storage.Store, runID, asset string, outcome strategist.Outcome) error {
	d := storage.Decision{
		ID: uuid.NewString(), AnalysisRunID: runID, Asset: asset,
		Side: outcome.Decision.Side, Confidence: outcome.Decision.Confidence,
		SizingPct: outcome.Decision.SizingPct, Rationale: outcome.Decision.Rationale,
		ProposedQuantity: outcome.Quantity, ProposedValue: outcome.Value,
		CreatedAt: time.Now().UTC(),
	}
	if outcome.Risk != nil {
		allowed := outcome.Risk.Allowed
		d.RiskAllowed = &allowed
		d.RiskReasons = outcome.Risk.Reasons
	}
	return store.SaveDecision(ctx, d)
}

func report(asset string, outcome strategist.Outcome) {
	if outcome.Decision.Side == "hold" {
		fmt.Fprintf(os.Stderr, "%s: hold — %s\n", asset, outcome.Decision.Rationale)
		return
	}
	status := "risk-engine unavailable"
	if outcome.Risk != nil {
		if outcome.Risk.Allowed {
			status = "approved"
		} else {
			status = fmt.Sprintf("rejected (%s)", strings.Join(outcome.Risk.Reasons, "; "))
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %s %.6f (%s) — %s\n", asset, outcome.Decision.Side, outcome.Quantity, status, outcome.Decision.Rationale)
}

// RunWithDSN connects its own storage using dsn, calls Run, then reads
// back the decisions it persisted (Run itself only returns error) — same
// cross-module-visibility reason as analysis/runner.RunWithDSN (sub-project
// 6, Task 6): callers outside this module can't import
// strategist/internal/storage or strategist/internal/llm directly, so
// this function is the module's public entry point for exactly that kind
// of caller — it returns []storage.Decision, which the caller (mcp) can
// range over and read fields from without ever importing
// strategist/internal/storage itself (Go's internal-package rule blocks
// the *import*, not the use of an already-typed value obtained through a
// legal call like this one).
func RunWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, analysisRunID string, assets []string, timeframe string, cash float64, positions map[string]float64, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) ([]storage.Decision, error) {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	client, err := llm.NewClient()
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	if err := Run(ctx, store, riskStore, client, analysisRunID, assets, timeframe, cash, positions, dailyLoss, weeklyLoss, drawdown, consecutiveLosses); err != nil {
		return nil, err
	}
	decisions, err := store.DecisionsForRun(ctx, analysisRunID)
	if err != nil {
		return nil, err
	}
	fresh := make([]storage.Decision, 0, len(decisions))
	for _, d := range decisions {
		if !d.CreatedAt.Before(startedAt) {
			fresh = append(fresh, d)
		}
	}
	return fresh, nil
}
