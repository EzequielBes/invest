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
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("%s: status %d: %s", url, resp.StatusCode, body)
	}
	return body, nil
}
