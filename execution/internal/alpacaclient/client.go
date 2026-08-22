// execution/internal/alpacaclient/client.go
package alpacaclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// Client is an authenticated Alpaca paper-trading client. Unlike Binance's
// HMAC-signed requests, Alpaca authenticates via two plain request
// headers — simpler, no query-string signature scheme needed.
type Client struct {
	http    *http.Client
	limiter *rate.Limiter
	apiKey  string
	secret  string
	baseURL string
}

// New constructs a Client. baseURL is a parameter (not hardcoded) so
// tests can point it at an httptest server — production callers pass
// Alpaca's real paper-trading base URL.
func New(apiKey, secret, baseURL string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(5), 10),
		apiKey:  apiKey,
		secret:  secret,
		baseURL: baseURL,
	}
}

func (c *Client) authHeaders(req *http.Request) {
	req.Header.Set("APCA-API-KEY-ID", c.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", c.secret)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	c.authHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody := respBody
		if len(errBody) > 500 {
			errBody = errBody[:500]
		}
		return nil, fmt.Errorf("alpaca: %s %s: status %d: %s", method, path, resp.StatusCode, errBody)
	}
	return respBody, nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

func (c *Client) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, body)
}

func (c *Client) delete(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodDelete, path, nil)
}
