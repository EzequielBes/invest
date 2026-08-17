// risk-engine/risk/evaluate.go
package risk

import (
	"context"
	"fmt"
	"time"

	"risk-engine/storage"
)

// EvalOptions carries the optional, backward-compatible parameters a
// simulated (backtest) caller needs and a live caller doesn't: AsOf caps
// which candles quality checks may see (nil = no cutoff, i.e. now);
// RunID isolates risk_state/risk_decisions to one backtest run (nil = the
// live row, exactly today's behavior).
type EvalOptions struct {
	AsOf  *time.Time
	RunID *string
}

// Evaluate is the risk engine's entry point: given the caller-supplied
// portfolio state and a proposed operation, it returns an allow/reject
// decision and records it, transitioning operational state to paused if a
// loss limit is breached.
func Evaluate(ctx context.Context, store *storage.Store, portfolio PortfolioState, proposed ProposedOperation, opts EvalOptions) (Decision, error) {
	state, err := store.GetState(ctx, opts.RunID)
	if err != nil {
		return Decision{}, fmt.Errorf("risk: get state: %w", err)
	}
	if state.Status != storage.StatusNormal {
		d := Decision{Allowed: false, Reasons: []string{fmt.Sprintf("system is %s: %s", state.Status, state.Reason)}}
		if err := store.RecordDecision(ctx, opts.RunID, toRecord(proposed, d)); err != nil {
			return Decision{}, fmt.Errorf("risk: record decision: %w", err)
		}
		return d, nil
	}

	limits, err := store.GetLimits(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("risk: get limits: %w", err)
	}

	var results []RuleResult
	results = append(results,
		checkAssetConcentration(portfolio, proposed, limits.MaxPctPerAsset),
		checkCryptoTotalConcentration(portfolio, proposed, limits.MaxPctCryptoTotal),
		checkMaxTradeValue(proposed, limits.MaxValuePerTrade),
	)

	lossResults := []RuleResult{
		checkDailyLoss(portfolio, limits.MaxDailyLoss),
		checkWeeklyLoss(portfolio, limits.MaxWeeklyLoss),
		checkDrawdown(portfolio, limits.MaxDrawdown),
		checkConsecutiveLosses(portfolio, limits.MaxConsecutiveLosses),
	}
	results = append(results, lossResults...)

	lossViolated := ""
	for _, r := range lossResults {
		if !r.Passed {
			lossViolated = r.Rule
			break
		}
	}

	results = append(results,
		checkDataFreshness(ctx, store, proposed.Asset, limits.MaxDataAgeMinutes, opts.AsOf),
		checkVolatility(ctx, store, proposed.Asset, limits.MaxVolatility, opts.AsOf),
		checkLiquidity(ctx, store, proposed.Asset, limits.MinLiquidity, opts.AsOf),
	)

	d := Decision{Allowed: true, Reasons: []string{}, RulesChecked: results}
	for _, r := range results {
		if !r.Passed {
			d.Allowed = false
			d.Reasons = append(d.Reasons, fmt.Sprintf("%s: %s", r.Rule, r.Detail))
		}
	}

	if lossViolated != "" {
		tx, err := store.BeginTx(ctx)
		if err != nil {
			return Decision{}, fmt.Errorf("risk: begin tx: %w", err)
		}
		defer tx.Rollback(context.WithoutCancel(ctx))

		reason := fmt.Sprintf("auto-paused: %s limit breached", lossViolated)
		if _, err := storage.SetStateIfNormal(ctx, tx, opts.RunID, storage.StatusPaused, reason); err != nil {
			return Decision{}, fmt.Errorf("risk: set state: %w", err)
		}
		// If SetStateIfNormal didn't apply, state already changed (e.g. to
		// kill_switch) since our initial read — don't downgrade it. The
		// operation is still rejected for the loss breach either way; just
		// record the decision without touching state further.
		if err := storage.RecordDecision(ctx, tx, opts.RunID, toRecord(proposed, d)); err != nil {
			return Decision{}, fmt.Errorf("risk: record decision: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Decision{}, fmt.Errorf("risk: commit: %w", err)
		}
		return d, nil
	}

	if err := store.RecordDecision(ctx, opts.RunID, toRecord(proposed, d)); err != nil {
		return Decision{}, fmt.Errorf("risk: record decision: %w", err)
	}
	return d, nil
}

func toRecord(proposed ProposedOperation, d Decision) storage.DecisionRecord {
	rules := make([]storage.RuleResultRecord, len(d.RulesChecked))
	for i, r := range d.RulesChecked {
		rules[i] = storage.RuleResultRecord{Rule: r.Rule, Passed: r.Passed, Measured: r.Measured, Limit: r.Limit, Detail: r.Detail}
	}
	reasons := d.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return storage.DecisionRecord{
		Asset: proposed.Asset, Side: string(proposed.Side), Quantity: proposed.Quantity, Value: proposed.Value,
		Allowed: d.Allowed, Reasons: reasons, RulesChecked: rules,
	}
}
