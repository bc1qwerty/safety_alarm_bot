package notifier

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/bc1qwerty/safety-alarm-bot/internal/config"
)

const bandPostURL = "https://openapi.band.us/v2.2/band/post/create"

// BandSendPost creates a post on Naver Band.
func BandSendPost(content string) bool {
	if config.BandAccessToken == "" || config.BandKey == "" {
		log.Println("[band] ACCESS_TOKEN or BAND_KEY not set")
		return false
	}

	params := url.Values{
		"access_token": {config.BandAccessToken},
		"band_key":     {config.BandKey},
		"content":      {content},
		"do_push":      {"true"},
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(bandPostURL, params)
	if err != nil {
		log.Printf("[band] send failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		ResultCode int `json:"result_code"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[band] response parse failed: %v", err)
		return false
	}

	if result.ResultCode == 1 {
		log.Println("[band] post created")
		return true
	}

	log.Printf("[band] API error: %s", string(body))
	return false
}
