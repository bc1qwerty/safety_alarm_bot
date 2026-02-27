"""텔레그램 알림 모듈."""

import logging

import requests

from config import TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID

logger = logging.getLogger(__name__)

API_URL = f"https://api.telegram.org/bot{TELEGRAM_BOT_TOKEN}"


def send_message(text: str) -> bool:
    """텔레그램으로 메시지 전송."""
    if not TELEGRAM_BOT_TOKEN or not TELEGRAM_CHAT_ID:
        logger.warning("[telegram] BOT_TOKEN 또는 CHAT_ID 미설정")
        return False

    url = f"{API_URL}/sendMessage"
    payload = {
        "chat_id": TELEGRAM_CHAT_ID,
        "text": text,
        "parse_mode": "HTML",
        "disable_web_page_preview": True,
    }
    try:
        resp = requests.post(url, json=payload, timeout=10)
        resp.raise_for_status()
        logger.info("[telegram] 메시지 전송 성공")
        return True
    except requests.RequestException as e:
        logger.error(f"[telegram] 전송 실패: {e}")
        return False


def send_document(doc_bytes: bytes, filename: str, caption: str = "") -> bool:
    """텔레그램으로 문서(PDF 등) 전송."""
    if not TELEGRAM_BOT_TOKEN or not TELEGRAM_CHAT_ID:
        logger.warning("[telegram] BOT_TOKEN 또는 CHAT_ID 미설정")
        return False

    url = f"{API_URL}/sendDocument"
    data = {"chat_id": TELEGRAM_CHAT_ID}
    if caption:
        data["caption"] = caption
    files = {"document": (filename, doc_bytes, "application/octet-stream")}
    try:
        resp = requests.post(url, data=data, files=files, timeout=60)
        resp.raise_for_status()
        logger.info(f"[telegram] 문서 전송 성공: {filename}")
        return True
    except requests.RequestException as e:
        logger.error(f"[telegram] 문서 전송 실패: {e}")
        return False


def send_photo(photo_bytes: bytes, caption: str = "") -> bool:
    """텔레그램으로 사진 전송."""
    if not TELEGRAM_BOT_TOKEN or not TELEGRAM_CHAT_ID:
        logger.warning("[telegram] BOT_TOKEN 또는 CHAT_ID 미설정")
        return False

    url = f"{API_URL}/sendPhoto"
    data = {"chat_id": TELEGRAM_CHAT_ID}
    if caption:
        data["caption"] = caption
    files = {"photo": ("image.jpg", photo_bytes, "image/jpeg")}
    try:
        resp = requests.post(url, data=data, files=files, timeout=30)
        resp.raise_for_status()
        logger.info("[telegram] 사진 전송 성공")
        return True
    except requests.RequestException as e:
        logger.error(f"[telegram] 사진 전송 실패: {e}")
        return False
