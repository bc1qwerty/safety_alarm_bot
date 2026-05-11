package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	koshaBoardURL  = "https://www.kosha.or.kr/notification/notice/contruction?bbsId=B2025021400001"
	koshaDetailURL = koshaBoardURL + "&pstNo="
	// koshaSource is the human-readable source label shown in notifications.
	// Kept as a \uXXXX literal so the file is ASCII-clean across editors.
	koshaSource = "안전보건공단" // 안전보건공단
)

// KoshaNoticeCrawler crawls 안전보건공단 notices using chromedp.
type KoshaNoticeCrawler struct {
	BaseCrawler
}

func NewKoshaNoticeCrawler() *KoshaNoticeCrawler {
	return &KoshaNoticeCrawler{BaseCrawler{Name: "kosha"}}
}

// baseListItem represents an item from koshaTboard.bbsInfo.tboard.result.search.baseList.
// KOSHA returns sticky notices first (each with totalCount=1) followed by regular posts
// (totalCount=total regular count). Both groups restart rnum at 1, so we have to split
// them by totalCount before mapping rnum -> pstNo, otherwise the sticky's pstNo is
// overwritten by the first regular post's pstNo and every regular row is off by one.
type baseListItem struct {
	Rnum       int    `json:"rnum"`
	PstNo      string `json:"pstNo"`
	TotalCount int    `json:"totalCount"`
}

// koshaRowItem is one row scraped from the KOSHA notice list DOM.
type koshaRowItem struct {
	Num   string `json:"num"`
	Title string `json:"title"`
}

func (c *KoshaNoticeCrawler) FetchPosts() ([]Post, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-zygote", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-features", "Translate,OptimizationHints,MediaRouter,InterestFeedContentSuggestions,VizDisplayCompositor"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.WindowSize(1280, 720),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var baseListJSON string
	var rowItems []koshaRowItem

	err := chromedp.Run(ctx,
		chromedp.Navigate(koshaBoardURL),
		chromedp.WaitVisible(".tboard_list_row", chromedp.ByQuery),
		// Extract baseList from JS context (JSON.stringify so unmarshal to a Go string)
		chromedp.Evaluate(`JSON.stringify(koshaTboard.bbsInfo.tboard.result.search.baseList)`, &baseListJSON),
		// Return a native JS array of {num,title} - chromedp marshals it to []koshaRowItem directly.
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
				return result;
			})()
		`, &rowItems),
	)
	if err != nil {
		return nil, fmt.Errorf("[kosha] chromedp failed: %w", err)
	}

	var baseList []baseListItem
	if err := json.Unmarshal([]byte(baseListJSON), &baseList); err != nil {
		return nil, fmt.Errorf("[kosha] baseList parse failed: %w", err)
	}

	posts := pairKoshaPosts(rowItems, baseList, koshaSource, koshaDetailURL)
	log.Printf("[kosha] %d posts emitted (%d rows scraped, %d baseList entries)", len(posts), len(rowItems), len(baseList))
	return posts, nil
}

// pairKoshaPosts joins each numbered row with its pstNo. It is a pure function
// so the off-by-one regression that this fix addresses can be locked in by
// kosha_notice_test.go.
//
// Mapping rules:
//   - KOSHA's baseList contains two groups: stickies (totalCount=1 each) and
//     regular posts (totalCount=N for all). Both groups restart rnum at 1, so
//     we keep only the largest-totalCount group when building rnum -> pstNo.
//   - Stickies in the DOM have non-numeric display numbers (e.g. "공지") and
//     are filtered out by isDigits, after which regular rows are indexed by
//     their position within the regular-only sequence (regularIdx).
//
// Invariant (locked in to prevent future silent regressions):
//
//	parsedDisplayNum == mainTotal - regularIdx + 1
//
// KOSHA always shows the newest post first with display number = totalCount,
// and decrements per row. If a row violates this -- because KOSHA changes its
// schema, inserts a new kind of row, or returns unexpected ordering -- the
// row is dropped with a WARN log rather than emitted with a wrong URL. This
// is the safeguard that prevents recurrence of the title/link mismatch.
func pairKoshaPosts(rowItems []koshaRowItem, baseList []baseListItem, source, detailURL string) []Post {
	mainTotal := 0
	for _, item := range baseList {
		if item.TotalCount > mainTotal {
			mainTotal = item.TotalCount
		}
	}
	pstMap := make(map[int]string)
	for _, item := range baseList {
		if item.TotalCount == mainTotal {
			pstMap[item.Rnum] = item.PstNo
		}
	}

	var posts []Post
	regularIdx := 0
	for _, row := range rowItems {
		numText := strings.TrimSpace(row.Num)
		if !isDigits(numText) {
			continue
		}
		regularIdx++

		if mainTotal > 0 {
			parsedNum, _ := strconv.Atoi(numText)
			expectedNum := mainTotal - regularIdx + 1
			if parsedNum != expectedNum {
				log.Printf("[kosha] WARN row#%d numText=%q expected display=%d -- skipping (sticky offset or schema change?)",
					regularIdx, numText, expectedNum)
				continue
			}
		}

		pstNo, ok := pstMap[regularIdx]
		if !ok || pstNo == "" {
			log.Printf("[kosha] WARN row#%d (No%s) has no pstNo in baseList -- skipping", regularIdx, numText)
			continue
		}

		posts = append(posts, Post{
			PostID: numText,
			Title:  row.Title,
			URL:    detailURL + pstNo,
			Source: source,
		})
	}
	return posts
}

func (c *KoshaNoticeCrawler) GetNewPosts() ([]Post, error) {
	posts, err := c.FetchPosts()
	if err != nil {
		return nil, err
	}
	return FilterNewPosts(c.Name, posts), nil
}
