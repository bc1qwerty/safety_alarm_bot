package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bc1qwerty/safety-alarm-bot/internal/config"
	"github.com/bc1qwerty/safety-alarm-bot/internal/crawler"
	"github.com/bc1qwerty/safety-alarm-bot/internal/notifier"
	"github.com/bc1qwerty/safety-alarm-bot/internal/notifyhub"
)

func formatMessage(posts []crawler.Post, source string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("\U0001f4e2 [%s] \uc0c8 \uacf5\uc9c0\uc0ac\ud56d %d\uac74\n", source, len(posts)))
	for _, p := range posts {
		lines = append(lines, fmt.Sprintf("\u2022 <a href=\"%s\">%s</a>", p.URL, p.Title))
	}
	return strings.Join(lines, "\n")
}

func formatBandMessage(posts []crawler.Post, source string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("[%s] \uc0c8 \uacf5\uc9c0\uc0ac\ud56d %d\uac74\n", source, len(posts)))
	for _, p := range posts {
		lines = append(lines, fmt.Sprintf("\u2022 %s\n  %s", p.Title, p.URL))
	}
	return strings.Join(lines, "\n")
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
	notifyhub.LogPush("safety-alarm-bot", "info", "run started", "")

	totalNew := 0

	// 1) Notice crawlers (batch send)
	noticeCrawlers := []crawler.Crawler{
		crawler.NewMoelCrawler(),
		crawler.NewKoshaNoticeCrawler(),
	}

	for _, c := range noticeCrawlers {
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
		notifier.TelegramSendMessage(tgMsg)

		bandMsg := formatBandMessage(newPosts, newPosts[0].Source)
		notifier.BandSendPost(bandMsg)

		// Push each notice to hub individually
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
	}

	// 2) Accident crawler (individual send with image)
	accidentCrawler := crawler.NewKoshaAccidentCrawler()
	newAccidents, err := accidentCrawler.GetNewPosts()
	if err != nil {
		log.Printf("[kosha_accident] crawl error: %v", err)
		newAccidents = nil
	}

	// Send oldest first
	for i := len(newAccidents) - 1; i >= 0; i-- {
		post := newAccidents[i]
		totalNew++
		if post.ImageData != nil {
			notifier.TelegramSendPhoto(post.ImageData, "")
		} else {
			log.Printf("[kosha_accident] no image: %s", post.Title)
		}
	}

	// 3) Archive crawlers (OPS/booklet/video, individual send)
	archiveTypes := []string{"ops", "booklet", "video"}
	for _, ct := range archiveTypes {
		c := crawler.NewKoshaArchiveCrawler(ct)
		newPosts, err := c.GetNewPosts()
		if err != nil {
			log.Printf("[%s] crawl error: %v", c.SiteName(), err)
			continue
		}

		// Send oldest first
		for i := len(newPosts) - 1; i >= 0; i-- {
			post := newPosts[i]
			totalNew++
			if post.ImageData != nil {
				notifier.TelegramSendPhoto(post.ImageData, post.Title)
			} else if post.FileData != nil && post.FileName != "" {
				notifier.TelegramSendDocument(post.FileData, post.FileName, post.Title)
			} else {
				// Text message for items without files (e.g., video links)
				msg := fmt.Sprintf("\U0001f4f9 [%s]\n%s\n%s", post.Source, post.Title, post.URL)
				notifier.TelegramSendMessage(msg)
			}
		}
	}

	// 4) eBook crawler (PDF, individual send)
	ebookCrawler := crawler.NewKoshaEbookCrawler()
	newEbooks, err := ebookCrawler.GetNewPosts()
	if err != nil {
		log.Printf("[kosha_ebook] crawl error: %v", err)
		newEbooks = nil
	}

	// Send oldest first
	for i := len(newEbooks) - 1; i >= 0; i-- {
		post := newEbooks[i]
		totalNew++
		if post.FileData != nil && post.FileName != "" {
			notifier.TelegramSendDocument(post.FileData, post.FileName, post.Title)
		} else {
			// eBook viewer link + PDF download links
			var lines []string
			lines = append(lines, fmt.Sprintf("\U0001f4d6 [%s] %s\n", post.Source, post.Title))
			lines = append(lines, fmt.Sprintf("\U0001f4d6 e-Book \ubcf4\uae30\n%s", post.URL))
			for _, dl := range post.DownloadURLs {
				lines = append(lines, fmt.Sprintf("\U0001f4e5 <a href=\"%s\">[\ub2e4\uc6b4\ub85c\ub4dc] %s</a>", dl.URL, dl.Label))
			}
			notifier.TelegramSendMessage(strings.Join(lines, "\n"))
		}
	}

	log.Printf("=== Done: %d new notice(s) total ===", totalNew)
}
