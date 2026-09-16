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
	"github.com/bc1qwerty/txid-bot-framework/pkg/logsafe"
	"github.com/bc1qwerty/txid-bot-framework/pkg/notify"
	"github.com/bc1qwerty/txid-bot-framework/pkg/store"
)

const (
	runTimeout    = 5 * time.Minute
	maxSendPerRun = 10
	// dedupRetain 은 bot_seen/bot_sent 보존기간이다. 프레임워크 기본값 90일은
	// 소스 목록의 수명보다 짧았다: MOEL 공지·KOSHA 자료실은 12개월치를 계속
	// 노출하므로, 90일이 지나 dedup 행이 지워지면 **그대로 남아 있는 목록의
	// 옛 글이 신규로 다시 잡혀 재발송**된다. 2026-09-15 실측으로 터졌다 —
	// 정리가 194행을 지우고 이미 보낸 월간지 백호 9건이 재발송됐다.
	// 보존은 소스 수명보다 길어야 한다. food-recall 이 같은 이유로 2년을 쓴다.
	dedupRetain = 2 * 365 * 24 * time.Hour
	// hubChannel is the txid notification-hub channel slug. It is NOT the
	// Telegram chat id: the hub keys notifications by logical channel,
	// while Telegram/Band delivery is handled separately by the
	// notifiers. It doubles as the framework bot name and store namespace
	// so all three stay in sync.
	hubChannel = "safety-alarm"
)

// SafetyFormatter renders a safety notice as Telegram HTML plus a
// plain-text variant for channels (Naver Band) that can't parse HTML.
// The output is intentionally minimal — title + link only. Any LLM-
// generated commentary has been removed: it kept hallucinating
// project-relevance ("직접 영향 없음 …") on unrelated notices, which
// the channel owner explicitly does not want.
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
		// 중대재해 사이렌 posts are a poster image with the accident details
		// on it; the title alone says almost nothing. Carrying the bytes
		// through makes Telegram render the notice inline instead of
		// forcing a click through to KOSHA's list page. Sources without
		// an image are unaffected — the notifier falls back to text.
		ImageData: item.ImageData,
		ImageName: item.ImageName,
		// 자료실 항목(OPS·소책자)은 본문이 PDF다. 제목만 보내면 매번 KOSHA
		// 목록 페이지를 눌러 들어가야 확인이 된다. 파일을 실어 보내면 채널에서
		// 바로 열리고, 사진이 아니라 문서로 보내므로 텔레그램이 재인코딩해
		// 잔글씨를 뭉개지 않는다.
		FileData: item.FileData,
		FileName: item.FileName,
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

