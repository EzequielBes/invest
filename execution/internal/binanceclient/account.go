// execution/internal/binanceclient/account.go
package binanceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Account is the subset of Binance's GET /fapi/v2/account response this
// client needs: available balance and any open positions.
type Account struct {
	AvailableBalance float64
	Positions        []AccountPosition
}

// AccountPosition is one open position, converted from Binance's
// USDT-margined perpetual symbol (e.g. "BTCUSDT") back to the canonical
// asset symbol ("BTC") — same convention market-data's binance collector
// uses for the reverse conversion (see market-data/internal/exchange/binance).
type AccountPosition struct {
	Asset    string
	Quantity float64
}

type accountResponse struct {
	AvailableBalance string `json:"availableBalance"`
	Positions        []struct {
		Symbol      string `json:"symbol"`
		PositionAmt string `json:"positionAmt"`
	} `json:"positions"`
}

// GetAccount reads real balance and open positions from the exchange.
// Positions with a zero quantity are omitted (Binance's account endpoint
// lists every symbol it tracks, most with a zero position).
func (c *Client) GetAccount(ctx context.Context) (Account, error) {
	body, err := c.signedRequest(ctx, http.MethodGet, "/fapi/v2/account", url.Values{})
	if err != nil {
		return Account{}, fmt.Errorf("binance: get account: %w", err)
	}
	var raw accountResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return Account{}, fmt.Errorf("binance: get account: unmarshal: %w", err)
	}
	balance, err := strconv.ParseFloat(raw.AvailableBalance, 64)
	if err != nil {
		return Account{}, fmt.Errorf("binance: get account: parse balance: %w", err)
	}
	account := Account{AvailableBalance: balance}
	for _, p := range raw.Positions {
		qty, err := strconv.ParseFloat(p.PositionAmt, 64)
		if err != nil {
			return Account{}, fmt.Errorf("binance: get account: parse position %s: %w", p.Symbol, err)
		}
		if qty == 0 {
			continue
		}
		account.Positions = append(account.Positions, AccountPosition{
			Asset:    strings.TrimSuffix(p.Symbol, "USDT"),
			Quantity: qty,
		})
	}
	return account, nil
}
