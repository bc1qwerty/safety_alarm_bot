package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/bc1qwerty/safety-alarm-bot/internal/config"
)

// tgRetryMax is the number of total attempts (1 + retries). Increase only if
// the underlying call is idempotent on the Telegram side -- sendMessage,
// sendPhoto and sendDocument are.
const tgRetryMax = 3

func tgAPIURL(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", config.TelegramBotToken, method)
}

// tgPost POSTs to the Telegram bot API with retries on 429 (Too Many Requests,
// honouring parameters.retry_after) and 5xx. 4xx other than 429 are treated as
// permanent errors and returned immediately so we don't burn the rate budget
// on malformed payloads. Body must be a fresh buffer per call because the
// retry path re-issues bytes.NewReader on it.
//
// kind is a short label for logs (e.g. "message", "photo", "document").
func tgPost(method, contentType string, body []byte, kind string) bool {
	client := &http.Client{Timeout: 60 * time.Second}

	for attempt := 1; attempt <= tgRetryMax; attempt++ {
		resp, err := client.Post(tgAPIURL(method), contentType, bytes.NewReader(body))
		if err != nil {
			log.Printf("[telegram] %s transport error (attempt %d/%d): %v", kind, attempt, tgRetryMax, err)
			if attempt < tgRetryMax {
				time.Sleep(time.Duration(attempt*2) * time.Second)
				continue
			}
			return false
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			log.Printf("[telegram] %s sent (attempt %d)", kind, attempt)
			return true
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := parseRetryAfter(respBody)
			log.Printf("[telegram] %s rate limited, sleeping %ds (attempt %d/%d)", kind, wait, attempt, tgRetryMax)
			if attempt < tgRetryMax {
				time.Sleep(time.Duration(wait) * time.Second)
				continue
			}
		case resp.StatusCode >= 500:
			log.Printf("[telegram] %s HTTP %d (attempt %d/%d): %s", kind, resp.StatusCode, attempt, tgRetryMax, string(respBody))
			if attempt < tgRetryMax {
				time.Sleep(time.Duration(attempt*2) * time.Second)
				continue
			}
		default:
			// 4xx other than 429: malformed request, no point retrying.
			log.Printf("[telegram] %s HTTP %d (permanent): %s", kind, resp.StatusCode, string(respBody))
			return false
		}
	}
	log.Printf("[telegram] %s failed after %d attempts", kind, tgRetryMax)
	return false
}

// parseRetryAfter pulls parameters.retry_after out of a Telegram 429 body and
// clamps it to [1, 60] seconds. Telegram sometimes reports very large values
// (intentional cooldowns); we cap so a single rate-limit burst does not stall
// the whole bot run.
func parseRetryAfter(body []byte) int {
	var r struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.Unmarshal(body, &r)
	w := r.Parameters.RetryAfter
	if w < 1 {
		w = 5
	}
	if w > 60 {
		w = 60
	}
	return w
}

// TelegramSendMessage sends a text message via Telegram Bot API.
func TelegramSendMessage(text string) bool {
	if config.TelegramBotToken == "" || config.TelegramChatID == "" {
		log.Println("[telegram] BOT_TOKEN or CHAT_ID not set")
		return false
	}

	payload := map[string]interface{}{
		"chat_id":                  config.TelegramChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	body, _ := json.Marshal(payload)
	return tgPost("sendMessage", "application/json", body, "message")
}

// TelegramSendDocument sends a file via Telegram Bot API (sendDocument).
func TelegramSendDocument(docBytes []byte, filename string, caption string) bool {
	if config.TelegramBotToken == "" || config.TelegramChatID == "" {
		log.Println("[telegram] BOT_TOKEN or CHAT_ID not set")
		return false
	}

	body, contentType, err := buildMultipart(map[string]string{
		"chat_id": config.TelegramChatID,
		"caption": caption,
	}, "document", filename, docBytes)
	if err != nil {
		log.Printf("[telegram] document build failed: %v", err)
		return false
	}
	return tgPost("sendDocument", contentType, body, "document:"+filename)
}

// TelegramSendPhoto sends an image as a document (uncompressed) via Telegram Bot API.
func TelegramSendPhoto(photoBytes []byte, caption string) bool {
	if config.TelegramBotToken == "" || config.TelegramChatID == "" {
		log.Println("[telegram] BOT_TOKEN or CHAT_ID not set")
		return false
	}

	fields := map[string]string{
		"chat_id": config.TelegramChatID,
		"caption": caption,
	}
	if caption != "" {
		fields["parse_mode"] = "HTML"
	}
	body, contentType, err := buildMultipart(fields, "document", "image.jpg", photoBytes)
	if err != nil {
		log.Printf("[telegram] photo build failed: %v", err)
		return false
	}
	return tgPost("sendDocument", contentType, body, "photo")
}

// buildMultipart returns the request body + content-type for a multipart
// upload. We materialize the whole body as []byte so retries can re-read it.
func buildMultipart(fields map[string]string, fileField, filename string, fileBytes []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	part, err := w.CreateFormFile(fileField, filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}
