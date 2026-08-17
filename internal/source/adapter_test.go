package source

import (
	"context"
	"testing"

	"github.com/bc1qwerty/safety-alarm-bot/internal/crawler"
)

// stubCrawler returns fixed posts for testing.
type stubCrawler struct {
	name  string
	posts []crawler.Post
}

func (s *stubCrawler) SiteName() string                      { return s.name }
func (s *stubCrawler) FetchPosts() ([]crawler.Post, error)   { return s.posts, nil }
func (s *stubCrawler) GetNewPosts() ([]crawler.Post, error)  { return s.posts, nil }

func TestAdapter_IDPrefixedWithSiteName(t *testing.T) {
	// Two crawlers returning the same PostID "100" must produce
	// distinct item IDs so they don't collide in bot_seen.
	crawlerA := &stubCrawler{
		name:  "kosha",
		posts: []crawler.Post{{PostID: "100", Title: "A", URL: "http://a", Source: "A"}},
	}
	crawlerB := &stubCrawler{
		name:  "moel",
		posts: []crawler.Post{{PostID: "100", Title: "B", URL: "http://b", Source: "B"}},
	}

	adapterA := NewAdapter(crawlerA)
	adapterB := NewAdapter(crawlerB)

	itemsA, err := adapterA.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	itemsB, err := adapterB.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(itemsA) != 1 || len(itemsB) != 1 {
		t.Fatalf("expected 1 item each, got %d and %d", len(itemsA), len(itemsB))
	}

	// IDs must be prefixed with site name to avoid collision
	if itemsA[0].ID == itemsB[0].ID {
		t.Errorf("item IDs must differ across crawlers, both got %q", itemsA[0].ID)
	}
	if itemsA[0].ID != "kosha:100" {
		t.Errorf("expected kosha:100, got %q", itemsA[0].ID)
	}
	if itemsB[0].ID != "moel:100" {
		t.Errorf("expected moel:100, got %q", itemsB[0].ID)
	}
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n and then some pixels")

func TestAdapter_AccidentImageRidesAlong(t *testing.T) {
	// 중대재해 사이렌's poster is inlined as base64 in the API response,
	// so the bytes are the only way it can reach Telegram.
	c := &stubCrawler{
		name: "kosha_accident",
		posts: []crawler.Post{{
			PostID: "837", Title: "(260812)중대재해 발생 알림(2)",
			URL: "http://k", Source: "중대재해 사이렌", ImageData: pngBytes,
		}},
	}

	items, err := NewAdapter(c).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(items[0].ImageData) != string(pngBytes) {
		t.Errorf("image bytes were dropped on the way to the framework Item")
	}
	// The extension is sniffed, not assumed — Telegram infers the upload's
	// MIME type from it.
	if items[0].ImageName != "kosha_accident-837.png" {
		t.Errorf("image name = %q, want kosha_accident-837.png", items[0].ImageName)
	}
}

func TestAdapter_ArchiveThumbnailStaysOff(t *testing.T) {
	// kosha_archive's ops entries carry an ImageData thumbnail too, but
	// it is a cover preview rather than the notice itself. Only
	// whitelisted sources turn into photo posts.
	c := &stubCrawler{
		name: "kosha_archive_ops",
		posts: []crawler.Post{{
			PostID: "1", Title: "OPS", URL: "http://k",
			Source: "안전보건공단 자료실", ImageData: pngBytes,
		}},
	}

	items, err := NewAdapter(c).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ImageData != nil {
		t.Errorf("non-whitelisted source should not carry an image")
	}
}
