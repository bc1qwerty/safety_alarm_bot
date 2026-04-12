// Package notifyhub sends notifications to the txid notification hub.
// If NOTIFICATION_HUB_URL or NOTIFICATION_SECRET is not set, Push is a no-op.
package notifyhub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Payload struct {
	ChannelID string `json:"channelId"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	URL       string `json:"url,omitempty"`
	Category  string `json:"category,omitempty"`
	ImageURL  string `json:"imageUrl,omitempty"`
}

var client = &http.Client{Timeout: 10 * time.Second}

// Push sends a notification to the hub.
// Returns nil if the hub is not configured (silent no-op).
func Push(p Payload) error {
	hubURL := os.Getenv("NOTIFICATION_HUB_URL")
	secret := os.Getenv("NOTIFICATION_SECRET")
	if hubURL == "" || secret == "" {
		return nil
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", hubURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notification-Secret", secret)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("push status %d: %s", resp.StatusCode, body)
	}
	return nil
}
