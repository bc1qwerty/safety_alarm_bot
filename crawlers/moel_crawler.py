"""고용노동부 공지사항 크롤러 (requests + BeautifulSoup)."""

import logging

import requests
from bs4 import BeautifulSoup

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

BASE_URL = "https://www.moel.go.kr"
LIST_URL = f"{BASE_URL}/news/notice/noticeList.do"


class MoelCrawler(BaseCrawler):
    site_name = "moel"

    def fetch_posts(self) -> list[Post]:
        """고용노동부 공지사항 최신 게시글 파싱."""
        try:
            resp = requests.get(LIST_URL, timeout=15)
            resp.raise_for_status()
        except requests.RequestException as e:
            logger.error(f"[moel] 요청 실패: {e}")
            return []

        soup = BeautifulSoup(resp.text, "html.parser")
        rows = soup.select("table.tstyle_list tbody tr")

        posts = []
        for row in rows:
            # 공지(상단고정) 행 건너뛰기
            num_td = row.select_one("td[aria-label='번호']")
            if not num_td:
                continue
            num_text = num_td.get_text(strip=True)
            if not num_text.isdigit():
                continue

            # 제목 + 링크 추출
            link_tag = row.select_one("strong.b_tit a")
            if not link_tag:
                continue

            title = link_tag.get("title") or link_tag.get_text(strip=True)
            href = link_tag.get("href", "")
            if href and not href.startswith("http"):
                href = BASE_URL + href

            # bbs_seq를 ID로 사용
            post_id = num_text
            posts.append(Post(
                post_id=post_id,
                title=title,
                url=href,
                source="고용노동부",
            ))

        logger.info(f"[moel] {len(posts)}건 파싱 완료")
        return posts
