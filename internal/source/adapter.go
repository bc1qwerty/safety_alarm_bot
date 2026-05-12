package source

import (
	"context"

	"github.com/bc1qwerty/safety-alarm-bot/internal/crawler"
	"github.com/bc1qwerty/txid-bot-framework/pkg/core"
)

// CrawlerAdapter converts a legacy Crawler to a framework Source.
type CrawlerAdapter struct {
	crawler crawler.Crawler
}

func NewAdapter(c crawler.Crawler) *CrawlerAdapter {
	return &CrawlerAdapter{crawler: c}
}

func (a *CrawlerAdapter) Name() string {
	return a.crawler.SiteName()
}

func (a *CrawlerAdapter) Fetch(ctx context.Context) ([]core.Item, error) {
	posts, err := a.crawler.FetchPosts()
	if err != nil {
		return nil, err
	}

	var items []core.Item
	for _, p := range posts {
		items = append(items, core.Item{
			ID:       p.PostID,
			Title:    p.Title,
			URL:      p.URL,
			Content:  p.Title, // Summary is the title for these alerts
			Category: p.Source,
		})
	}
	return items, nil
}
