package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bc1qwerty/safety-alarm-bot/internal/config"
	"github.com/bc1qwerty/safety-alarm-bot/internal/crawler"
	"github.com/bc1qwerty/safety-alarm-bot/internal/notifier"
	"github.com/bc1qwerty/safety-alarm-bot/internal/notifyhub"
)

// formatMessage builds the HTML-mode Telegram batch text. Telegram parses
// the body as HTML when parse_mode=HTML, so every user-controlled string
// (source, title, URL) MUST be escaped or a stray '<', '>' or '&' in a
// title aborts the send with a 400 from Telegram.
func formatMessage(posts []crawler.Post, source string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("\U0001f4e2 [%s] 새 공지사항 %d건\n", html.EscapeString(source), len(posts)))
	for _, p := range posts {
		lines = append(lines, fmt.Sprintf("• <a href=\"%s\">%s</a>", html.EscapeString(p.URL), html.EscapeString(p.Title)))
	}
	return strings.Join(lines, "\n")
}

func formatBandMessage(posts []crawler.Post, source string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("[%s] 새 공지사항 %d건\n", source, len(posts)))
	for _, p := range posts {
		lines = append(lines, fmt.Sprintf("• %s\n  %s", p.Title, p.URL))
	}
	return strings.Join(lines, "\n")
}