// frameworkRev 는 빌드 시 -ldflags 로 주입된다(CI 의 "Capture framework revision").
// go.mod 의 replace 때문에 빌드정보에는 프레임워크가 (devel) 로만 남아, 이것이 없으면
// 프로드 바이너리가 어느 프레임워크 커밋으로 만들어졌는지 알 방법이 없다.
// 로컬 빌드에서는 "unknown" 이고, 그 자체가 «릴리스본이 아니다» 라는 신호다.
var frameworkRev = "unknown"

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	log.Printf("=== Safety Alarm Bot (Framework Mode) starting === framework=%s", frameworkRev)
	_ = notifyhub.LogPush("safety-alarm-bot", "info", "run started", "")

	projectRoot := resolveProjectRoot()
	config.InitWithRoot(projectRoot)

	if only := os.Getenv("SAFETY_ALARM_ONLY"); only != "" {
		log.Printf("Filter: ONLY=%s", only)
	}
	if skip := os.Getenv("SAFETY_ALARM_SKIP"); skip != "" {
		log.Printf("Filter: SKIP=%s", skip)
	}

	dbPath := filepath.Join(projectRoot, "data", "safety-alarm.db")
	st, err := store.Open(dbPath, hubChannel)
	if err != nil {
		log.Fatalf("framework store open: %v", err)
	}
	// store.Open uses journal_mode=WAL, so committed rows live in the -wal
	// sidecar until a checkpoint folds them into the main db. GHA's cache
	// step only captures data/safety-alarm.db, so without an explicit
	// TRUNCATE checkpoint the restored DB next run is schema-only and the
	// bot redispatches the same kosha_* backlog every 30 minutes.
	defer func() {
		// One-shot runs never reach the daemon-only runCleanup (bot.Run starts
		// it; we only call PollOnce), so prune old dedup rows here with the same
		// dedupRetain the Config below declares — **이 호출이 이 봇의 유일한
		// 정리 경로다.** Config 쪽만 고치면 아무 효과가 없다. Must run *before*
		// the checkpoint — otherwise the DELETEs stay in the -wal sidecar that
		// GHA's cache drops.
		if err := st.Cleanup(dedupRetain); err != nil {
			log.Printf("store cleanup warning: %v", err)
		}
		if db := st.DB(); db != nil {
			if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
				log.Printf("[wal_checkpoint] %v", err)
			}
		}
		_ = st.Close()
	}()
	if config.TelegramChatID != "" {
		_ = st.Subscribe(config.TelegramChatID)
	}

	// seen 은 (site, postID) 가 이미 bot_seen 에 있는지 본다 — 크롤러는
	// 첨부 다운로드를 건너뛰는 데, 어댑터는 kosha 키 전환 심에 쓴다. 조회
	// 실패는 "안 봄"으로 두는데, 그런 항목은 프레임워크의 IsSeen 도 같은
	// 이유로 실패해 dispatch 없이 건너뛰므로 오발송으로 이어지지 않는다.
	seen := func(site, postID string) bool {
		ok, err := st.IsSeen(site, source.ItemID(site, postID))
		return err == nil && ok
	}

	// Apply ONLY/SKIP filter to the crawler list before adapting.
	allCrawlers := []crawler.Crawler{
		crawler.NewKoshaNoticeCrawler(),
		crawler.NewKoshaAccidentCrawler(),
		crawler.NewKoshaArchiveCrawler("ops", seen),
		crawler.NewKoshaArchiveCrawler("video", seen),
		crawler.NewKoshaArchiveCrawler("booklet", seen),
		crawler.NewKoshaEbookCrawler(seen),
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
		sources = append(sources, source.NewAdapter(c, seen))
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
	// 부분 실패(예: 텔레그램만 강퇴되고 밴드는 성공)는 Send 가 nil 을 돌려줘
	// Runner 의 OnError·영구수신자 가드에 절대 닿지 않는다 — 허브 로그로
	// 승격해 표면화한다. 영구 판정(IsPermanentRecipient)은 채널이 스스로
	// 회복하지 못하니 메시지에 구별해 남긴다.
	multiNotifier.OnPartialFailure = func(name string, err error) {
		kind := "일시"
		if core.IsPermanentRecipient(err) {
			kind = "영구"
		}
		_ = notifyhub.LogPush("safety-alarm-bot", "error",
			logsafe.Mask(fmt.Sprintf("채널 부분 실패(%s, %s): %v — 다른 채널은 성공해 재발송 없음", name, kind, err)), "")
	}

	runner := bot.New(bot.Config{
		Name:              hubChannel,
		Source:            multiSource,
		Formatter:         &SafetyFormatter{},
		Notifier:          multiNotifier,
		Store:             st,
		ArchiveDir:        archiveDir(projectRoot),
		HeartbeatDir:      heartbeatDir(),
		MaxItemsPerPoll:   maxSendPerRun,
		RetainDuration:    dedupRetain,
		ArchiveRetainDays: 30,
		BootstrapMode:     os.Getenv("BOOTSTRAP_DEDUP") == "1",
		OnNewItem: func(ctx context.Context, item core.Item) error {
			return notifyhub.Push(notifyhub.Payload{
				ChannelID: hubChannel,
				Title:     item.Title,
				URL:       item.URL,
				Category:  item.Category,
			})
		},
		OnError: func(err error) {
			// 텔레그램 오류는 *url.Error 로 토큰이 든 URL 을 담을 수 있다.
			// 허브로 나가기 전에 토큰 형태를 원천에서 가린다 — 외부 sed
			// 파이프 없이도 평문 토큰이 프로세스 밖으로 안 나가게.
			_ = notifyhub.LogPush("safety-alarm-bot", "error", logsafe.Mask(err.Error()), "")
		},
		OnPollComplete: func(ctx context.Context, n int) error {
			return notifyhub.LogPush("safety-alarm-bot", "info",
				fmt.Sprintf("poll complete (%d items)", n), "")
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
