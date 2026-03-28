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

func tgAPIURL(method string) string {
	return fmt.Sprintf("https://api.telegram.org/bot%s/%s", config.TelegramBotToken, method)
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(tgAPIURL("sendMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[telegram] send failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[telegram] send failed: HTTP %d: %s", resp.StatusCode, string(respBody))
		return false
	}

	log.Println("[telegram] message sent")
	return true
}

// TelegramSendDocument sends a file via Telegram Bot API (sendDocument).
func TelegramSendDocument(docBytes []byte, filename string, caption string) bool {
	if config.TelegramBotToken == "" || config.TelegramChatID == "" {
		log.Println("[telegram] BOT_TOKEN or CHAT_ID not set")
		return false
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	_ = w.WriteField("chat_id", config.TelegramChatID)
	if caption != "" {
		_ = w.WriteField("caption", caption)
	}

	part, err := w.CreateFormFile("document", filename)
	if err != nil {
		log.Printf("[telegram] create form file failed: %v", err)
		return false
	}
	if _, err := part.Write(docBytes); err != nil {
		log.Printf("[telegram] write form file failed: %v", err)
		return false
	}
	w.Close()

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(tgAPIURL("sendDocument"), w.FormDataContentType(), &buf)
	if err != nil {
		log.Printf("[telegram] document send failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[telegram] document send failed: HTTP %d: %s", resp.StatusCode, string(respBody))
		return false
	}

	log.Printf("[telegram] document sent: %s", filename)
	return true
}

// TelegramSendPhoto sends an image as a document (uncompressed) via Telegram Bot API.
func TelegramSendPhoto(photoBytes []byte, caption string) bool {
	if config.TelegramBotToken == "" || config.TelegramChatID == "" {
		log.Println("[telegram] BOT_TOKEN or CHAT_ID not set")
		return false
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	_ = w.WriteField("chat_id", config.TelegramChatID)
	if caption != "" {
		_ = w.WriteField("caption", caption)
		_ = w.WriteField("parse_mode", "HTML")
	}

	part, err := w.CreateFormFile("document", "image.jpg")
	if err != nil {
		log.Printf("[telegram] create form file failed: %v", err)
		return false
	}
	if _, err := part.Write(photoBytes); err != nil {
		log.Printf("[telegram] write form file failed: %v", err)
		return false
	}
	w.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(tgAPIURL("sendDocument"), w.FormDataContentType(), &buf)
	if err != nil {
		log.Printf("[telegram] photo send failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[telegram] photo send failed: HTTP %d: %s", resp.StatusCode, string(respBody))
		return false
	}

	log.Println("[telegram] photo sent (uncompressed)")
	return true
}
