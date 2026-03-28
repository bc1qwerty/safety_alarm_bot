package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	koshaBoardURL  = "https://www.kosha.or.kr/notification/notice/contruction?bbsId=B2025021400001"
	koshaDetailURL = koshaBoardURL + "&pstNo="
)

// KoshaNoticeCrawler crawls 안전보건공단 notices using chromedp.
type KoshaNoticeCrawler struct {
	BaseCrawler
}

func NewKoshaNoticeCrawler() *KoshaNoticeCrawler {
	return &KoshaNoticeCrawler{BaseCrawler{Name: "kosha"}}
}

// baseListItem represents an item from koshaTboard.bbsInfo.tboard.result.search.baseList.
type baseListItem struct {
	Rnum  int    `json:"rnum"`
	PstNo string `json:"pstNo"`
}

func (c *KoshaNoticeCrawler) FetchPosts() ([]Post, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.WindowSize(1280, 720),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var baseListJSON string
	var rowsData []map[string]string

	err := chromedp.Run(ctx,
		chromedp.Navigate(koshaBoardURL),
		chromedp.WaitVisible(".tboard_list_row", chromedp.ByQuery),
		// Extract baseList from JS context
		chromedp.Evaluate(`JSON.stringify(koshaTboard.bbsInfo.tboard.result.search.baseList)`, &baseListJSON),
		// Extract rows data from DOM
		chromedp.Evaluate(`
			(() => {
				const rows = document.querySelectorAll('.tboard_list_row');
				const result = [];
				rows.forEach(row => {
					const numEl = row.querySelector("[data-tboard-artcl-no='D020100001']");
					const titleEl = row.querySelector("a.tboard_list_subject");
					if (numEl && titleEl) {
						let num = numEl.textContent.trim().replace(/,/g, '').replace('No', '').trim();
						let title = titleEl.getAttribute('title') || titleEl.textContent.trim();
						result.push({num: num, title: title});
					}
				});
				return JSON.stringify(result);
			})()
		`, &rowsData),
	)
	if err != nil {
		return nil, fmt.Errorf("[kosha] chromedp failed: %w", err)
	}

	// Parse baseList
	var baseList []baseListItem
	if err := json.Unmarshal([]byte(baseListJSON), &baseList); err != nil {
		return nil, fmt.Errorf("[kosha] baseList parse failed: %w", err)
	}
	pstMap := make(map[int]string)
	for _, item := range baseList {
		pstMap[item.Rnum] = item.PstNo
	}

	// Parse rowsData - it was returned as a JSON string, need to re-unmarshal
	// chromedp.Evaluate with a string return may need special handling
	var rowItems []struct {
		Num   string `json:"num"`
		Title string `json:"title"`
	}

	// The Evaluate call above returns a string (JSON.stringify result)
	// We need to handle this as the rowsData might be a string
	rowsJSON := ""
	// Re-run to get rows as string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`
			(() => {
				const rows = document.querySelectorAll('.tboard_list_row');
				const result = [];
				rows.forEach(row => {
					const numEl = row.querySelector("[data-tboard-artcl-no='D020100001']");
					const titleEl = row.querySelector("a.tboard_list_subject");
					if (numEl && titleEl) {
						let num = numEl.textContent.trim().replace(/,/g, '').replace('No', '').trim();
						let title = titleEl.getAttribute('title') || titleEl.textContent.trim();
						result.push({"num": num, "title": title});
					}
				});
				return result;
			})()
		`, &rowItems),
	)
	if err != nil {
		// Fallback: try parsing the original string
		_ = json.Unmarshal([]byte(rowsJSON), &rowItems)
	}

	var posts []Post
	for idx, row := range rowItems {
		numText := strings.TrimSpace(row.Num)
		if !isDigits(numText) {
			continue
		}

		pstNo := pstMap[idx+1]
		url := koshaBoardURL
		if pstNo != "" {
			url = koshaDetailURL + pstNo
		}

		posts = append(posts, Post{
			PostID: numText,
			Title:  row.Title,
			URL:    url,
			Source:  "\uC548\uC804\uBCF4\uAC74\uACF5\uB2E8", // 안전보건공단
		})
	}

	log.Printf("[kosha] %d posts parsed", len(posts))
	return posts, nil
}

func (c *KoshaNoticeCrawler) GetNewPosts() ([]Post, error) {
	posts, err := c.FetchPosts()
	if err != nil {
		return nil, err
	}
	return FilterNewPosts(c.Name, posts), nil
}
