"""Safety Alarm Bot — 안전/노동 공지사항 크롤링 & 알림."""

import logging
import sys

from crawlers.moel_crawler import MoelCrawler
from crawlers.kosha_crawler import KoshaCrawler
from crawlers.kosha_accident_crawler import KoshaAccidentCrawler
from crawlers.kosha_archive_crawler import KoshaArchiveCrawler
from crawlers.kosha_ebook_crawler import KoshaEbookCrawler
from notifiers import telegram_bot, band_api

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
logger = logging.getLogger(__name__)


def format_message(posts, source: str) -> str:
    """게시글 목록을 알림 메시지로 포맷."""
    lines = [f"\U0001f4e2 [{source}] 새 공지사항 {len(posts)}건\n"]
    for post in posts:
        lines.append(f'• <a href="{post.url}">{post.title}</a>')
    return "\n".join(lines)


def format_band_message(posts, source: str) -> str:
    """밴드용 텍스트 메시지 (HTML 미지원)."""
    lines = [f"[{source}] 새 공지사항 {len(posts)}건\n"]
    for post in posts:
        lines.append(f"• {post.title}\n  {post.url}")
    return "\n".join(lines)




def main():
    logger.info("=== Safety Alarm Bot 실행 ===")

    # 일반 공지사항 크롤러 (묶어서 전송)
    notice_crawlers = [
        MoelCrawler(),
        KoshaCrawler(),
    ]

    # 중대재해 크롤러 (건별 전송)
    accident_crawler = KoshaAccidentCrawler()

    total_new = 0

    # 1) 공지사항 처리
    for crawler in notice_crawlers:
        try:
            new_posts = crawler.get_new_posts()
        except Exception as e:
            logger.error(f"[{crawler.site_name}] 크롤링 에러: {e}")
            continue

        if not new_posts:
            continue

        total_new += len(new_posts)

        tg_msg = format_message(new_posts, new_posts[0].source)
        telegram_bot.send_message(tg_msg)

        band_msg = format_band_message(new_posts, new_posts[0].source)
        band_api.send_post(band_msg)

    # 2) 중대재해 처리 (건별 개별 전송)
    try:
        new_accidents = accident_crawler.get_new_posts()
    except Exception as e:
        logger.error(f"[kosha_accident] 크롤링 에러: {e}")
        new_accidents = []

    for post in reversed(new_accidents):  # 오래된 순 → 최신이 마지막
        total_new += 1
        if post.image_data:
            telegram_bot.send_photo(post.image_data)
        else:
            logger.warning(f"[kosha_accident] 이미지 없음: {post.title}")

    # 3) 자료실 처리 (OPS/책자/동영상 건별 전송)
    archive_crawlers = [
        KoshaArchiveCrawler("ops"),
        KoshaArchiveCrawler("booklet"),
        KoshaArchiveCrawler("video"),
    ]

    for crawler in archive_crawlers:
        try:
            new_posts = crawler.get_new_posts()
        except Exception as e:
            logger.error(f"[{crawler.site_name}] 크롤링 에러: {e}")
            continue

        for post in reversed(new_posts):  # 오래된 순 전송
            total_new += 1
            if post.image_data:
                telegram_bot.send_photo(post.image_data, caption=post.title)
            elif post.file_data and post.file_name:
                telegram_bot.send_document(
                    post.file_data, post.file_name, caption=post.title
                )
            else:
                # 파일 없는 경우 (동영상 링크 등) 텍스트 메시지
                msg = f"\U0001f4f9 [{post.source}]\n{post.title}\n{post.url}"
                telegram_bot.send_message(msg)

    # 4) 월간 안전보건 e-Book (PDF 건별 전송)
    ebook_crawler = KoshaEbookCrawler()
    try:
        new_ebooks = ebook_crawler.get_new_posts()
    except Exception as e:
        logger.error(f"[kosha_ebook] 크롤링 에러: {e}")
        new_ebooks = []

    for post in reversed(new_ebooks):
        total_new += 1
        if post.file_data and post.file_name:
            telegram_bot.send_document(
                post.file_data, post.file_name, caption=post.title
            )
        else:
            # e-Book 뷰어 링크 + PDF 다운로드 링크
            lines = [f"\U0001f4d6 [{post.source}] {post.title}\n"]
            lines.append(f"\U0001f4d6 e-Book 보기\n{post.url}")
            for label, dl_url in post.download_urls:
                lines.append(f'\U0001f4e5 <a href="{dl_url}">[다운로드] {label}</a>')
            telegram_bot.send_message("\n".join(lines))

    logger.info(f"=== 완료: 총 {total_new}건 새 공지 ===")


if __name__ == "__main__":
    main()
