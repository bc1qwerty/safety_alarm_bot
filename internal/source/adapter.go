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

// ItemID is the dedup key this adapter emits for a crawler post: bot_seen
// rows are keyed (source=siteName, item_id=ItemID). main.go's seen closure
// must build the exact same key, so both call sites go through this helper.
func ItemID(siteName, postID string) string {
	return siteName + ":" + postID
}

// CrawlerAdapter converts a legacy Crawler to a framework Source.
type CrawlerAdapter struct {
	crawler crawler.Crawler
	seen    crawler.SeenFunc
}

func NewAdapter(c crawler.Crawler, seen crawler.SeenFunc) *CrawlerAdapter {
	return &CrawlerAdapter{crawler: c, seen: seen}
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
		// dedup 키 전환 심 (kosha displayNo→pstNo, 2026-09-13). 옛 키(게시판
		// 표시번호)는 글 하나가 삭제되면 전체 번호가 밀려 재사용되는 불안정한
		// 키였다. 재발송 폭풍 없이 갈아타기 위해 읽기는 «옛 키 OR 새 키»로
		// 보고, 쓰기(프레임워크 MarkSeen)는 새 키로만 한다: 옛 키로만 seen 인
		// 항목은 여기서 걸러 프레임워크에 아예 안 보인다. 목록 첫 페이지가
		// 전부 새 키 항목으로 물갈이되면(신규 10건 뒤, 넉넉히 보존 주기 한 번)
		// 이 블록과 Post.LegacyPostID 를 함께 제거해도 된다.
		if p.LegacyPostID != "" && a.seen != nil &&
			!a.seen(prefix, p.PostID) && a.seen(prefix, p.LegacyPostID) {
			continue
		}
		item := core.Item{
			ID:       ItemID(prefix, p.PostID),
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
		// A crawler that produced FileData already decided the attachment is
		// the material itself (a KOSHA OPS sheet, a booklet PDF), and it
		// checked Telegram's size cap before downloading. No whitelist here:
		// unlike a thumbnail, there is no case where the actual document is
		// the wrong thing to post.
		//
		// Until this existed the bytes were fetched and then dropped on the
		// floor — booklet and video crawlers had been downloading files that
		// never reached the channel.
		if len(p.FileData) > 0 {
			item.FileData = p.FileData
			item.FileName = p.FileName
		}
		items = append(items, item)
	}
	return items, nil
}
