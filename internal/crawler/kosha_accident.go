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
			ImgSrc             string `json:"imgSrc"`
		} `json:"imprtnDsstrSirnList"`
	} `json:"payload"`
}

func (c *KoshaAccidentCrawler) FetchPosts() ([]Post, error) {
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

	// Capture network responses
	var mu sync.Mutex
	var capturedBody string
	var capturedRequestID network.RequestID

	// Enable network events and listen for the API response
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			if strings.Contains(e.Response.URL, apiKeyword) {
				mu.Lock()
				capturedRequestID = e.RequestID
				mu.Unlock()
			}
		case *network.EventLoadingFinished:
			mu.Lock()
			rid := capturedRequestID
			mu.Unlock()
			if rid != "" && e.RequestID == rid {
				// We'll fetch the body after navigation completes
			}
		}
	})

	// Navigate and wait for content
	err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(accidentListURL),
		chromedp.WaitVisible("a.subject", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second), // Allow network responses to complete
	)
	if err != nil {
		return nil, fmt.Errorf("[kosha_accident] chromedp failed: %w", err)
	}

	// Get the captured request body
	mu.Lock()
	rid := capturedRequestID
	mu.Unlock()

	if rid == "" {
		return nil, fmt.Errorf("[kosha_accident] API response not captured")
	}

	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		body, err := network.GetResponseBody(rid).Do(ctx)
		if err != nil {
			return err
		}
		capturedBody = string(body)
		return nil
	}))
	if err != nil {
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
			Source:     "\uC911\uB300\uC7AC\uD574 \uC0AC\uC774\uB80C", // 중대재해 사이렌
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
