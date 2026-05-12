package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bc1qwerty/safety-alarm-bot/internal/config"
	"github.com/bc1qwerty/safety-alarm-bot/internal/crawler"
	"github.com/bc1qwerty/safety-alarm-bot/internal/notifyhub"
	"github.com/bc1qwerty/safety-alarm-bot/internal/source"
	"github.com/bc1qwerty/txid-bot-framework/pkg/bot"
	"github.com/bc1qwerty/txid-bot-framework/pkg/core"
	"github.com/bc1qwerty/txid-bot-framework/pkg/notify"
	"github.com/bc1qwerty/txid-bot-framework/pkg/store"
)

const (
	runTimeout      = 5 * time.Minute
	maxSendPerRun   = 10
)

// SafetyFormatter renders a safety notice as Telegram HTML and provides
// a plain-text variant for channels (Naver Band) that cannot parse HTML.
type SafetyFormatter struct{}

func (f *SafetyFormatter) Format(item core.Item) core.Message {
	htmlText := fmt.Sprintf("📢 <b>[%s]</b> 새 공지사항\n\n• <a href=\"%s\">%s</a>",
		html.EscapeString(item.Category),
		html.EscapeString(item.URL),
		html.EscapeString(item.Title))

	plain := fmt.Sprintf("[%s] 새 공지사항\n\n• %s\n  %s",
		item.Category, item.Title, item.URL)

	return core.Message{
		Text:      htmlText,
		PlainText: plain,
		ParseMode: "HTML",
	}
}

// shouldRun applies the SAFETY_ALARM_ONLY / SAFETY_ALARM_SKIP env policy
// so operators can disable a flaky source or pin a deploy to a single
// crawler without redeploying. ONLY takes precedence over SKIP.
func shouldRun(name string) bool {
	if only := strings.TrimSpace(os.Getenv("SAFETY_ALARM_ONLY")); only != "" {
		for _, s := range strings.Split(only, ",") {
			if strings.TrimSpace(s) == name {
				return true
			}
		}
		return false
	}
	if skip := strings.TrimSpace(os.Getenv("SAFETY_ALARM_SKIP")); skip != "" {
		for _, s := range strings.Split(skip, ",") {
			if strings.TrimSpace(s) == name {
				return false
			}
		}
	}
	return true
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	log.Println("=== Safety Alarm Bot (Framework Mode) starting ===")
	_ = notifyhub.LogPush("safety-alarm-bot", "info", "run started", "")

	projectRoot := resolveProjectRoot()
	config.InitWithRoot(projectRoot)

	if only := os.Getenv("SAFETY_ALARM_ONLY"); only != "" {
		log.Printf("Filter: ONLY=%s", only)
	}
	if skip := os.Getenv("SAFETY_ALARM_SKIP"); skip != "" {
		log.Printf("Filter: SKIP=%s", skip)
	}

	// Apply ONLY/SKIP filter to the crawler list before adapting.
	allCrawlers := []crawler.Crawler{
		crawler.NewKoshaNoticeCrawler(),
		crawler.NewKoshaAccidentCrawler(),
		crawler.NewKoshaArchiveCrawler("ops"),
		crawler.NewKoshaArchiveCrawler("video"),
		crawler.NewKoshaArchiveCrawler("booklet"),
		crawler.NewKoshaEbookCrawler(),
		crawler.NewMoelCrawler(),
	}
	var crawlers []crawler.Crawler
	for _, c := range allCrawlers {
		if shouldRun(c.SiteName()) {
			crawlers = append(crawlers, c)
			continue
		}
		log.Printf("skipping crawler: %s", c.SiteName())
	}
	if len(crawlers) == 0 {
		log.Println("no crawlers selected — nothing to do")
		return
	}

	var sources []core.Source
	for _, c := range crawlers {
		sources = append(sources, source.NewAdapter(c))
	}
	multiSource := core.NewMultiSource(sources...)

	// Multi-channel notifier — Telegram (HTML) and Band (plain-text).
	// MultiNotifier reports success if at least one channel delivered,
	// so a Band outage no longer causes Telegram duplicates next poll.
	var notifiers []core.Notifier
	if config.TelegramBotToken != "" {
		tg, err := notify.NewTelegram(config.TelegramBotToken)
		if err != nil {
			log.Fatalf("Telegram init: %v", err)
		}
		notifiers = append(notifiers, tg)
	}
	if config.BandAccessToken != "" && config.BandKey != "" {
		notifiers = append(notifiers, notify.NewBand(config.BandAccessToken, config.BandKey))
	}
	if len(notifiers) == 0 {
		log.Fatal("no notifier configured (need TELEGRAM_BOT_TOKEN or BAND_ACCESS_TOKEN)")
	}
	multiNotifier := core.NewMultiNotifier(notifiers...)

	dbPath := filepath.Join(projectRoot, "data", "safety-alarm.db")
	st, err := store.Open(dbPath, "safety-alarm")
	if err != nil {
		log.Fatalf("framework store open: %v", err)
	}
	if config.TelegramChatID != "" {
		_ = st.Subscribe(config.TelegramChatID)
	}

	runner := bot.New(bot.Config{
		Name:            "safety-alarm",
		Source:          multiSource,
		Formatter:       &SafetyFormatter{},
		Notifier:        multiNotifier,
		Store:           st,
		ArchiveDir:      archiveDir(projectRoot),
		HeartbeatDir:    heartbeatDir(),
		MaxItemsPerPoll: maxSendPerRun,
		BootstrapMode:   os.Getenv("BOOTSTRAP_DEDUP") == "1",
		OnNewItem: func(ctx context.Context, item core.Item) error {
			return notifyhub.Push(notifyhub.Payload{
				ChannelID: config.TelegramChatID,
				Title:     item.Title,
				URL:       item.URL,
				Category:  item.Category,
			})
		},
		OnError: func(err error) {
			_ = notifyhub.LogPush("safety-alarm-bot", "error", err.Error(), "")
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	runner.PollOnce(ctx)

	_ = notifyhub.LogPush("safety-alarm-bot", "info", "run finished", "")
	log.Println("=== Safety Alarm Bot run complete ===")
}

func resolveProjectRoot() string {
	if wd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(wd, "data")); err == nil {
			return wd
		}
	}
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func archiveDir(baseDir string) string {
	if v := os.Getenv("ARCHIVE_DIR"); v != "" {
		return v
	}
	return filepath.Join(baseDir, "data", "archive")
}

func heartbeatDir() string {
	if v := os.Getenv("HEARTBEAT_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".txid-bots", "heartbeats")
}
