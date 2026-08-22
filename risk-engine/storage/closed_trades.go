// risk-engine/storage/closed_trades.go
package storage

import "context"

// TradeOutcome is one closed trade's realized return as a fraction of its
// cost basis (e.g. 0.10 = +10%).
type TradeOutcome struct {
	PnLPct float64
}

// ClosedTradeOutcomes reads execution's paper_fills table directly — a
// pure data read across module boundaries (see AGENTS.md's cross-module
// rules), no Go dependency on the execution module. Only sell fills carry
// cost_basis/realized_pnl (a buy has nothing realized yet), so buys are
// naturally excluded by the NOT NULL filter below.
func (s *Store) ClosedTradeOutcomes(ctx context.Context, asset string) ([]TradeOutcome, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT realized_pnl / (cost_basis * quantity)
		FROM paper_fills
		WHERE asset = $1 AND side = 'sell' AND cost_basis IS NOT NULL
		  AND realized_pnl IS NOT NULL AND cost_basis > 0 AND quantity > 0
		ORDER BY created_at
	`, asset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []TradeOutcome
	for rows.Next() {
		var t TradeOutcome
		if err := rows.Scan(&t.PnLPct); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, t)
	}
	return outcomes, rows.Err()
}
