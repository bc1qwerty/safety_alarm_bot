package crawler

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	moelBaseURL = "https://www.moel.go.kr"
	moelListURL = moelBaseURL + "/news/notice/noticeList.do"
)

// MoelCrawler crawls 고용노동부 notices.
type MoelCrawler struct {
	BaseCrawler
}

func NewMoelCrawler() *MoelCrawler {
	return &MoelCrawler{BaseCrawler{Name: "moel"}}
}

func (c *MoelCrawler) FetchPosts() ([]Post, error) {
	// moel.go.kr blocks Go's default HTTP/2 transport (RST during handshake).
	// Force HTTP/1.1 by disabling h2 negotiation. curl works fine over h1.1.
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
		ForceAttemptHTTP2: false,
	}
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	req, err := http.NewRequest("GET", moelListURL, nil)
	if err != nil {
		return nil, fmt.Errorf("[moel] new request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en;q=0.8")
	req.Header.Set("Referer", moelBaseURL+"/news/notice/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[moel] request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[moel] HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[moel] parse failed: %w", err)
	}

	var posts []Post
	doc.Find("table.tstyle_list tbody tr").Each(func(_ int, row *goquery.Selection) {
		numTd := row.Find("td[aria-label='번호']")
		if numTd.Length() == 0 {
			return
		}
		numText := strings.TrimSpace(numTd.Text())
		if !isDigits(numText) {
			return
		}

		linkTag := row.Find("strong.b_tit a")
		if linkTag.Length() == 0 {
			return
		}

		title, exists := linkTag.Attr("title")
		if !exists || title == "" {
			title = strings.TrimSpace(linkTag.Text())
		}

		href, _ := linkTag.Attr("href")
		if href != "" && !strings.HasPrefix(href, "http") {
			href = moelBaseURL + href
		}

		posts = append(posts, Post{
			PostID: numText,
			Title:  title,
			URL:    href,
			Source:  "\uACE0\uC6A9\uB178\uB3D9\uBD80", // 고용노동부
		})
	})

	log.Printf("[moel] %d posts parsed", len(posts))
	return posts, nil
}

func (c *MoelCrawler) GetNewPosts() ([]Post, error) {
	posts, err := c.FetchPosts()
	if err != nil {
		return nil, err
	}
	return FilterNewPosts(c.Name, posts), nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