// shouldRun returns true when the given source should be executed in this run.
// Controlled by SAFETY_ALARM_ONLY / SAFETY_ALARM_SKIP env vars (comma separated).
// ONLY takes precedence over SKIP. When neither is set, all sources run.
func shouldRun(source string) bool {
	only := strings.TrimSpace(os.Getenv("SAFETY_ALARM_ONLY"))
	if only != "" {
		for _, s := range strings.Split(only, ",") {
			if strings.TrimSpace(s) == source {
				return true
			}
		}
		return false
	}
	skip := strings.TrimSpace(os.Getenv("SAFETY_ALARM_SKIP"))
	if skip != "" {
		for _, s := range strings.Split(skip, ",") {
			if strings.TrimSpace(s) == source {
				return false
			}
		}
	}
	return true
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime)

	// Determine project root from executable location or working directory
	exe, err := os.Executable()
	if err == nil {
		projectRoot := filepath.Dir(exe)
		// Check if data dir exists relative to executable
		if _, err := os.Stat(filepath.Join(projectRoot, "data")); err != nil {
			// Fall back to working directory
			projectRoot, _ = os.Getwd()
		}
		config.InitWithRoot(projectRoot)
	} else {
		config.Init()
	}

	log.Println("=== Safety Alarm Bot started ===")
	if only := os.Getenv("SAFETY_ALARM_ONLY"); only != "" {
		log.Printf("Filter: ONLY=%s", only)
	}
	if skip := os.Getenv("SAFETY_ALARM_SKIP"); skip != "" {
		log.Printf("Filter: SKIP=%s", skip)
	}
	notifyhub.LogPush("safety-alarm-bot", "info", "run started", "")

	totalNew := 0

	// State advancement policy:
	//
	// FilterNewPosts no longer auto-saves lastID. State only advances after
	// a successful Telegram delivery. This means a transient Telegram outage
	// turns into a re-send next run (acceptable duplicates) instead of
	// permanent silence on those posts. Band is best-effort and does not
	// gate state. notifyhub is also best-effort.

	// 1) Notice crawlers (batch send) — filtered by shouldRun
	type noticeEntry struct {
		source  string
		crawler crawler.Crawler
	}
	noticeCrawlers := []noticeEntry{
		{"moel", crawler.NewMoelCrawler()},
		{"kosha_notice", crawler.NewKoshaNoticeCrawler()},
	}

	for _, entry := range noticeCrawlers {
		if !shouldRun(entry.source) {
			log.Printf("[%s] skipped (filter)", entry.source)
			continue
		}
		c := entry.crawler
		newPosts, err := c.GetNewPosts()
		if err != nil {
			log.Printf("[%s] crawl error: %v", c.SiteName(), err)
			continue
		}
		if len(newPosts) == 0 {
			continue
		}

		totalNew += len(newPosts)

		tgMsg := formatMessage(newPosts, newPosts[0].Source)
		tgOk := notifier.TelegramSendMessage(tgMsg)

		bandMsg := formatBandMessage(newPosts, newPosts[0].Source)
		notifier.BandSendPost(bandMsg) // best effort, does not gate state

		// Push each notice to hub individually (best effort)
		for _, p := range newPosts {
			if err := notifyhub.Push(notifyhub.Payload{
				ChannelID: "safety-alarm",
				Title:     p.Title,
				Body:      p.Source,
				URL:       p.URL,
				Category:  p.Source,
			}); err != nil {
				log.Printf("hub push error: %v", err)
			}
		}

		if tgOk {
			// newPosts is newest-first; advance cursor to the newest post.
			crawler.SaveLastID(c.SiteName(), newPosts[0].PostID)
		} else {
			log.Printf("[%s] Telegram send failed; state NOT advanced — batch will retry next run", c.SiteName())
		}
	}

	// 2) Accident crawler (individual send with image)
	if shouldRun("kosha_accident") {
		accidentCrawler := crawler.NewKoshaAccidentCrawler()
		newAccidents, err := accidentCrawler.GetNewPosts()
		if err != nil {
			log.Printf("[kosha_accident] crawl error: %v", err)
			newAccidents = nil
		}

		// Send oldest first; advance state per success, stop at first failure.
		for i := len(newAccidents) - 1; i >= 0; i-- {
			post := newAccidents[i]
			totalNew++
			sent := true
			if post.ImageData != nil {
				sent = notifier.TelegramSendPhoto(post.ImageData, "")
			} else {
				log.Printf("[kosha_accident] no image: %s -- skipping but advancing state", post.Title)
			}
			if !sent {
				log.Printf("[kosha_accident] Telegram failed for #%s; batch stopped, will retry next run", post.PostID)
				break
			}
			crawler.SaveLastID("kosha_accident", post.PostID)
		}
	} else {
		log.Printf("[kosha_accident] skipped (filter)")
	}

	// 3) Archive crawlers (OPS/booklet/video, individual send)
	archiveTypes := []string{"ops", "booklet", "video"}
	for _, ct := range archiveTypes {
		if !shouldRun("kosha_archive_" + ct) {
			log.Printf("[kosha_archive_%s] skipped (filter)", ct)
			continue
		}
		c := crawler.NewKoshaArchiveCrawler(ct)
		newPosts, err := c.GetNewPosts()
		if err != nil {
			log.Printf("[%s] crawl error: %v", c.SiteName(), err)
			continue
		}

		// Send oldest first; advance state per success, stop at first failure.
		for i := len(newPosts) - 1; i >= 0; i-- {
			post := newPosts[i]
			totalNew++
			var sent bool
			if post.ImageData != nil {
				sent = notifier.TelegramSendPhoto(post.ImageData, post.Title)
			} else if post.FileData != nil && post.FileName != "" {
				sent = notifier.TelegramSendDocument(post.FileData, post.FileName, post.Title)
			} else {
				// Text message for items without files (e.g., video links).
				// Plain text mode -- no HTML escape needed.
				msg := fmt.Sprintf("\U0001f4f9 [%s]\n%s\n%s", post.Source, post.Title, post.URL)
				sent = notifier.TelegramSendMessage(msg)
			}
			if !sent {
				log.Printf("[%s] Telegram failed for #%s; batch stopped, will retry next run", c.SiteName(), post.PostID)
				break
			}
			crawler.SaveLastID(c.SiteName(), post.PostID)
		}
	}

	// 4) eBook crawler (PDF, individual send)
	if !shouldRun("kosha_ebook") {
		log.Printf("[kosha_ebook] skipped (filter)")
		log.Printf("=== Done: %d new notice(s) total ===", totalNew)
		return
	}
	ebookCrawler := crawler.NewKoshaEbookCrawler()
	newEbooks, err := ebookCrawler.GetNewPosts()
	if err != nil {
		log.Printf("[kosha_ebook] crawl error: %v", err)
		newEbooks = nil
	}

	// Send oldest first; advance state per success, stop at first failure.
	for i := len(newEbooks) - 1; i >= 0; i-- {
		post := newEbooks[i]
		totalNew++
		var sent bool
		if post.FileData != nil && post.FileName != "" {
			sent = notifier.TelegramSendDocument(post.FileData, post.FileName, post.Title)
		} else {
			// eBook viewer link + PDF download links. HTML-escape user content
			// because parse_mode=HTML is set.
			var lines []string
			lines = append(lines, fmt.Sprintf("\U0001f4d6 [%s] %s\n", html.EscapeString(post.Source), html.EscapeString(post.Title)))
			lines = append(lines, fmt.Sprintf("\U0001f4d6 e-Book 보기\n%s", html.EscapeString(post.URL)))
			for _, dl := range post.DownloadURLs {
				lines = append(lines, fmt.Sprintf("\U0001f4e5 <a href=\"%s\">[다운로드] %s</a>",
					html.EscapeString(dl.URL), html.EscapeString(dl.Label)))
			}
			sent = notifier.TelegramSendMessage(strings.Join(lines, "\n"))
		}
		if !sent {
			log.Printf("[kosha_ebook] Telegram failed for #%s; batch stopped, will retry next run", post.PostID)
			break
		}
		crawler.SaveLastID("kosha_ebook", post.PostID)
	}

	log.Printf("=== Done: %d new notice(s) total ===", totalNew)
}
