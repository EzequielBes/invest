// Package runner exposes strategist's provider-neutral intent workflow.
package runner

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"risk-engine/risk"
	riskstorage "risk-engine/storage"

	"execution/executor"
	"strategist/internal/storage"
)

// priceTimeframeFor returns the candle timeframe to read asset's latest
// price from — matches the granularity each asset class is actually
// collected at (crypto: 1m, stocks: 5m via Alpaca's free-tier delay).
func priceTimeframeFor(asset string) string {
	if risk.IsCrypto(asset) {
		return "1m"
	}
	return "5m"
}

type Ranking struct {
	Asset          string
	Rank           int
	CompositeScore float64
	Thesis         string
	Confidence     float64
}

// StrategyContext is the completed analysis material a caller uses to form
// stable intents. Rankings are ordered best-first and shared by all targets.
type StrategyContext struct {
	AnalysisRunID string
	Rankings      []Ranking
}

// Intent is caller-supplied structured strategy output. ID must be stable
// across retries of the same intended action.
type Intent struct {
	ID         string
	Asset      string
	Side       string
	Confidence float64
	SizingPct  float64
	Rationale  string
}

// Target independently sizes and risk-checks shared intents against its own
// portfolio. ID distinguishes paper and testnet application idempotency.
type Target struct {
	ID                              string
	Executor                        executor.Client
	DailyLoss, WeeklyLoss, Drawdown float64
	ConsecutiveLosses               int
}

type Application struct {
	IntentID         string
	TargetID         string
	Asset            string
	Side             string
	Quantity         float64
	Value            float64
	RiskAllowed      *bool
	RiskReasons      []string
	ExecutionStatus  string
	ExecutionOrderID string
}

