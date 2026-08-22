package newsfeed

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"market-data/internal/httpclient"
)

type Item struct {
	Source      string
	Title       string
	Body        string
	URL         string
	PublishedAt time.Time
}

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func Fetch(ctx context.Context, client *httpclient.Client, sourceName, feedURL string) ([]Item, error) {
	body, err := client.Get(ctx, feedURL)
	if err != nil {
		return nil, err
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("newsfeed: decode %s: %w", sourceName, err)
	}

	items := make([]Item, 0, len(feed.Channel.Items))
	for _, raw := range feed.Channel.Items {
		published, err := parsePubDate(raw.PubDate)
		if err != nil {
			continue // skip items with an unparseable date rather than failing the whole feed
		}
		items = append(items, Item{
			Source:      sourceName,
			Title:       raw.Title,
			Body:        raw.Description,
			URL:         raw.Link,
			PublishedAt: published,
		})
	}
	return items, nil
}

// pubDateFormats covers the RSS pubDate variants observed across feeds:
// numeric offset (RFC1123Z, e.g. Cointelegraph/CoinDesk) and named zone
// abbreviation like "GMT" (RFC1123, e.g. MarketWatch).
var pubDateFormats = []string{time.RFC1123Z, time.RFC1123}

func parsePubDate(raw string) (time.Time, error) {
	var lastErr error
	for _, format := range pubDateFormats {
		t, err := time.Parse(format, raw)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}
