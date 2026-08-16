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
		published, err := time.Parse(time.RFC1123Z, raw.PubDate)
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