// PrepareWithDSN returns rankings only for a successfully completed analysis
// run. It deliberately performs no model call or portfolio-specific sizing.
func PrepareWithDSN(ctx context.Context, dsn, analysisRunID string) (StrategyContext, error) {
	if strings.TrimSpace(analysisRunID) == "" {
		return StrategyContext{}, fmt.Errorf("analysis run ID is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return StrategyContext{}, fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()
	completed, err := store.CompletedRun(ctx, analysisRunID)
	if err != nil {
		return StrategyContext{}, fmt.Errorf("read analysis run: %w", err)
	}
	if !completed {
		return StrategyContext{}, fmt.Errorf("analysis run %s is not completed", analysisRunID)
	}
	rankings, err := store.RankingsForRun(ctx, analysisRunID)
	if err != nil {
		return StrategyContext{}, fmt.Errorf("read analysis rankings: %w", err)
	}
	context := StrategyContext{AnalysisRunID: analysisRunID, Rankings: make([]Ranking, len(rankings))}
	for i, ranking := range rankings {
		context.Rankings[i] = Ranking{Asset: ranking.Asset, Rank: ranking.Rank, CompositeScore: ranking.CompositeScore, Thesis: ranking.Thesis, Confidence: ranking.Confidence}
	}
	return context, nil
}

// ApplyIntentsWithDSN validates caller-supplied intents, then applies every
// intent to every target. Each target fetches and values its own portfolio,
// so paper and testnet share strategy but not sizing or risk decisions.
func ApplyIntentsWithDSN(ctx context.Context, dsn string, riskStore *riskstorage.Store, strategy StrategyContext, intents []Intent, targets []Target) ([]Application, error) {
	prepared, err := PrepareWithDSN(ctx, dsn, strategy.AnalysisRunID)
	if err != nil {
		return nil, err
	}
	strategy = prepared
	if err := validate(strategy, intents, targets); err != nil {
		return nil, err
	}
	if riskStore == nil {
		return nil, fmt.Errorf("risk store is required")
	}
	store, err := storage.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect strategist storage: %w", err)
	}
	defer store.Close()

	for _, target := range targets {
		cash, positions, err := target.Executor.FetchPortfolio(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch %s portfolio: %w", target.ID, err)
		}
		portfolio, value, err := buildPortfolio(ctx, store, positions, cash, target)
		if err != nil {
			return nil, fmt.Errorf("build %s portfolio: %w", target.ID, err)
		}
		for _, intent := range intents {
			portfolio, value, err = apply(ctx, store, riskStore, strategy.AnalysisRunID, target, intent, portfolio, value)
			if err != nil {
				return nil, err
			}
		}
	}
	return applications(ctx, store, strategy.AnalysisRunID, intents)
}

func validate(strategy StrategyContext, intents []Intent, targets []Target) error {
	if strings.TrimSpace(strategy.AnalysisRunID) == "" || len(strategy.Rankings) == 0 {
		return fmt.Errorf("completed ranked strategy context is required")
	}
	ranked := make(map[string]bool, len(strategy.Rankings))
	for _, ranking := range strategy.Rankings {
		ranked[ranking.Asset] = true
	}
	seen := make(map[string]bool, len(intents))
	for _, intent := range intents {
		if strings.TrimSpace(intent.ID) == "" || seen[intent.ID] || !ranked[intent.Asset] ||
			(intent.Side != "buy" && intent.Side != "sell" && intent.Side != "hold") ||
			intent.Confidence < 0 || intent.Confidence > 1 || intent.SizingPct < 0 || intent.SizingPct > 1 {
			return fmt.Errorf("invalid intent %q", intent.ID)
		}
		seen[intent.ID] = true
	}
	if len(intents) == 0 {
		return fmt.Errorf("at least one intent is required")
	}
	seen = make(map[string]bool, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.ID) == "" || seen[target.ID] || target.Executor == nil {
			return fmt.Errorf("invalid target %q", target.ID)
		}
		seen[target.ID] = true
	}
	if len(targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	return nil
}

func buildPortfolio(ctx context.Context, store *storage.Store, positions map[string]float64, cash float64, target Target) (risk.PortfolioState, float64, error) {
	riskPositions := make(map[string]risk.Position, len(positions))
	total := cash
	for asset, quantity := range positions {
		price, found, err := store.LatestPrice(ctx, risk.ExchangeFor(asset), asset, priceTimeframeFor(asset))
		if err != nil || !found {
			if err != nil {
				return risk.PortfolioState{}, 0, err
			}
			return risk.PortfolioState{}, 0, fmt.Errorf("no price data for held position %s", asset)
		}
		value := quantity * price
		riskPositions[asset] = risk.Position{Asset: asset, Quantity: quantity, Value: value}
		total += value
	}
	if total <= 0 {
		return risk.PortfolioState{}, 0, fmt.Errorf("portfolio value is zero")
	}
	return risk.PortfolioState{Positions: riskPositions, Cash: cash, DailyLoss: target.DailyLoss, WeeklyLoss: target.WeeklyLoss, Drawdown: target.Drawdown, ConsecutiveLosses: target.ConsecutiveLosses}, total, nil
}

func apply(ctx context.Context, store *storage.Store, riskStore *riskstorage.Store, runID string, target Target, intent Intent, portfolio risk.PortfolioState, portfolioValue float64) (risk.PortfolioState, float64, error) {
	price, found, err := store.LatestPrice(ctx, risk.ExchangeFor(intent.Asset), intent.Asset, priceTimeframeFor(intent.Asset))
	if err != nil || !found {
		if err != nil {
			return portfolio, portfolioValue, fmt.Errorf("read %s price: %w", intent.Asset, err)
		}
		return portfolio, portfolioValue, fmt.Errorf("no price data for %s", intent.Asset)
	}
	quantity := 0.0
	if intent.Side != "hold" {
		quantity = intent.SizingPct * portfolioValue / price
		if intent.Side == "sell" {
			quantity = math.Min(quantity, portfolio.Positions[intent.Asset].Quantity)
		}
	}
	application := storage.IntentApplication{IntentID: intent.ID, TargetID: target.ID, AnalysisRunID: runID, Asset: intent.Asset, Side: intent.Side, Confidence: intent.Confidence, SizingPct: intent.SizingPct, Rationale: intent.Rationale, ProposedQuantity: quantity, ProposedValue: quantity * price, ExecutionStatus: "applying", CreatedAt: time.Now().UTC()}
	created, err := store.CreateIntentApplication(ctx, application)
	if err != nil || !created {
		return portfolio, portfolioValue, err
	}
	if intent.Side == "hold" || quantity <= 0 {
		return portfolio, portfolioValue, store.CompleteIntentApplication(ctx, runID, intent.ID, target.ID, "not_applicable", nil, nil, nil)
	}
	decision, err := risk.Evaluate(ctx, riskStore, portfolio, risk.ProposedOperation{Asset: intent.Asset, Side: risk.Side(intent.Side), Quantity: quantity, Value: application.ProposedValue}, risk.EvalOptions{})
	if err != nil {
		return portfolio, portfolioValue, store.SetIntentApplicationRisk(ctx, runID, intent.ID, target.ID, "risk_error", nil, nil)
	}
	if !decision.Allowed {
		return portfolio, portfolioValue, store.SetIntentApplicationRisk(ctx, runID, intent.ID, target.ID, "rejected", &decision.Allowed, decision.Reasons)
	}
	if err := store.SetIntentApplicationRisk(ctx, runID, intent.ID, target.ID, "executing", &decision.Allowed, decision.Reasons); err != nil {
		return portfolio, portfolioValue, err
	}
	outcome, err := target.Executor.Execute(ctx, intent.Asset, risk.Side(intent.Side), quantity, price, intent.ID)
	if err != nil {
		return portfolio, portfolioValue, store.CompleteIntentApplication(ctx, runID, intent.ID, target.ID, "execution_error", nil, nil, nil)
	}
	if err := store.CompleteIntentApplication(ctx, runID, intent.ID, target.ID, outcome.Status, &outcome.OrderID, &outcome.FilledQuantity, &outcome.FilledPrice); err != nil {
		return portfolio, portfolioValue, err
	}
	return projectPortfolio(portfolio, intent.Asset, risk.Side(intent.Side), outcome.FilledQuantity, outcome.FilledPrice), portfolioValue, nil
}

func projectPortfolio(portfolio risk.PortfolioState, asset string, side risk.Side, quantity, price float64) risk.PortfolioState {
	if quantity <= 0 || price <= 0 {
		return portfolio
	}
	positions := make(map[string]risk.Position, len(portfolio.Positions)+1)
	for name, position := range portfolio.Positions {
		positions[name] = position
	}
	value := quantity * price
	position := positions[asset]
	position.Asset = asset
	switch side {
	case risk.SideBuy:
		position.Quantity += quantity
		position.Value += value
		portfolio.Cash -= value
	case risk.SideSell:
		quantity = math.Min(quantity, position.Quantity)
		value = quantity * price
		position.Quantity -= quantity
		position.Value = math.Max(0, position.Value-value)
		portfolio.Cash += value
	}
	if position.Quantity <= 0 {
		delete(positions, asset)
	} else {
		positions[asset] = position
	}
	portfolio.Positions = positions
	return portfolio
}

func applications(ctx context.Context, store *storage.Store, analysisRunID string, intents []Intent) ([]Application, error) {
	ids := make([]string, len(intents))
	for i, intent := range intents {
		ids[i] = intent.ID
	}
	recorded, err := store.IntentApplications(ctx, analysisRunID, ids)
	if err != nil {
		return nil, err
	}
	result := make([]Application, len(recorded))
	for i, a := range recorded {
		result[i] = Application{IntentID: a.IntentID, TargetID: a.TargetID, Asset: a.Asset, Side: a.Side, Quantity: a.ProposedQuantity, Value: a.ProposedValue, RiskAllowed: a.RiskAllowed, RiskReasons: a.RiskReasons, ExecutionStatus: a.ExecutionStatus}
		if a.ExecutionOrderID != nil {
			result[i].ExecutionOrderID = *a.ExecutionOrderID
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].IntentID+result[i].TargetID < result[j].IntentID+result[j].TargetID
	})
	return result, nil
}
