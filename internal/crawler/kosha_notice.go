package crawler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	koshaBoardURL  = "https://www.kosha.or.kr/notification/notice/contruction?bbsId=B2025021400001"
	koshaDetailURL = koshaBoardURL + "&pstNo="
	// koshaSource is the human-readable source label shown in notifications.
	koshaSource = "안전보건공단"

	// koshaBbsID is the board id of 대표홈페이지 공지사항.
	koshaBbsID = "B2025021400001"
	// koshaProcessURL is the stdtboard JSON API behind the kosha24 Vue SPA.
	// In 2026-05 KOSHA replaced the server-rendered notice board with a Vue
	// single-page app: the old `.tboard_list_row` DOM and the `koshaTboard`
	// JS global no longer exist, so the previous chromedp WaitVisible scrape
	// hung for its full timeout ("context deadline exceeded"). The SPA loads
	// the list from this endpoint, which we now call directly — no headless
	// Chrome, faster, and immune to future DOM/markup changes.
	koshaProcessURL = "https://www.kosha.or.kr/api/compn24/auth/stdtboard/process.do"
	koshaUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0 Safari/537.36"
)

// koshaListReqTemplate is the process.do request payload for serviceId=basicAccess,
// captured from the kosha24 SPA. %s is the bbsId. The SPA submits this JSON as a
// URL-encoded value of the _JSON form field; FetchPosts replicates that encoding.
const koshaListReqTemplate = `{"common":{"frontInfo":{"viewId":"","menuId":"","siteId":""},"frontAuthKey":"","auth":{},"securityInfo":{},"data":{"pagingInfo":null,"whereId":null,"tboard":{"systemCd":"50","channel":"web","bbsId":"%s","bbsGrpId":"","serviceId":"basicAccess"}}},"service":{"info":{"id":"","type":""},"data":{"searchDefaultCndGrid":[{"orPstNm":"","orPstCn":"","curPageCo":1,"recodePageCo":10,"rowsPerPage":10,"pstSeCd":"1200001","atcflCntSrchYn":"Y","artclNoList":[],"pstNoOrder":"Y","isDesc":"Y","sortType":"01","sortOrder":"1","isAddPstCn":"N"}],"searchArtclCndGrid":[]}}}`

// KoshaNoticeCrawler crawls 안전보건공단 notices via the stdtboard JSON API.
type KoshaNoticeCrawler struct {
	BaseCrawler
}

func NewKoshaNoticeCrawler() *KoshaNoticeCrawler {
	return &KoshaNoticeCrawler{BaseCrawler{Name: "kosha"}}
}

// koshaProcessResp is the process.do JSON envelope (only the fields we consume).
type koshaProcessResp struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Response struct {
		// TotalNormalCnt is the number of regular (non-sticky) posts. The
		// board's visible "No" of the newest regular post equals this value.
		TotalNormalCnt int `json:"totalNormalCnt"`
		// PstNoGrid carries one entry per row in display order. KOSHA returns
		// sticky notices first (totalCount=1 each) then regular posts
		// (totalCount=totalNormalCnt); both groups restart rnum at 1.
		PstNoGrid []koshaPstNoItem `json:"pstNoGrid"`
		// BbsPstGrid carries post metadata (title) keyed by pstNo.
		BbsPstGrid []koshaBbsPstItem `json:"bbsPstGrid"`
	} `json:"response"`
}

// koshaPstNoItem maps a display row to its pstNo.
type koshaPstNoItem struct {
	Rnum       int    `json:"rnum"`
	PstNo      string `json:"pstNo"`
	TotalCount int    `json:"totalCount"`
}

// koshaBbsPstItem is one post's metadata; PstNm is the title.
type koshaBbsPstItem struct {
	PstNo string `json:"pstNo"`
	PstNm string `json:"pstNm"`
}

func (c *KoshaNoticeCrawler) FetchPosts() ([]Post, error) {
	jsonBody := fmt.Sprintf(koshaListReqTemplate, koshaBbsID)
	// The SPA sends `_JSON=encodeURIComponent(json)`; form encoding then
	// escapes it a second time, so the value on the wire is double-encoded.
	form := url.Values{}
	form.Set("_JSON", url.QueryEscape(jsonBody))

	req, err := http.NewRequest(http.MethodPost, koshaProcessURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("[kosha] build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", koshaUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[kosha] process.do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[kosha] read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("[kosha] process.do HTTP %d", resp.StatusCode)
	}

	var pr koshaProcessResp
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("[kosha] response parse: %w", err)
	}
	if pr.Code != 0 {
		return nil, fmt.Errorf("[kosha] process.do code=%d msg=%q", pr.Code, pr.Message)
	}

	posts := buildKoshaPosts(pr.Response.PstNoGrid, pr.Response.BbsPstGrid,
		pr.Response.TotalNormalCnt, koshaSource, koshaDetailURL)
	log.Printf("[kosha] %d posts emitted (%d pstNoGrid, %d bbsPstGrid, totalNormal=%d)",
		len(posts), len(pr.Response.PstNoGrid), len(pr.Response.BbsPstGrid), pr.Response.TotalNormalCnt)
	return posts, nil
}

// buildKoshaPosts joins the pstNoGrid display order with bbsPstGrid titles. It
// is a pure function so its behavior can be locked in by kosha_notice_test.go.
//
// Mapping rules:
//   - pstNoGrid mixes stickies (totalCount=1) and regular posts
//     (totalCount=totalNormalCnt). Stickies are pinned notices with no
//     sequential number, so they are dropped — matching the pre-SPA crawler,
//     which filtered the DOM's non-numeric "공지" rows.
//   - PostID is the board's visible "No" = mainTotal - rnum + 1. This MUST stay
//     stable: the framework dedups on the exact (source, PostID) pair and
//     bot_seen already holds these display numbers from the chromedp-era
//     crawler. Emitting pstNo instead would make every backlog item look new
//     and redispatch it.
//   - A regular pstNo with no matching title in bbsPstGrid is dropped with a
//     WARN rather than emitted with an empty title.
func buildKoshaPosts(pstNoGrid []koshaPstNoItem, bbsPstGrid []koshaBbsPstItem, totalNormalCnt int, source, detailURL string) []Post {
	titles := make(map[string]string, len(bbsPstGrid))
	for _, b := range bbsPstGrid {
		titles[b.PstNo] = strings.TrimSpace(b.PstNm)
	}

	// mainTotal is the regular group's totalCount. totalNormalCnt is the
	// authoritative value; fall back to the largest totalCount seen in the
	// grid in case the envelope ever omits it.
	mainTotal := totalNormalCnt
	for _, e := range pstNoGrid {
		if e.TotalCount > mainTotal {
			mainTotal = e.TotalCount
		}
	}
	if mainTotal < 1 {
		return nil
	}

	var posts []Post
	for _, e := range pstNoGrid {
		if e.TotalCount != mainTotal {
			continue // sticky / pinned notice — no sequential "No"
		}
		displayNo := mainTotal - e.Rnum + 1
		if displayNo < 1 {
			log.Printf("[kosha] WARN pstNo=%s rnum=%d yields display=%d -- skipping",
				e.PstNo, e.Rnum, displayNo)
			continue
		}
		title, ok := titles[e.PstNo]
		if !ok || title == "" {
			log.Printf("[kosha] WARN pstNo=%s (No%d) missing title in bbsPstGrid -- skipping",
				e.PstNo, displayNo)
			continue
		}
		posts = append(posts, Post{
			PostID: strconv.Itoa(displayNo),
			Title:  title,
			URL:    detailURL + e.PstNo,
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
