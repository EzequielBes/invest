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

	"execution/executor"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/internal/strategist"
)

const priceTimeframe = "1m"

// Run decides on every requested asset, executes approved decisions for
// real via execClient, and persists the outcome. It returns an error
// only for a whole-run failure (can't read analysis results, can't fetch
// the real portfolio, can't price a held position, zero portfolio
// value); a single asset failing to decide is logged to stderr and
// skipped, not returned as an error. Exported so both cmd/strategist and
// other modules (the MCP server) can call it directly.
func Run(
	ctx context.Context,
	store *storage.Store,
	riskStore *riskstorage.Store,
	client llm.Client,
	execClient executor.Client,
	runID string,
	assets []string,
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

	cash, positions, err := execClient.FetchPortfolio(ctx)
	if err != nil {
		return fmt.Errorf("fetch portfolio: %w", err)
	}
	portfolio, portfolioValue, err := buildPortfolio(ctx, store, positions, cash, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
	if err != nil {
		return fmt.Errorf("build portfolio: %w", err)
	}

	for _, asset := range assets {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, asset, priceTimeframe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: read price: %v\n", asset, err)
			continue
		}
		if !found {
			fmt.Fprintf(os.Stderr, "%s: no price data, skipping\n", asset)
			continue
		}

		decisionID := uuid.NewString()
		outcome, err := strategist.Decide(ctx, riskStore, client, execClient, decisionID, asset, byAsset[asset], riskContext, portfolio, portfolioValue, price)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", asset, err)
			continue
		}
		if outcome.RiskErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", outcome.RiskErr)
		}
		if outcome.ExecutionErr != nil {
			fmt.Fprintf(os.Stderr, "%v\n", outcome.ExecutionErr)
		}
		if err := save(ctx, store, runID, decisionID, asset, outcome); err != nil {
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
func buildPortfolio(ctx context.Context, store *storage.Store, positions map[string]float64, cash float64, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) (risk.PortfolioState, float64, error) {
	riskPositions := make(map[string]risk.Position, len(positions))
	total := cash
	for symbol, qty := range positions {
		price, found, err := store.LatestPrice(ctx, risk.ReferenceExchange, symbol, priceTimeframe)
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
		return risk.PortfolioState{}, 0, fmt.Errorf("portfolio value is zero — check the exchange account has funds")
	}
	return portfolio, total, nil
}

func save(ctx context.Context, store *storage.Store, runID, decisionID, asset string, outcome strategist.Outcome) error {
	d := storage.Decision{
		ID: decisionID, AnalysisRunID: runID, Asset: asset,
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
	if outcome.Execution != nil {
		status := outcome.Execution.Status
		orderID := outcome.Execution.OrderID
		filledQty := outcome.Execution.FilledQuantity
		filledPrice := outcome.Execution.FilledPrice
		d.ExecutionStatus = &status
		d.ExecutionOrderID = &orderID
		d.ExecutionFilledQuantity = &filledQty
		d.ExecutionFilledPrice = &filledPrice
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
	execStatus := "not executed"
	if outcome.Execution != nil {
		execStatus = fmt.Sprintf("%s (%.6f @ %.2f)", outcome.Execution.Status, outcome.Execution.FilledQuantity, outcome.Execution.FilledPrice)
	}
	fmt.Fprintf(os.Stderr, "%s: %s %.6f (%s, %s) — %s\n", asset, outcome.Decision.Side, outcome.Quantity, status, execStatus, outcome.Decision.Rationale)
}

// RunWithDSN connects its own storage and execution client using dsn,
// calls Run, then reads back the decisions it persisted (Run itself only
// returns error) — same cross-module-visibility reason as
// analysis/runner.RunWithDSN (sub-project 6, Task 6): callers outside
// this module can't import strategist/internal/storage,
// strategist/internal/llm, or execution/internal/* directly, so this
// function is the module's public entry point for exactly that kind of
// caller (the MCP server).
func RunWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, analysisRunID string, assets []string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) ([]storage.Decision, error) {
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	client, err := llm.NewClient()
	if err != nil {
		return nil, err
	}
	execClient, err := executor.NewClient(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer execClient.Close()
	startedAt := time.Now().UTC()
	if err := Run(ctx, store, riskStore, client, execClient, analysisRunID, assets, dailyLoss, weeklyLoss, drawdown, consecutiveLosses); err != nil {
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
