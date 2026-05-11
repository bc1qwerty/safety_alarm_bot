package crawler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
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

func (c *KoshaAccidentCrawler) FetchPosts() ([]Post, error) {
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

	ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	// Track every request whose URL contains apiKeyword (in case the page
	// fires more than one). Signal respDone the moment any of them finishes
	// loading -- that request ID is the one we'll fetch the body from.
	var pendingMu sync.Mutex
	pending := make(map[network.RequestID]bool)
	respDone := make(chan network.RequestID, 1)

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			if strings.Contains(e.Response.URL, apiKeyword) {
				pendingMu.Lock()
				pending[e.RequestID] = true
				pendingMu.Unlock()
			}
		case *network.EventLoadingFinished:
			pendingMu.Lock()
			matched := pending[e.RequestID]
			if matched {
				delete(pending, e.RequestID)
			}
			pendingMu.Unlock()
			if matched {
				select {
				case respDone <- e.RequestID:
				default:
				}
			}
		}
	})

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(accidentListURL),
	); err != nil {
		return nil, fmt.Errorf("[kosha_accident] navigate failed: %w", err)
	}

	// Wait for the API response to finish loading.
	var capturedRequestID network.RequestID
	select {
	case capturedRequestID = <-respDone:
	case <-time.After(accidentAPIWait):
		return nil, fmt.Errorf("[kosha_accident] timed out (%s) waiting for %s response", accidentAPIWait, apiKeyword)
	case <-ctx.Done():
		return nil, fmt.Errorf("[kosha_accident] context cancelled while waiting for API: %w", ctx.Err())
	}

	var capturedBody string
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		body, err := network.GetResponseBody(capturedRequestID).Do(ctx)
		if err != nil {
			return err
		}
		capturedBody = string(body)
		return nil
	})); err != nil {
		return nil, fmt.Errorf("[kosha_accident] get response body failed: %w", err)
	}

	// Parse the API response
	var apiResp accidentAPIResponse
	if err := json.Unmarshal([]byte(capturedBody), &apiResp); err != nil {
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
