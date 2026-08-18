// strategist/cmd/strategist/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"strategist/internal/llm"
	"strategist/internal/storage"
	"strategist/internal/strategist"
)

func main() {
	runID := flag.String("run-id", "", "analysis_run_id to decide from (required)")
	assetsStr := flag.String("assets", "", "comma-separated asset symbols to decide on (required)")
	timeframe := flag.String("timeframe", "1h", "timeframe used to look up the current price")
	cash := flag.Float64("cash", 0, "cash available, in USD (required)")
	positionsStr := flag.String("positions", "", "comma-separated SYMBOL:quantity current positions")
	dailyLoss := flag.Float64("daily-loss", 0, "portfolio daily loss so far, as a fraction (e.g. 0.02 = 2%)")
	weeklyLoss := flag.Float64("weekly-loss", 0, "portfolio weekly loss so far, as a fraction")
	drawdown := flag.Float64("drawdown", 0, "portfolio drawdown from peak, as a fraction")
	consecutiveLosses := flag.Int("consecutive-losses", 0, "number of consecutive losing trades")
	flag.Parse()

	if err := run(context.Background(), *runID, *assetsStr, *timeframe, *cash, *positionsStr, *dailyLoss, *weeklyLoss, *drawdown, *consecutiveLosses); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, runID, assetsStr, timeframe string, cash float64, positionsStr string, dailyLoss, weeklyLoss, drawdown float64, consecutiveLosses int) error {
	if runID == "" {
		return fmt.Errorf("-run-id is required")
	}
	assets := splitNonEmpty(assetsStr)
	if len(assets) == 0 {
		return fmt.Errorf("-assets is required")
	}
	positions, err := parsePositions(positionsStr)
	if err != nil {
		return err
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	riskStore, err := riskstorage.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect risk-engine storage: %w", err)
	}
	defer riskStore.Close()

	client := llm.NewAnthropicClient()
	return Run(ctx, store, riskStore, client, runID, assets, timeframe, cash, positions, dailyLoss, weeklyLoss, drawdown, consecutiveLosses)
}

// Run decides on every requested asset and persists the outcome. It
// returns an error only for a whole-run failure (can't read analysis
// results, can't price a held position); a single asset failing to
// decide is logged to stderr and skipped, not returned as an error.
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
// (cash + all position values) for sizing.
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
		fmt.Printf("%s: hold — %s\n", asset, outcome.Decision.Rationale)
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
	fmt.Printf("%s: %s %.6f (%s) — %s\n", asset, outcome.Decision.Side, outcome.Quantity, status, outcome.Decision.Rationale)
}

func splitNonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parsePositions(value string) (map[string]float64, error) {
	positions := make(map[string]float64)
	for _, entry := range splitNonEmpty(value) {
		symbol, qtyStr, found := strings.Cut(entry, ":")
		symbol = strings.TrimSpace(symbol)
		qtyStr = strings.TrimSpace(qtyStr)
		if !found || symbol == "" || qtyStr == "" {
			return nil, fmt.Errorf("invalid -positions entry %q (want SYMBOL:quantity)", entry)
		}
		qty, err := strconv.ParseFloat(qtyStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid -positions entry %q: %w", entry, err)
		}
		positions[symbol] = qty
	}
	return positions, nil
}
