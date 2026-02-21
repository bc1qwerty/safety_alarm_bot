"""네이버 밴드 알림 모듈."""

import logging

import requests

from config import BAND_ACCESS_TOKEN, BAND_KEY

logger = logging.getLogger(__name__)

POST_URL = "https://openapi.band.us/v2.2/band/post/create"


def send_post(content: str) -> bool:
    """네이버 밴드에 게시글 작성."""
    if not BAND_ACCESS_TOKEN or not BAND_KEY:
        logger.warning("[band] ACCESS_TOKEN 또는 BAND_KEY 미설정")
        return False

    params = {
        "access_token": BAND_ACCESS_TOKEN,
        "band_key": BAND_KEY,
        "content": content,
        "do_push": "true",
    }
    try:
        resp = requests.post(POST_URL, data=params, timeout=10)
        resp.raise_for_status()
        result = resp.json()
        if result.get("result_code") == 1:
            logger.info("[band] 게시글 작성 성공")
            return True
        logger.error(f"[band] API 에러: {result}")
        return False
    except requests.RequestException as e:
        logger.error(f"[band] 전송 실패: {e}")
        return False
