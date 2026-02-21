import os
from pathlib import Path

from dotenv import load_dotenv

load_dotenv(Path(__file__).parent / ".env")

# Telegram
TELEGRAM_BOT_TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "")
TELEGRAM_CHAT_ID = os.environ.get("TELEGRAM_CHAT_ID", "")

# Naver Band
BAND_ACCESS_TOKEN = os.environ.get("BAND_ACCESS_TOKEN", "")
BAND_KEY = os.environ.get("BAND_KEY", "")

# Data path
DATA_DIR = os.path.join(os.path.dirname(__file__), "data")
LAST_POST_IDS_PATH = os.path.join(DATA_DIR, "last_post_ids.json")
