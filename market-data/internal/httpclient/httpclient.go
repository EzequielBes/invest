package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/time/rate"
)

// Client wraps http.Client with a per-instance rate limiter, so each
// exchange collector can be configured to its own API's rate limit.
type Client struct {
	http    *http.Client
	limiter *rate.Limiter
}

func New(requestsPerSecond float64, burst int) *Client {
	return &Client{
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), burst),
	}
}

func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	return c.GetWithHeaders(ctx, url, nil)
}

// GetWithHeaders is Get plus caller-supplied request headers — needed for
// APIs authenticated via headers instead of a URL query param (e.g.
// Alpaca's APCA-API-KEY-ID/APCA-API-SECRET-KEY), so credentials never end
// up in a logged URL.
func (c *Client) GetWithHeaders(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
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
		errBody := body
		if len(errBody) > 500 {
			errBody = errBody[:500]
		}
		return nil, fmt.Errorf("%s: status %d: %s", url, resp.StatusCode, errBody)
	}
	return body, nil
}
