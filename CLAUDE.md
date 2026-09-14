# Safety Alarm Bot

## Language
- Respond in Korean (한국어로 응답)

## Description
Construction safety notice crawler and notification bot. Scrapes notices from MOEL (고용노동부) and KOSHA (안전보건공단) including accident reports, archives (OPS/booklets/videos), and eBooks. Sends to Telegram and Naver Band.

## Tech Stack
- **Language**: Go 1.24
- **Scraping**: goquery + chromedp (headless Chrome for JS-rendered pages)
- **Notifications**: Telegram Bot API + Naver Band API (via txid-bot-framework `pkg/notify` + `core.MultiNotifier`)
- **State**: framework SQLite store (data/safety-alarm.db, bot_key namespace `safety-alarm`)
- **Config**: godotenv

## Project Structure
```
main.go                        # Run-once: crawl all sources → send new items → exit
internal/
  config/                      # Env config (Telegram, Band tokens, data paths)
  crawler/
    moel.go                    # 고용노동부 notice crawler
    kosha_notice.go            # KOSHA notice crawler
    kosha_accident.go          # KOSHA accident report crawler (with images, chromedp)
    kosha_archive.go           # KOSHA archive crawler (OPS/booklet/video)
    kosha_ebook.go             # KOSHA eBook crawler (PDF downloads)
    types.go                   # Shared types (Post, DownloadURL)
  source/
    adapter.go                 # Crawler → framework core.Source adapter
  notifyhub/
    client.go                  # txid notification-hub push client
data/                          # Runtime state (safety-alarm.db, framework store)
```
Dedup/delivery state and Telegram/Band sending live in txid-bot-framework
(`pkg/store`, `pkg/notify`); this repo keeps only the crawlers (moel/kosha
split unchanged) and the Source adapter.

## Build & Run
```bash
go build -o safety_alarm_bot .
# Set env vars in .env
./safety_alarm_bot             # runs once, exits
```

## Environment Variables
- `TELEGRAM_BOT_TOKEN` - **Required** Telegram bot token
- `TELEGRAM_CHAT_ID` - **Required** Telegram chat ID
- `BAND_ACCESS_TOKEN` - Naver Band access token
- `BAND_KEY` - Naver Band key

## Deployment
- Designed for cron execution (run-once, not a daemon)
- Requires Chrome/Chromium for chromedp (accident report screenshots)

## Status
Active. Cron-based on acer or dell. Dual-channel output (Telegram + Band).
