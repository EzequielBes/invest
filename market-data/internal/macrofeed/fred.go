// Package macrofeed polls FRED (Federal Reserve Economic Data) for real
// macro indicators — Fed funds rate, CPI, unemployment — so the analysis
// pipeline's macro narrative has actual data behind it instead of being
// fabricated by the LLM.
package macrofeed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"market-data/internal/httpclient"
)

const baseURL = "https://api.stlouisfed.org/fred/series/observations"

// Observation is one FRED data point: an observed value on a given date.
type Observation struct {
	ObservedAt time.Time
	Value      float64
}

type fredResponse struct {
	Observations []struct {
		Date  string `json:"date"`
		Value string `json:"value"`
	} `json:"observations"`
}

// Fetch retrieves every available observation for seriesID (e.g.
// "FEDFUNDS", "CPIAUCSL", "UNRATE"). FRED represents a missing data point
// as the literal string "." — those rows are skipped rather than parsed
// as a bogus zero value.
func Fetch(ctx context.Context, client *httpclient.Client, seriesID, apiKey string) ([]Observation, error) {
	u := baseURL + "?" + url.Values{
		"series_id": {seriesID},
		"api_key":   {apiKey},
		"file_type": {"json"},
	}.Encode()
	return fetchFrom(ctx, client, u)
}

func fetchFrom(ctx context.Context, client *httpclient.Client, requestURL string) ([]Observation, error) {
	body, err := client.Get(ctx, requestURL)
	if err != nil {
		return nil, err
	}

	var resp fredResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("macrofeed: decode response: %w", err)
	}

	observations := make([]Observation, 0, len(resp.Observations))
	for _, raw := range resp.Observations {
		if raw.Value == "." {
			continue // FRED's marker for a missing data point
		}
		date, err := time.Parse("2006-01-02", raw.Date)
		if err != nil {
			continue
		}
		var value float64
		if _, err := fmt.Sscanf(raw.Value, "%f", &value); err != nil {
			continue
		}
		observations = append(observations, Observation{ObservedAt: date, Value: value})
	}
	return observations, nil
}
