package crawler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	ebookBase        = "https://www.kosha.or.kr/ebook"
	monthlyListURL   = ebookBase + "/fcatalog/include/monthly_list.jsp"
	ebookDownloadURL = ebookBase + "/fcatalog/download.jsp"
	ebookFileLimit   = 50 * 1024 * 1024 // 50MB
)

var (
	sdirRe     = regexp.MustCompile(`sdir=(\d+)`)
	pdfFilesRe = regexp.MustCompile(`show_download\('([^']*)',\s*'([^']*)'`)
)

// KoshaEbookCrawler crawls 월간 안전보건 e-Book.
type KoshaEbookCrawler struct {
	BaseCrawler
	seen SeenFunc
}

func NewKoshaEbookCrawler(seen SeenFunc) *KoshaEbookCrawler {
	return &KoshaEbookCrawler{BaseCrawler: BaseCrawler{Name: "kosha_ebook"}, seen: seen}
}

func (c *KoshaEbookCrawler) FetchPosts() ([]Post, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(monthlyListURL)
	if err != nil {
		return nil, fmt.Errorf("[kosha_ebook] request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[kosha_ebook] HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[kosha_ebook] parse failed: %w", err)
	}

	var posts []Post
	doc.Find("ul.e-booklist > li").Each(func(_ int, item *goquery.Selection) {
		post := c.parseItem(item)
		if post != nil {
			posts = append(posts, *post)
		}
	})

	log.Printf("[kosha_ebook] %d posts parsed", len(posts))
	return posts, nil
}

func (c *KoshaEbookCrawler) parseItem(item *goquery.Selection) *Post {
	detailLink := item.Find("a[href*='detail']")
	if detailLink.Length() == 0 {
		return nil
	}

	href, exists := detailLink.Attr("href")
	if !exists {
		return nil
	}

	sdirMatch := sdirRe.FindStringSubmatch(href)
	if len(sdirMatch) < 2 {
		return nil
	}
	sdir := sdirMatch[1]

	titleEl := item.Find(".e-title span")
	title := ""
	if titleEl.Length() > 0 {
		title = strings.TrimSpace(titleEl.Text())
	}

	// Extract PDF filenames from show_download() JS call
	var pdfFiles []string
	pdfLink := item.Find("a[href*='show_download']")
	if pdfLink.Length() > 0 {
		pdfHref, _ := pdfLink.Attr("href")
		pdfMatch := pdfFilesRe.FindStringSubmatch(pdfHref)
		if len(pdfMatch) >= 2 {
			for _, f := range strings.Split(pdfMatch[1], ";") {
				f = strings.TrimSpace(f)
				if f != "" {
					pdfFiles = append(pdfFiles, f)
				}
			}
		}
	}

	viewerURL := fmt.Sprintf("%s/fcatalog/ecatalog5.jsp?Dir=%s&catimage=&listCall=Y&eclang=", ebookBase, sdir)

	var dlURLs []DownloadURL
	for _, fn := range pdfFiles {
		decoded, _ := url.QueryUnescape(fn)
		dlURL := fmt.Sprintf("%s?kd=feb&sdir=%s&cimg=&fn=%s", ebookDownloadURL, sdir, url.QueryEscape(decoded))
		dlURLs = append(dlURLs, DownloadURL{Label: decoded, URL: dlURL})
	}

	post := &Post{
		PostID:       sdir,
		Title:        title,
		URL:          viewerURL,
		Source:       "\uC6D4\uAC04 \uC548\uC804\uBCF4\uAC74", // 월간 안전보건
		DownloadURLs: dlURLs,
	}

	// Try to download the first PDF that fits within the size limit.
	// 이미 발송된(seen) 호는 첨부를 받지 않는다 — 목록에 남아 있는 과월호
	// PDF 를 매 폴 새로 내려받고 dedup 뒤에서 버리던 낭비였다.
	if len(pdfFiles) > 0 && (c.seen == nil || !c.seen(c.Name, post.PostID)) {
		fileBytes, fileName := c.downloadPDF(sdir, pdfFiles)
		if fileBytes != nil {
			post.FileData = fileBytes
			post.FileName = fileName
		}
	}

	return post
}

func (c *KoshaEbookCrawler) downloadPDF(sdir string, pdfFiles []string) ([]byte, string) {
	client := &http.Client{
		Timeout: 120 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	for _, fnRaw := range pdfFiles {
		fn, _ := url.QueryUnescape(fnRaw)
		dlURL := fmt.Sprintf("%s?kd=feb&sdir=%s&cimg=&fn=%s", ebookDownloadURL, sdir, url.QueryEscape(fn))

		// HEAD request to check content length
		headResp, err := client.Head(dlURL)
		if err != nil {
			log.Printf("[kosha_ebook] HEAD request failed: %v", err)
			continue
		}
		headResp.Body.Close()

		contentLength := headResp.ContentLength
		if contentLength > ebookFileLimit {
			continue
		}

		// GET the file
		resp, err := client.Get(dlURL)
		if err != nil {
			log.Printf("[kosha_ebook] PDF download failed: %v", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			log.Printf("[kosha_ebook] PDF download HTTP %d — skipping", resp.StatusCode)
			continue
		}
		// download.jsp 는 실패 시에도 200 에러 페이지를 줄 수 있다 — HTML 이
		// 그대로 'PDF' 첨부로 나가지 않게 막는다.
		if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
			resp.Body.Close()
			log.Printf("[kosha_ebook] PDF download got Content-Type %q — skipping", ct)
			continue
		}

		// HEAD 가 ContentLength 를 안 주면(-1) 위 상한 검사가 통과해 버리므로,
		// 읽기 자체를 상한+1 로 자른다. 넘친 파일은 아래 크기 검사가 버린다.
		data, err := io.ReadAll(io.LimitReader(resp.Body, ebookFileLimit+1))
		resp.Body.Close()
		if err != nil {
			log.Printf("[kosha_ebook] PDF read failed: %v", err)
			continue
		}

		if int64(len(data)) <= ebookFileLimit {
			return data, fn
		}
	}

	return nil, ""
}
