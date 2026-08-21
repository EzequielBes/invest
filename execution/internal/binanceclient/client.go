// execution/internal/binanceclient/client.go
package binanceclient

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Client is an authenticated Binance Futures client. Every request is
// signed with HMAC-SHA256 per Binance's futures API auth scheme
// (timestamp + all params, signed, appended as &signature=...).
type Client struct {
	http     *http.Client
	limiter  *rate.Limiter
	apiKey   string
	secret   string
	baseURL  string
	filters  map[string]lotSize
	filterMu sync.Mutex
}

// New constructs a Client. baseURL is a parameter (not hardcoded here) so
// tests can point it at an httptest server — production callers pass the
// real testnet base URL.
func New(apiKey, secret, baseURL string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(5), 10),
		apiKey:  apiKey,
		secret:  secret,
		baseURL: baseURL,
		filters: make(map[string]lotSize),
	}
}

// publicRequest sends Binance's unsigned market-data requests.
func (c *Client) publicRequest(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("binance: GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) sign(query string) string {
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(query))
	return hex.EncodeToString(mac.Sum(nil))
}

// signedRequest builds a signed request: params gets `timestamp` added,
// is encoded as the query string, signed, and the signature appended.
// GET/DELETE send the signed query string in the URL; POST sends it as
// the request body (form-encoded) — matching Binance's futures API for
// each of these methods.
func (c *Client) signedRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	query := params.Encode()
	query += "&signature=" + c.sign(query)

	var req *http.Request
	var err error
	if method == http.MethodPost {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(query))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path+"?"+query, nil)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody := body
		if len(errBody) > 500 {
			errBody = errBody[:500]
		}
		return nil, fmt.Errorf("binance: %s %s: status %d: %s", method, path, resp.StatusCode, errBody)
	}
	return body, nil
}
