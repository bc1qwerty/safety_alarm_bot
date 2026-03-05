"""고용노동부 공지사항 크롤러 (Scrapling)."""

import logging

from scrapling.fetchers import Fetcher

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

BASE_URL = "https://www.moel.go.kr"
LIST_URL = f"{BASE_URL}/news/notice/noticeList.do"


class MoelCrawler(BaseCrawler):
    site_name = "moel"

    def fetch_posts(self) -> list[Post]:
        """고용노동부 공지사항 최신 게시글 파싱."""
        try:
            page = Fetcher(auto_match=False).get(LIST_URL, timeout=15)
            if page.status != 200:
                logger.error(f"[moel] HTTP {page.status}")
                return []
        except Exception as e:
            logger.error(f"[moel] 요청 실패: {e}")
            return []

        rows = page.css("table.tstyle_list tbody tr")

        posts = []
        for row in rows:
            num_tds = row.css("td[aria-label='번호']")
            if not num_tds:
                continue
            num_text = num_tds[0].text.strip()
            if not num_text.isdigit():
                continue

            link_tags = row.css("strong.b_tit a")
            if not link_tags:
                continue
            link_tag = link_tags[0]

            title = link_tag.attrib.get("title") or link_tag.text.strip()
            href = link_tag.attrib.get("href", "")
            if href and not href.startswith("http"):
                href = BASE_URL + href

            posts.append(Post(
                post_id=num_text,
                title=title,
                url=href,
                source="고용노동부",
            ))

        logger.info(f"[moel] {len(posts)}건 파싱 완료")
        return posts
