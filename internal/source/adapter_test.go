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

func (s *stubCrawler) SiteName() string                    { return s.name }
func (s *stubCrawler) FetchPosts() ([]crawler.Post, error) { return s.posts, nil }

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

	adapterA := NewAdapter(crawlerA, nil)
	adapterB := NewAdapter(crawlerB, nil)

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

	items, err := NewAdapter(c, nil).Fetch(context.Background())
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

	items, err := NewAdapter(c, nil).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ImageData != nil {
		t.Errorf("non-whitelisted source should not carry an image")
	}
}

func TestAdapter_LegacyKeyShimSuppressesOldSeen(t *testing.T) {
	// kosha 키 전환(displayNo→pstNo): 옛 표시번호 키로만 seen 인 항목은
	// 프레임워크로 내보내면 새 키로는 미발송이라 통째로 재발송된다.
	// 심이 여기서 걸러야 한다.
	c := &stubCrawler{
		name: "kosha",
		posts: []crawler.Post{
			{PostID: "P-OLD", LegacyPostID: "101", Title: "already sent", URL: "u"},
			{PostID: "P-NEW", LegacyPostID: "102", Title: "genuinely new", URL: "u"},
		},
	}
	seen := func(site, postID string) bool {
		return site == "kosha" && postID == "101" // 옛 키만 bot_seen 에 있음
	}

	items, err := NewAdapter(c, seen).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "kosha:P-NEW" {
		t.Fatalf("want only kosha:P-NEW, got %+v", items)
	}
}

func TestAdapter_LegacyKeyShimKeepsNewKeySeenItems(t *testing.T) {
	// 새 키로 이미 seen 인 항목은 옛 키와 무관하게 그대로 내보낸다 —
	// dedup 판정은 프레임워크 몫이고, 심은 «새 키가 모르는» 항목에만 낀다.
	c := &stubCrawler{
		name: "kosha",
		posts: []crawler.Post{
			{PostID: "P1", LegacyPostID: "50", Title: "t", URL: "u"},
		},
	}
	seen := func(site, postID string) bool { return true } // 둘 다 seen

	items, err := NewAdapter(c, seen).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "kosha:P1" {
		t.Fatalf("want kosha:P1 passed through, got %+v", items)
	}
}

func TestAdapter_NoLegacyIDMeansNoShim(t *testing.T) {
	// LegacyPostID 가 없는 크롤러(모든 비 kosha 소스)는 심의 영향을 받지
	// 않는다 — 옛 키 조회 자체가 일어나면 안 된다.
	c := &stubCrawler{
		name:  "moel",
		posts: []crawler.Post{{PostID: "7", Title: "t", URL: "u"}},
	}
	seen := func(site, postID string) bool {
		t.Errorf("seen consulted for %s:%s — LegacyPostID 없는 포스트는 조회하지 않아야 한다", site, postID)
		return false
	}

	items, err := NewAdapter(c, seen).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "moel:7" {
		t.Fatalf("want moel:7, got %+v", items)
	}
}
