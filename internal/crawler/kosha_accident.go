package crawler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"
)

const (
	accidentListURL = "https://portal.kosha.or.kr/archive/imprtnDsstrAlrame/CSADV50000/CSADV50000M01"
	apiKeyword      = "selectImprtnDsstrSirnList"
	// accidentAPIWait is how long we'll wait for the API call to finish loading
	// after the page navigates. Replaces the previous fixed chromedp.Sleep(2s)
	// which raced the network -- if the response took longer than 2s, the
	// requestID was never captured and the crawler reported "API response not
	// captured". Now we wait on an actual EventLoadingFinished signal.
	accidentAPIWait = 20 * time.Second
	// accidentWSTimeout is how long chromedp waits for Chrome to print its
	// "DevTools listening on ws://..." line. chromedp's library default is
	// 20s; GitHub Actions' shared runners occasionally need longer to bring
	// Chrome up under CPU/IO contention. A 20s miss surfaces as
	// "navigate failed: websocket url timeout reached" and drops the whole
	// accident source for that poll cycle (observed 2026-05-22).
	accidentWSTimeout = 40 * time.Second
	// accidentSessionTimeout bounds one browser session: WS startup headroom
	// + navigate + accidentAPIWait + response-body fetch.
	accidentSessionTimeout = 75 * time.Second
	// accidentMaxAttempts retries the whole chromedp session so a one-off
	// Chrome startup hang or navigate flake does not silently skip the
	// source, mirroring the 3x retry in moel.go.
	accidentMaxAttempts = 3
)

// KoshaAccidentCrawler crawls 중대재해 사이렌 using chromedp + CDP network capture.
type KoshaAccidentCrawler struct {
	BaseCrawler
}

func NewKoshaAccidentCrawler() *KoshaAccidentCrawler {
	return &KoshaAccidentCrawler{BaseCrawler{Name: "kosha_accident"}}
}

// accidentAPIResponse represents the API response structure.
type accidentAPIResponse struct {
	Payload struct {
		ImprtnDsstrSirnList []struct {
			ImprtnDsstrSirnNo int    `json:"imprtnDsstrSirnNo"`
			ImprtnDsstrSirnNm string `json:"imprtnDsstrSirnNm"`
			ImgSrc            string `json:"imgSrc"`
		} `json:"imprtnDsstrSirnList"`
	} `json:"payload"`
}

// FetchPosts retries the chromedp session so a transient Chrome startup
// hang ("websocket url timeout reached") or navigate flake on a shared CI
// runner does not drop the whole accident source for a poll cycle.
func (c *KoshaAccidentCrawler) FetchPosts() ([]Post, error) {
	var lastErr error
	for attempt := 1; attempt <= accidentMaxAttempts; attempt++ {
		posts, err := c.fetchPostsOnce()
		if err == nil {
			return posts, nil
		}
		lastErr = err
		log.Printf("[kosha_accident] attempt %d/%d failed: %v", attempt, accidentMaxAttempts, err)
		if attempt < accidentMaxAttempts {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
	}
	return nil, fmt.Errorf("[kosha_accident] failed after %d attempts: %w", accidentMaxAttempts, lastErr)
}

func (c *KoshaAccidentCrawler) fetchPostsOnce() ([]Post, error) {
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
		chromedp.WSURLReadTimeout(accidentWSTimeout),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, accidentSessionTimeout)
	defer cancel()

	// Intercept the API call at the Response stage. Pausing the request pins
	// its body until we read it, so GetResponseBody no longer races Chrome's
	// inspector cache. The previous network.GetResponseBody hit "Request
	// content was evicted from inspector cache (-32000)" because this payload
	// is large (base64 images inline) and Chrome evicted it before the
	// deferred fetch could run.
	paused := make(chan fetch.RequestID, 1)
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok || !strings.Contains(e.Request.URL, apiKeyword) {
			return
		}
		select {
		case paused <- e.RequestID:
		default:
		}
	})

	if err := chromedp.Run(ctx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{URLPattern: "*" + apiKeyword + "*", RequestStage: fetch.RequestStageResponse},
		}),
		chromedp.Navigate(accidentListURL),
	); err != nil {
		return nil, fmt.Errorf("[kosha_accident] navigate failed: %w", err)
	}

	// Wait for the intercepted API response.
	var reqID fetch.RequestID
	select {
	case reqID = <-paused:
	case <-time.After(accidentAPIWait):
		return nil, fmt.Errorf("[kosha_accident] timed out (%s) waiting for %s response", accidentAPIWait, apiKeyword)
	case <-ctx.Done():
		return nil, fmt.Errorf("[kosha_accident] context cancelled while waiting for API: %w", ctx.Err())
	}

	var capturedBody []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		body, err := fetch.GetResponseBody(reqID).Do(ctx)
		if err != nil {
			return err
		}
		capturedBody = body
		// Release the paused request so the page can settle.
		_ = fetch.ContinueRequest(reqID).Do(ctx)
		return nil
	})); err != nil {
		return nil, fmt.Errorf("[kosha_accident] get response body failed: %w", err)
	}

	// Parse the API response
	var apiResp accidentAPIResponse
	if err := json.Unmarshal(capturedBody, &apiResp); err != nil {
		return nil, fmt.Errorf("[kosha_accident] JSON parse failed: %w", err)
	}

	items := apiResp.Payload.ImprtnDsstrSirnList
	if len(items) == 0 {
		log.Printf("[kosha_accident] no items in API response")
		return nil, nil
	}

	var posts []Post
	for _, item := range items {
		no := fmt.Sprintf("%d", item.ImprtnDsstrSirnNo)
		title := item.ImprtnDsstrSirnNm

		var imgBytes []byte
		imgSrc := item.ImgSrc
		if imgSrc != "" {
			// Strip "data:image/jpg;base64," prefix
			if idx := strings.Index(imgSrc, ","); idx >= 0 {
				imgSrc = imgSrc[idx+1:]
			}
			decoded, err := base64.StdEncoding.DecodeString(imgSrc)
			if err != nil {
				log.Printf("[kosha_accident] image decode failed for %s: %v", no, err)
			} else {
				imgBytes = decoded
			}
		}

		posts = append(posts, Post{
			PostID:    no,
			Title:     title,
			URL:       accidentListURL,
			Source:    "중대재해 사이렌", // 중대재해 사이렌
			ImageData: imgBytes,
		})
	}

	log.Printf("[kosha_accident] %d posts parsed", len(posts))
	return posts, nil
}

func (c *KoshaAccidentCrawler) GetNewPosts() ([]Post, error) {
	posts, err := c.FetchPosts()
	if err != nil {
		return nil, err
	}
	return FilterNewPosts(c.Name, posts), nil
}
