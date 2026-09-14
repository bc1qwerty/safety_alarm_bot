package crawler

// DownloadURL represents a labeled download link.
type DownloadURL struct {
	Label string
	URL   string
}

// Post represents a crawled article.
type Post struct {
	PostID string
	// LegacyPostID carries this post's previous dedup key when a crawler
	// has switched PostID schemes (kosha: displayNo -> pstNo, 2026-09-13).
	// The adapter's dual-key shim drops posts already seen under it so the
	// switch cannot redispatch the backlog. Empty for everyone else.
	LegacyPostID string
	Title        string
	URL          string
	Source       string
	ImageData    []byte
	FileData     []byte
	FileName     string
	DownloadURLs []DownloadURL
}

// Crawler is the interface all crawlers implement.
type Crawler interface {
	SiteName() string
	FetchPosts() ([]Post, error)
}

// SeenFunc reports whether (siteName, postID) has already been fetched —
// i.e. bot_seen holds the adapter's item key for it (source.ItemID).
// 크롤러는 이것으로 dedup 이 어차피 버릴 항목의 첨부 다운로드를 건너뛴다.
// nil 이면 "안 본 것"으로 취급한다(전부 내려받는 예전 동작).
type SeenFunc func(siteName, postID string) bool

// BaseCrawler provides the shared SiteName implementation.
type BaseCrawler struct {
	Name string
}

func (b *BaseCrawler) SiteName() string {
	return b.Name
}
