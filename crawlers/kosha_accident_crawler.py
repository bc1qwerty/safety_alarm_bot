"""안전보건공단 중대재해 사이렌 크롤러 (Selenium + CDP 네트워크 캡처)."""

import base64
import json
import logging

from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.support.ui import WebDriverWait

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

LIST_URL = "https://portal.kosha.or.kr/archive/imprtnDsstrAlrame/CSADV50000/CSADV50000M01"
API_KEYWORD = "selectImprtnDsstrSirnList"


class KoshaAccidentCrawler(BaseCrawler):
    site_name = "kosha_accident"

    def _create_driver(self) -> webdriver.Chrome:
        """CDP 네트워크 로깅 활성화된 headless Chrome."""
        opts = Options()
        opts.add_argument("--headless=new")
        opts.add_argument("--no-sandbox")
        opts.add_argument("--disable-dev-shm-usage")
        opts.add_argument("--disable-gpu")
        opts.add_argument("--window-size=1280,720")
        opts.set_capability("goog:loggingPrefs", {"performance": "ALL"})
        return webdriver.Chrome(options=opts)

    def fetch_posts(self) -> list[Post]:
        """중대재해 사이렌 게시글 파싱 (API 응답에서 이미지 포함)."""
        driver = None
        try:
            driver = self._create_driver()
            driver.get(LIST_URL)

            WebDriverWait(driver, 15).until(
                EC.presence_of_element_located((By.CSS_SELECTOR, "a.subject"))
            )

            items = self._extract_from_logs(driver)
            if not items:
                logger.warning("[kosha_accident] API 응답 캡처 실패")
                return []

            posts = []
            for item in items:
                no = str(item.get("imprtnDsstrSirnNo", ""))
                title = item.get("imprtnDsstrSirnNm", "")

                # base64 이미지 디코딩
                img_bytes = None
                img_src = item.get("imgSrc", "")
                if img_src:
                    # "data:image/jpg;base64,..." 형식에서 base64 부분 추출
                    if "," in img_src:
                        img_src = img_src.split(",", 1)[1]
                    try:
                        img_bytes = base64.b64decode(img_src)
                    except Exception:
                        logger.warning(f"[kosha_accident] 이미지 디코딩 실패: {no}")

                posts.append(Post(
                    post_id=no,
                    title=title,
                    url=LIST_URL,
                    source="중대재해 사이렌",
                    image_data=img_bytes,
                ))

            logger.info(f"[kosha_accident] {len(posts)}건 파싱 완료")
            return posts

        except Exception as e:
            logger.error(f"[kosha_accident] 크롤링 실패: {e}")
            return []
        finally:
            if driver:
                driver.quit()

    def _extract_from_logs(self, driver) -> list[dict]:
        """CDP Performance 로그에서 API 응답 데이터 추출."""
        logs = driver.get_log("performance")
        for log in logs:
            msg = json.loads(log["message"])["message"]
            if msg["method"] != "Network.responseReceived":
                continue
            url = msg["params"]["response"]["url"]
            if API_KEYWORD not in url:
                continue
            req_id = msg["params"]["requestId"]
            try:
                body = driver.execute_cdp_cmd(
                    "Network.getResponseBody", {"requestId": req_id}
                )
                data = json.loads(body["body"])
                return data.get("payload", {}).get("imprtnDsstrSirnList", [])
            except Exception:
                continue
        return []
