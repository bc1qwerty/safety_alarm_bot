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
	"strings"
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

// LogPush sends a log entry to /logs/push.
// No-op if NOTIFICATION_HUB_URL / NOTIFICATION_SECRET is unset.
func LogPush(source, level, message, details string) error {
	hubURL := os.Getenv("NOTIFICATION_HUB_URL")
	secret := os.Getenv("NOTIFICATION_SECRET")
	if hubURL == "" || secret == "" {
		return nil
	}

	logURL := strings.Replace(hubURL, "/notifications/push", "/logs/push", 1)

	// ⚠fmt %q 수제 조립 금지: Go 의 \x1b 류 이스케이프는 JSON 으로는 불법이라,
	// 제어문자가 든 에러 메시지를 허브가 400 으로 조용히 버렸다.
	body, err := json.Marshal(struct {
		Source  string `json:"source"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Details string `json:"details"`
	}{source, level, message, details})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", logURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Notification-Secret", secret)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("logpush status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
