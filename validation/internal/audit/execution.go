package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"validation/internal/metrics"
	"validation/internal/storage"
	validation "validation/internal/validation"
)

type ExecutionInput struct {
	HypothesisID  string
	ClientOrderID string
	Config        json.RawMessage
	GitCommit     string
	Splits        []validation.Split
}

// Execution records an observational audit of one real order, linked only by
// its documented client_order_id. It intentionally does not infer P&L,
// position closure, or funding from an order fill.
func Execution(ctx context.Context, store *storage.Store, input ExecutionInput) (storage.Run, error) {
	run, err := createRunWithSplits(ctx, store, storage.Run{
		HypothesisID: input.HypothesisID,
		Config:       input.Config,
		GitCommit:    input.GitCommit,
	}, input.Splits)
	if err != nil {
		return storage.Run{}, err
	}

	execution, err := store.ExecutionByClientOrderID(ctx, input.ClientOrderID)
	if err != nil {
		var duplicate *storage.DuplicateClientOrderIDError
		if errors.As(err, &duplicate) {
			err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("duplicate_client_order_id", "multiple executions share client_order_id and cannot be audited unambiguously", input.ClientOrderID, map[string]any{"matching_execution_count": duplicate.Count}))
			run.Status = "inconclusive"
			return run, err
		}
		if err == pgx.ErrNoRows {
			err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("missing_execution", "no execution exists for client_order_id", input.ClientOrderID, nil))
			run.Status = "inconclusive"
			return run, err
		}
		return run, err
	}

	status := strings.ToLower(strings.TrimSpace(execution.Status))
	switch status {
	case "partial":
		err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("partial_fill", "partial execution fill cannot be treated as a complete fill", input.ClientOrderID, execution))
		run.Status = "inconclusive"
		return run, err
	case "cancelled":
		err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("cancelled_execution", "cancelled execution has no complete fill to audit", input.ClientOrderID, execution))
		run.Status = "inconclusive"
		return run, err
	case "filled":
		if !positiveFinite(execution.FilledQuantity) {
			err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("missing_fill_quantity", "filled execution has no positive filled quantity", input.ClientOrderID, execution))
			run.Status = "inconclusive"
			return run, err
		}
		if !positiveFinite(execution.Price) || !positiveFinite(execution.FilledPrice) {
			err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("missing_fill_price", "filled execution has invalid requested or filled price", input.ClientOrderID, execution))
			run.Status = "inconclusive"
			return run, err
		}
		slippage, err := metrics.SlippageBps(execution.Side, execution.Price, execution.FilledPrice)
		if err != nil {
			finishErr := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("invalid_execution_side", "filled execution has an unknown side", input.ClientOrderID, execution))
			run.Status = "inconclusive"
			return run, finishErr
		}
		if err := store.SaveMetrics(ctx, run.ID, []storage.Metric{{
			Name: "realized_slippage_bps", Value: slippage, Segment: "execution", Unit: "basis_points",
			Evidence: map[string]any{"client_order_id": input.ClientOrderID, "execution_id": execution.ID, "asset": execution.Asset, "side": execution.Side, "filled_quantity": execution.FilledQuantity},
		}}); err != nil {
			return run, err
		}
		if err := store.FinishRun(ctx, run.ID, "completed", ""); err != nil {
			return run, err
		}
		run.Status = "completed"
		return run, nil
	default:
		err := finishWithFinding(ctx, store, run.ID, "inconclusive", executionFinding("unknown_execution_status", fmt.Sprintf("execution status %q cannot be audited as a fill", execution.Status), input.ClientOrderID, execution))
		run.Status = "inconclusive"
		return run, err
	}
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func executionFinding(rule, message, clientOrderID string, execution any) validation.Finding {
	evidence := map[string]any{"client_order_id": clientOrderID}
	if execution != nil {
		evidence["execution"] = execution
	}
	return validation.Finding{Severity: "warning", Rule: rule, Message: message, Evidence: evidence}
}
