package crawler

// DownloadURL represents a labeled download link.
type DownloadURL struct {
	Label string
	URL   string
}

// Post represents a crawled article.
type Post struct {
	PostID       string
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
	GetNewPosts() ([]Post, error)
}

// BaseCrawler provides shared get_new_posts logic.
type BaseCrawler struct {
	Name string
}

func (b *BaseCrawler) SiteName() string {
	return b.Name
}
