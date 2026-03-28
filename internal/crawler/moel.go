package crawler

import (
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
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(moelListURL)
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
