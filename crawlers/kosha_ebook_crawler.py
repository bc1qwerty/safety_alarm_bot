"""안전보건공단 월간 안전보건 e-Book 크롤러 (requests + BeautifulSoup).

www.kosha.or.kr의 JSP 레거시 시스템에서 월간 안전보건 PDF 다운로드.
"""

import logging
import re
from urllib.parse import quote, unquote

import requests
from bs4 import BeautifulSoup

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

EBOOK_BASE = "https://www.kosha.or.kr/ebook"
MONTHLY_LIST_URL = f"{EBOOK_BASE}/fcatalog/include/monthly_list.jsp"
DOWNLOAD_URL = f"{EBOOK_BASE}/fcatalog/download.jsp"

# 텔레그램 파일 전송 제한 (50MB)
TG_FILE_LIMIT = 50 * 1024 * 1024


class KoshaEbookCrawler(BaseCrawler):
    """안전보건공단 월간 안전보건 e-Book 크롤러."""

    site_name = "kosha_ebook"

    def fetch_posts(self) -> list[Post]:
        """월간 안전보건 목록에서 최신 게시글 파싱 + PDF 다운로드."""
        try:
            resp = requests.get(MONTHLY_LIST_URL, timeout=15)
            resp.raise_for_status()
            resp.encoding = "utf-8"
        except requests.RequestException as e:
            logger.error(f"[{self.site_name}] 목록 요청 실패: {e}")
            return []

        soup = BeautifulSoup(resp.text, "html.parser")
        items = soup.select("ul.e-booklist > li")

        posts = []
        for item in items:
            post = self._parse_item(item)
            if post:
                posts.append(post)

        logger.info(f"[{self.site_name}] {len(posts)}건 파싱 완료")
        return posts

    def _parse_item(self, item) -> Post | None:
        """li 요소에서 게시글 정보 추출."""
        # 상세 링크에서 sdir 추출
        detail_link = item.select_one("a[href*='detail']")
        if not detail_link:
            return None

        href = detail_link.get("href", "")
        sdir_match = re.search(r"sdir=(\d+)", href)
        if not sdir_match:
            return None
        sdir = sdir_match.group(1)

        # 제목
        title_el = item.select_one(".e-title span")
        title = title_el.get_text(strip=True) if title_el else ""

        # PDF 파일명 추출 (show_download 함수 파라미터)
        pdf_link = item.select_one("a[href*='show_download']")
        pdf_files = []
        if pdf_link:
            pdf_href = pdf_link.get("href", "")
            pdf_match = re.search(
                r"show_download\('([^']*)',\s*'([^']*)'", pdf_href
            )
            if pdf_match:
                files_str = pdf_match.group(1)
                pdf_files = [
                    f.strip() for f in files_str.split(";") if f.strip()
                ]

        # e-Book 뷰어 URL
        viewer_url = (
            f"{EBOOK_BASE}/fcatalog/ecatalog5.jsp"
            f"?Dir={sdir}&catimage=&listCall=Y&eclang="
        )

        # PDF 다운로드 URL 목록 생성
        dl_urls = []
        for fn_raw in pdf_files:
            fn = unquote(fn_raw)
            url = (
                f"{DOWNLOAD_URL}?kd=feb&sdir={sdir}"
                f"&cimg=&fn={quote(fn)}"
            )
            dl_urls.append((fn, url))

        post = Post(
            post_id=sdir,
            title=title,
            url=viewer_url,
            source="월간 안전보건",
            download_urls=dl_urls,
        )

        # 50MB 이하 PDF는 직접 전송 시도
        if pdf_files:
            file_bytes, file_name = self._download_pdf(sdir, pdf_files)
            if file_bytes:
                post.file_data = file_bytes
                post.file_name = file_name

        return post

    def _download_pdf(
        self, sdir: str, pdf_files: list[str]
    ) -> tuple[bytes | None, str]:
        """PDF 파일 다운로드. 50MB 이하 파일 중 첫 번째 선택."""
        for fn_raw in pdf_files:
            fn = unquote(fn_raw)
            url = (
                f"{DOWNLOAD_URL}?kd=feb&sdir={sdir}"
                f"&cimg=&fn={quote(fn)}"
            )
            try:
                # HEAD로 크기 확인
                head = requests.head(url, timeout=10, allow_redirects=True)
                content_length = int(head.headers.get("Content-Length", 0))

                if content_length > TG_FILE_LIMIT:
                    logger.info(
                        f"[{self.site_name}] PDF 크기 초과 "
                        f"({content_length/1024/1024:.1f}MB): {fn}"
                    )
                    continue

                # 다운로드
                resp = requests.get(url, timeout=120, stream=True)
                resp.raise_for_status()

                data = resp.content
                if len(data) > TG_FILE_LIMIT:
                    logger.info(
                        f"[{self.site_name}] PDF 실제 크기 초과 "
                        f"({len(data)/1024/1024:.1f}MB): {fn}"
                    )
                    continue

                logger.info(
                    f"[{self.site_name}] PDF 다운로드 완료: {fn} "
                    f"({len(data)/1024/1024:.1f}MB)"
                )
                return data, fn

            except requests.RequestException as e:
                logger.warning(f"[{self.site_name}] PDF 다운로드 실패: {e}")
                continue

        return None, ""
