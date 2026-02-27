import json
import logging
import os
from abc import ABC, abstractmethod
from dataclasses import dataclass, field

from config import LAST_POST_IDS_PATH, DATA_DIR

logger = logging.getLogger(__name__)


@dataclass
class Post:
    """크롤링된 게시글 데이터."""
    post_id: str
    title: str
    url: str
    source: str  # 출처 사이트명
    image_data: bytes | None = field(default=None, repr=False)
    file_data: bytes | None = field(default=None, repr=False)
    file_name: str = ""
    download_urls: list[tuple[str, str]] = field(default_factory=list)  # [(label, url)]


class BaseCrawler(ABC):
    """크롤러 공통 인터페이스."""

    site_name: str = ""

    @abstractmethod
    def fetch_posts(self) -> list[Post]:
        """최신 게시글 목록을 가져온다."""
        ...

    def get_new_posts(self) -> list[Post]:
        """마지막 저장된 ID 이후의 새 게시글만 반환."""
        last_id = self._load_last_id()
        posts = self.fetch_posts()
        if not posts:
            return []

        new_posts = []
        for post in posts:
            if last_id and post.post_id <= last_id:
                break
            new_posts.append(post)

        if new_posts:
            self._save_last_id(new_posts[0].post_id)
            logger.info(f"[{self.site_name}] 새 게시글 {len(new_posts)}건 발견")
        else:
            logger.info(f"[{self.site_name}] 새 게시글 없음")

        return new_posts

    def _load_last_id(self) -> str:
        """저장된 마지막 게시글 ID 로드."""
        if not os.path.exists(LAST_POST_IDS_PATH):
            return ""
        with open(LAST_POST_IDS_PATH, "r", encoding="utf-8") as f:
            data = json.load(f)
        return data.get(self.site_name, "")

    def _save_last_id(self, post_id: str) -> None:
        """마지막 게시글 ID 저장."""
        os.makedirs(DATA_DIR, exist_ok=True)
        data = {}
        if os.path.exists(LAST_POST_IDS_PATH):
            with open(LAST_POST_IDS_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
        data[self.site_name] = post_id
        with open(LAST_POST_IDS_PATH, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
