"""안전보건공단 월간 안전보건 e-Book 크롤러 (Scrapling)."""

import logging
import re
from urllib.parse import quote, unquote

from scrapling.fetchers import Fetcher

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

EBOOK_BASE = "https://www.kosha.or.kr/ebook"
MONTHLY_LIST_URL = f"{EBOOK_BASE}/fcatalog/include/monthly_list.jsp"
DOWNLOAD_URL = f"{EBOOK_BASE}/fcatalog/download.jsp"

TG_FILE_LIMIT = 50 * 1024 * 1024


class KoshaEbookCrawler(BaseCrawler):
    site_name = "kosha_ebook"

    def fetch_posts(self) -> list[Post]:
        try:
            page = Fetcher(auto_match=False).get(MONTHLY_LIST_URL, timeout=15)
            if page.status != 200:
                logger.error(f"[{self.site_name}] HTTP {page.status}")
                return []
        except Exception as e:
            logger.error(f"[{self.site_name}] 요청 실패: {e}")
            return []

        items = page.css("ul.e-booklist > li")
        posts = []
        for item in items:
            post = self._parse_item(item)
            if post:
                posts.append(post)

        logger.info(f"[{self.site_name}] {len(posts)}건 파싱 완료")
        return posts

    def _parse_item(self, item) -> Post | None:
        detail_link = item.css("a[href*='detail']")
        if not detail_link:
            return None
        href = detail_link[0].attrib.get("href", "")
        sdir_match = re.search(r"sdir=(\d+)", href)
        if not sdir_match:
            return None
        sdir = sdir_match.group(1)

        title_el = item.css(".e-title span")
        title = title_el[0].text.strip() if title_el else ""

        pdf_link = item.css("a[href*='show_download']")
        pdf_files = []
        if pdf_link:
            pdf_href = pdf_link[0].attrib.get("href", "")
            pdf_match = re.search(r"show_download\('([^']*)',\s*'([^']*)'", pdf_href)
            if pdf_match:
                pdf_files = [f.strip() for f in pdf_match.group(1).split(";") if f.strip()]

        viewer_url = f"{EBOOK_BASE}/fcatalog/ecatalog5.jsp?Dir={sdir}&catimage=&listCall=Y&eclang="
        dl_urls = [(unquote(fn), f"{DOWNLOAD_URL}?kd=feb&sdir={sdir}&cimg=&fn={quote(unquote(fn))}") for fn in pdf_files]

        post = Post(post_id=sdir, title=title, url=viewer_url, source="월간 안전보건", download_urls=dl_urls)

        if pdf_files:
            file_bytes, file_name = self._download_pdf(sdir, pdf_files)
            if file_bytes:
                post.file_data = file_bytes
                post.file_name = file_name

        return post

    def _download_pdf(self, sdir: str, pdf_files: list[str]) -> tuple[bytes | None, str]:
        import httpx
        for fn_raw in pdf_files:
            fn = unquote(fn_raw)
            url = f"{DOWNLOAD_URL}?kd=feb&sdir={sdir}&cimg=&fn={quote(fn)}"
            try:
                with httpx.Client(timeout=120, follow_redirects=True) as client:
                    head = client.head(url)
                    content_length = int(head.headers.get("Content-Length", 0))
                    if content_length > TG_FILE_LIMIT:
                        continue
                    resp = client.get(url)
                    resp.raise_for_status()
                    data = resp.content
                    if len(data) <= TG_FILE_LIMIT:
                        return data, fn
            except Exception as e:
                logger.warning(f"[{self.site_name}] PDF 다운로드 실패: {e}")
        return None, ""
