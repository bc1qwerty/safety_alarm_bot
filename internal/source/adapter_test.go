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
