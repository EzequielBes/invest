// execution/internal/alpacaclient/account.go
package alpacaclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// Account is the subset of Alpaca's account/positions this client needs:
// available cash and any open stock positions.
type Account struct {
	Cash      float64
	Positions []AccountPosition
}

// AccountPosition is one open position — Alpaca's symbols are already the
// canonical asset symbol (no suffix mangling needed, unlike Binance's
// USDT-margined futures naming).
type AccountPosition struct {
	Asset    string
	Quantity float64
}

type accountResponse struct {
	Cash string `json:"cash"`
}

type positionResponse struct {
	Symbol string `json:"symbol"`
	Qty    string `json:"qty"`
}

// GetAccount reads real cash and open positions from Alpaca — two
// separate requests, since Alpaca's account endpoint doesn't include
// positions (unlike Binance's combined account response).
func (c *Client) GetAccount(ctx context.Context) (Account, error) {
	body, err := c.get(ctx, "/v2/account")
	if err != nil {
		return Account{}, fmt.Errorf("alpaca: get account: %w", err)
	}
	var raw accountResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Account{}, fmt.Errorf("alpaca: get account: unmarshal: %w", err)
	}
	cash, err := strconv.ParseFloat(raw.Cash, 64)
	if err != nil {
		return Account{}, fmt.Errorf("alpaca: get account: parse cash: %w", err)
	}

	posBody, err := c.get(ctx, "/v2/positions")
	if err != nil {
		return Account{}, fmt.Errorf("alpaca: get positions: %w", err)
	}
	var rawPositions []positionResponse
	if err := json.Unmarshal(posBody, &rawPositions); err != nil {
		return Account{}, fmt.Errorf("alpaca: get positions: unmarshal: %w", err)
	}

	account := Account{Cash: cash}
	for _, p := range rawPositions {
		qty, err := strconv.ParseFloat(p.Qty, 64)
		if err != nil {
			return Account{}, fmt.Errorf("alpaca: get positions: parse %s quantity: %w", p.Symbol, err)
		}
		account.Positions = append(account.Positions, AccountPosition{Asset: p.Symbol, Quantity: qty})
	}
	return account, nil
}
