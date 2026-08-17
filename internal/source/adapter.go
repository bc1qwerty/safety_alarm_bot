package source

import (
	"context"
	"net/http"

	"github.com/bc1qwerty/safety-alarm-bot/internal/crawler"
	"github.com/bc1qwerty/txid-bot-framework/pkg/core"
)

// imageSources lists the crawlers whose Post.ImageData should be posted
// as a Telegram photo. It is a whitelist rather than "any crawler that
// has bytes" on purpose: kosha_archive's ops entries also carry an
// ImageData thumbnail, but that is a tiny cover preview, not the notice
// itself, and turning those into photo posts would change how an
// unrelated source reads in the channel.
var imageSources = map[string]bool{
	"kosha_accident": true, // 중대재해 사이렌 — the poster IS the alert
}

// imageExt maps a sniffed content type to the filename extension the
// Telegram Bot API uses to infer the upload's MIME type. 중대재해 사이렌
// currently serves PNG, but the payload is an opaque data: URI, so the
// format is sniffed rather than assumed.
func imageExt(b []byte) string {
	switch http.DetectContentType(b) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

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

	prefix := a.crawler.SiteName()
	var items []core.Item
	for _, p := range posts {
		item := core.Item{
			ID:       prefix + ":" + p.PostID,
			Title:    p.Title,
			URL:      p.URL,
			Content:  p.Title, // Summary is the title for these alerts
			Category: p.Source,
		}
		// 중대재해 사이렌 ships its poster inline as a base64 image — there
		// is no public URL for Telegram to fetch, so the bytes have to
		// ride along and be uploaded.
		if imageSources[prefix] && len(p.ImageData) > 0 {
			item.ImageData = p.ImageData
			item.ImageName = prefix + "-" + p.PostID + imageExt(p.ImageData)
		}
		items = append(items, item)
	}
	return items, nil
}
