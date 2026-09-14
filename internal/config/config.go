package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
)

var (
	TelegramBotToken string
	TelegramChatID   string
	BandAccessToken  string
	BandKey          string
)

func Init() {
	// Determine project root from this file's location
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")

	_ = godotenv.Load(filepath.Join(projectRoot, ".env"))

	TelegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	TelegramChatID = os.Getenv("TELEGRAM_CHAT_ID")
	BandAccessToken = os.Getenv("BAND_ACCESS_TOKEN")
	BandKey = os.Getenv("BAND_KEY")
}

// InitWithRoot initializes config using an explicit project root path.
func InitWithRoot(projectRoot string) {
	_ = godotenv.Load(filepath.Join(projectRoot, ".env"))

	TelegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	TelegramChatID = os.Getenv("TELEGRAM_CHAT_ID")
	BandAccessToken = os.Getenv("BAND_ACCESS_TOKEN")
	BandKey = os.Getenv("BAND_KEY")
}
