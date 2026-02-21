"""중대재해 사이렌 전체 일괄 전송 스크립트 (1회용).

마지막 페이지(가장 오래된 건)부터 역순으로 수집 → 오래된 순서대로 텔레그램 전송.
완료 후 last_post_ids.json에 최신 ID 저장.
"""

import base64
import json
import logging
import os
import sys
import time

from selenium import webdriver
from selenium.webdriver.chrome.options import Options
from selenium.webdriver.common.by import By
from selenium.webdriver.support import expected_conditions as EC
from selenium.webdriver.support.ui import WebDriverWait

import config
from notifiers import telegram_bot

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
logger = logging.getLogger(__name__)

LIST_URL = "https://portal.kosha.or.kr/archive/imprtnDsstrAlrame/CSADV50000/CSADV50000M01"
API_KEYWORD = "selectImprtnDsstrSirnList"
SEND_INTERVAL = 3


def create_driver():
    opts = Options()
    opts.add_argument("--headless=new")
    opts.add_argument("--no-sandbox")
    opts.add_argument("--disable-dev-shm-usage")
    opts.add_argument("--disable-gpu")
    opts.add_argument("--window-size=1280,720")
    opts.set_capability("goog:loggingPrefs", {"performance": "ALL"})
    return webdriver.Chrome(options=opts)


def extract_items_from_logs(driver):
    """CDP 로그에서 API 응답의 게시글 리스트 추출."""
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


def click_page(driver, page_num):
    """페이지 번호 클릭. 필요시 다음/이전 그룹으로 이동."""
    # 현재 보이는 페이지 번호들 확인
    page_links = driver.find_elements(By.CSS_SELECTOR, ".pagination .pageLinks a")
    visible_pages = [a.text.strip() for a in page_links]

    if str(page_num) in visible_pages:
        # 로그 비우기
        driver.get_log("performance")
        for a in page_links:
            if a.text.strip() == str(page_num):
                a.click()
                time.sleep(4)
                return True
    else:
        # 다음 그룹으로 이동
        driver.get_log("performance")
        if page_num > int(visible_pages[-1]):
            driver.find_element(By.CSS_SELECTOR, ".pagination a.next").click()
        else:
            driver.find_element(By.CSS_SELECTOR, ".pagination a.prev").click()
        time.sleep(3)
        return click_page(driver, page_num)

    return False


def main():
    logger.info("=== 중대재해 사이렌 전체 일괄 전송 시작 ===")

    driver = create_driver()
    all_items = []
    try:
        driver.get(LIST_URL)
        WebDriverWait(driver, 15).until(
            EC.presence_of_element_located((By.CSS_SELECTOR, "a.subject"))
        )

        # 1페이지 데이터
        items = extract_items_from_logs(driver)
        if items:
            total_count = items[0].get("totalCount", 0)
            per_page = len(items)
            total_pages = (total_count + per_page - 1) // per_page
            all_items.extend(items)
            logger.info(
                f"총 {total_count}건, 페이지당 {per_page}건, {total_pages}페이지"
            )
            logger.info(f"페이지 1/{total_pages} → {len(items)}건 (누적 {len(all_items)}건)")
        else:
            logger.error("1페이지 캡처 실패")
            return

        # 2페이지부터 마지막까지
        for page in range(2, total_pages + 1):
            retry = 0
            while retry < 3:
                click_page(driver, page)
                items = extract_items_from_logs(driver)
                if items:
                    all_items.extend(items)
                    logger.info(
                        f"페이지 {page}/{total_pages} → {len(items)}건 "
                        f"(누적 {len(all_items)}건)"
                    )
                    break
                retry += 1
                logger.warning(f"페이지 {page} 재시도 {retry}/3")
                time.sleep(2)
            else:
                logger.error(f"페이지 {page} 캡처 실패, 건너뜀")

    finally:
        driver.quit()

    logger.info(f"수집 완료: 총 {len(all_items)}건")

    # 역순 (오래된 것부터)
    all_items.reverse()

    # 텔레그램 전송
    sent = 0
    failed = 0
    max_id = 0

    for i, item in enumerate(all_items):
        no = item.get("imprtnDsstrSirnNo", 0)
        title = item.get("imprtnDsstrSirnNm", "")
        img_src = item.get("imgSrc", "")

        if not img_src:
            logger.warning(f"[{i+1}/{len(all_items)}] 이미지 없음: {title}")
            failed += 1
            continue

        if "," in img_src:
            img_src = img_src.split(",", 1)[1]
        try:
            img_bytes = base64.b64decode(img_src)
        except Exception:
            logger.warning(f"[{i+1}/{len(all_items)}] 디코딩 실패: {title}")
            failed += 1
            continue

        ok = telegram_bot.send_photo(img_bytes)
        if ok:
            sent += 1
            if no > max_id:
                max_id = no
        else:
            failed += 1

        if (i + 1) % 50 == 0:
            logger.info(
                f"진행: {i+1}/{len(all_items)} (성공 {sent}, 실패 {failed})"
            )

        time.sleep(SEND_INTERVAL)

    # 최신 ID 저장
    if max_id > 0:
        data = {}
        if os.path.exists(config.LAST_POST_IDS_PATH):
            with open(config.LAST_POST_IDS_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
        data["kosha_accident"] = str(max_id)
        with open(config.LAST_POST_IDS_PATH, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)

    logger.info(f"=== 완료: 전송 {sent}건, 실패 {failed}건, 최신 ID: {max_id} ===")


if __name__ == "__main__":
    main()
