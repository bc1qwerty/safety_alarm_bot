"""안전보건공단 공지사항 크롤러 (Selenium headless Chrome)."""

import json
import logging

from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.support.ui import WebDriverWait

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

BOARD_URL = "https://www.kosha.or.kr/notification/notice/contruction?bbsId=B2025021400001"
DETAIL_URL = "https://www.kosha.or.kr/notification/notice/contruction?bbsId=B2025021400001&pstNo="


class KoshaCrawler(BaseCrawler):
    site_name = "kosha"

    def _create_driver(self) -> webdriver.Chrome:
        """Headless Chrome 드라이버 생성."""
        opts = Options()
        opts.add_argument("--headless=new")
        opts.add_argument("--no-sandbox")
        opts.add_argument("--disable-dev-shm-usage")
        opts.add_argument("--disable-gpu")
        opts.add_argument("--window-size=1280,720")
        return webdriver.Chrome(options=opts)

    def fetch_posts(self) -> list[Post]:
        """안전보건공단 공지사항 게시글 파싱 (Selenium)."""
        driver = None
        try:
            driver = self._create_driver()
            driver.get(BOARD_URL)

            WebDriverWait(driver, 15).until(
                EC.presence_of_element_located(
                    (By.CSS_SELECTOR, ".tboard_list_row")
                )
            )

            # JS 내부 데이터에서 pstNo 목록 추출
            base_list = json.loads(driver.execute_script(
                "return JSON.stringify("
                "koshaTboard.bbsInfo.tboard.result.search.baseList);"
            ))
            pst_map = {item["rnum"]: item["pstNo"] for item in base_list}

            # DOM에서 번호 + 제목 파싱
            rows = driver.find_elements(By.CSS_SELECTOR, ".tboard_list_row")
            posts = []
            for idx, row in enumerate(rows):
                # 번호
                num_el = row.find_element(
                    By.CSS_SELECTOR,
                    "[data-tboard-artcl-no='D020100001']",
                )
                num_text = num_el.text.strip().replace(",", "")
                # "No" 라벨 제거
                num_text = num_text.replace("No", "").strip()
                if not num_text.isdigit():
                    continue

                # 제목
                title_el = row.find_element(
                    By.CSS_SELECTOR, "a.tboard_list_subject"
                )
                title = title_el.get_attribute("title") or title_el.text.strip()

                # pstNo 매칭 (rnum = idx + 1)
                pst_no = pst_map.get(idx + 1, "")
                url = f"{DETAIL_URL}{pst_no}" if pst_no else BOARD_URL

                posts.append(Post(
                    post_id=num_text,
                    title=title,
                    url=url,
                    source="안전보건공단",
                ))

            logger.info(f"[kosha] {len(posts)}건 파싱 완료")
            return posts

        except Exception as e:
            logger.error(f"[kosha] 크롤링 실패: {e}")
            return []
        finally:
            if driver:
                driver.quit()
