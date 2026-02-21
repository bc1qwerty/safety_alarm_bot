"""안전보건공단 자료실 크롤러 (Selenium + XHR API 호출).

OPS, 책자, 동영상 3개 타입의 자료를 크롤링.
- OPS: 썸네일 이미지(JPEG) 텔레그램 전송
- 책자: 원본 PDF 텔레그램 전송
- 동영상: 첨부파일 전송 (50MB 초과 시 링크 전송)
"""

import base64
import json
import logging
import time

from selenium import webdriver
from selenium.webdriver.chrome.options import Options

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

BASE_URL = "https://portal.kosha.or.kr"
ENTRY_URL = f"{BASE_URL}/archive/cent-archive/master-arch/master-list1?page=1&rowsPerPage=12"
LIST_API = "/api/portal24/bizV/p/VCPDG01007/selectMediaList"
FILE_LIST_API = "/api/portal24/bizA/p/files/getFileList"
FILE_DOWNLOAD_API = "/api/portal24/bizA/p/files/download"
THUMBNAIL_API = "/api/portal24/bizV/p/VCPDG01007/viewThumbnail"

SHP_CD = {"ops": "12", "video": "02", "booklet": "14"}

# 타입별 상세 페이지 URL 패턴
DETAIL_URL = {
    "ops": f"{BASE_URL}/archive/cent-archive/master-arch/master-list1/master-detail1",
    "video": f"{BASE_URL}/archive/cent-archive/master-arch/master-list2/master-detail2",
    "booklet": f"{BASE_URL}/archive/cent-archive/master-arch/master-list3/master-detail3",
}

# 텔레그램 파일 전송 제한 (50MB)
TG_FILE_LIMIT = 50 * 1024 * 1024

# ArrayBuffer→base64 변환 JS 함수 (재사용)
_AB_TO_B64_JS = """
function ab2b64(buffer) {
    const arr = new Uint8Array(buffer);
    let binary = '';
    const chunk = 8192;
    for (let i = 0; i < arr.length; i += chunk) {
        binary += String.fromCharCode.apply(null, arr.subarray(i, i + chunk));
    }
    return btoa(binary);
}
"""


class KoshaArchiveCrawler(BaseCrawler):
    """안전보건공단 자료실 크롤러."""

    def __init__(self, content_type: str):
        """content_type: 'ops' | 'booklet' | 'video'"""
        self.content_type = content_type
        self.site_name = f"kosha_archive_{content_type}"
        self._shp_cd = SHP_CD[content_type]

    def _create_driver(self) -> webdriver.Chrome:
        opts = Options()
        opts.add_argument("--headless=new")
        opts.add_argument("--no-sandbox")
        opts.add_argument("--disable-dev-shm-usage")
        opts.add_argument("--disable-gpu")
        opts.add_argument("--window-size=1280,720")
        driver = webdriver.Chrome(options=opts)
        driver.set_script_timeout(60)
        return driver

    def fetch_posts(self) -> list[Post]:
        """자료실 API에서 최신 게시글 목록 + 파일 다운로드."""
        driver = None
        try:
            driver = self._create_driver()
            driver.get(ENTRY_URL)
            time.sleep(6)

            items = self._fetch_list(driver)
            if not items:
                logger.warning(f"[{self.site_name}] 목록 조회 실패")
                return []

            posts = []
            for item in items:
                med_seq = str(item.get("medSeq", ""))
                title = item.get("medName", "").strip()
                atcfl_no = item.get("contsAtcflNo", "")
                thumb_atcfl_no = item.get("thumbAtcflNo", "")

                detail_url = (
                    f"{DETAIL_URL[self.content_type]}?medSeq={med_seq}"
                )
                post = Post(
                    post_id=med_seq,
                    title=title,
                    url=detail_url,
                    source="안전보건공단 자료실",
                )

                if self.content_type == "ops":
                    if thumb_atcfl_no:
                        img = self._download_thumbnail(driver, thumb_atcfl_no)
                        if img:
                            post.image_data = img

                elif self.content_type == "booklet":
                    if atcfl_no:
                        file_bytes, file_name = self._download_file(
                            driver, atcfl_no
                        )
                        if file_bytes:
                            post.file_data = file_bytes
                            post.file_name = file_name

                elif self.content_type == "video":
                    ytb_url = item.get("ytbUrlAddr")
                    if ytb_url:
                        post.url = ytb_url
                    elif atcfl_no:
                        file_info = self._get_file_info(driver, atcfl_no)
                        if file_info:
                            file_size = file_info.get("atcflSz", 0)
                            if file_size > TG_FILE_LIMIT:
                                logger.info(
                                    f"[{self.site_name}] 동영상 "
                                    f"{file_size/1024/1024:.1f}MB → 링크 전송"
                                )
                            else:
                                file_bytes = self._download_blob(
                                    driver, atcfl_no,
                                    file_info.get("atcflSeq", 1),
                                )
                                if file_bytes:
                                    post.file_data = file_bytes
                                    post.file_name = file_info.get(
                                        "orgnlAtchFileNm", "video"
                                    )

                posts.append(post)

            logger.info(f"[{self.site_name}] {len(posts)}건 파싱 완료")
            return posts

        except Exception as e:
            logger.error(f"[{self.site_name}] 크롤링 실패: {e}")
            return []
        finally:
            if driver:
                driver.quit()

    def _fetch_list(self, driver, page: int = 1, count: int = 10) -> list[dict]:
        """selectMediaList API 호출 (동기 XHR)."""
        result = driver.execute_script(f"""
            try {{
                const xhr = new XMLHttpRequest();
                xhr.open('POST', '{LIST_API}', false);
                xhr.setRequestHeader('Content-Type', 'application/json');
                xhr.send(JSON.stringify({{
                    "shpCd": "{self._shp_cd}",
                    "searchCondition": "all",
                    "searchValue": null,
                    "ascDesc": "desc",
                    "page": {page},
                    "rowsPerPage": {count}
                }}));
                if (xhr.status === 200) return xhr.responseText;
                return '{{"error": "status ' + xhr.status + '"}}';
            }} catch(e) {{ return '{{"error": "' + e.message + '"}}'; }}
        """)
        try:
            data = json.loads(result)
            if "error" in data:
                logger.error(f"[{self.site_name}] API 오류: {data['error']}")
                return []
            return data.get("payload", {}).get("list", [])
        except json.JSONDecodeError:
            logger.error(f"[{self.site_name}] API 응답 파싱 실패")
            return []

    def _download_thumbnail(self, driver, thumb_atcfl_no: str) -> bytes | None:
        """썸네일 이미지(JPEG) 다운로드."""
        try:
            b64 = driver.execute_async_script(f"""
                {_AB_TO_B64_JS}
                const callback = arguments[arguments.length - 1];
                const xhr = new XMLHttpRequest();
                xhr.open('GET', '{THUMBNAIL_API}?atcflNo={thumb_atcfl_no},1');
                xhr.responseType = 'arraybuffer';
                xhr.timeout = 30000;
                xhr.onload = () => {{
                    if (xhr.status !== 200) {{ callback(null); return; }}
                    callback(ab2b64(xhr.response));
                }};
                xhr.onerror = () => callback(null);
                xhr.ontimeout = () => callback(null);
                xhr.send();
            """)
            if b64:
                return base64.b64decode(b64)
        except Exception as e:
            logger.warning(f"[{self.site_name}] 썸네일 다운로드 실패: {e}")
        return None

    def _download_file(self, driver, atcfl_no: str) -> tuple[bytes | None, str]:
        """첨부파일 다운로드 (파일 정보 조회 + 다운로드)."""
        try:
            file_info = self._get_file_info(driver, atcfl_no)
            if not file_info:
                logger.warning(f"[{self.site_name}] 파일 정보 없음: {atcfl_no}")
                return None, ""

            seq = file_info.get("atcflSeq", 1)
            orig_name = file_info.get("orgnlAtchFileNm", "file")
            file_size = file_info.get("atcflSz", 0)

            # 50MB 초과 파일은 스킵
            if file_size > TG_FILE_LIMIT:
                logger.info(
                    f"[{self.site_name}] 파일 크기 초과 "
                    f"({file_size/1024/1024:.1f}MB): {orig_name}"
                )
                return None, ""

            logger.info(
                f"[{self.site_name}] 다운로드: {orig_name} "
                f"(seq={seq}, size={file_size})"
            )

            file_bytes = self._download_blob(driver, atcfl_no, seq)
            if file_bytes:
                return file_bytes, orig_name
            logger.warning(f"[{self.site_name}] blob 다운로드 결과 None: {atcfl_no}")
        except Exception as e:
            logger.warning(f"[{self.site_name}] 파일 다운로드 실패: {e}")
        return None, ""

    def _download_blob(self, driver, atcfl_no: str, seq: int) -> bytes | None:
        """파일 바이너리 다운로드 (async XHR → base64)."""
        try:
            b64 = driver.execute_async_script(f"""
                {_AB_TO_B64_JS}
                const callback = arguments[arguments.length - 1];
                const xhr = new XMLHttpRequest();
                xhr.open('POST', '{FILE_DOWNLOAD_API}');
                xhr.setRequestHeader('Content-Type', 'application/json');
                xhr.responseType = 'arraybuffer';
                xhr.timeout = 45000;
                xhr.onload = () => {{
                    if (xhr.status !== 200) {{ callback(null); return; }}
                    callback(ab2b64(xhr.response));
                }};
                xhr.onerror = () => callback(null);
                xhr.ontimeout = () => callback(null);
                xhr.send(JSON.stringify({{
                    fileId: '{atcfl_no}',
                    seq: {seq},
                    taskSeCd: '10',
                    isDirect: 'N'
                }}));
            """)
            if b64:
                return base64.b64decode(b64)
        except Exception as e:
            logger.warning(f"[{self.site_name}] blob 다운로드 실패: {e}")
        return None

    def _get_file_info(self, driver, atcfl_no: str) -> dict | None:
        """getFileList API로 파일 메타데이터 조회 (동기 XHR)."""
        try:
            result = driver.execute_script(f"""
                try {{
                    const xhr = new XMLHttpRequest();
                    xhr.open('POST', '{FILE_LIST_API}', false);
                    xhr.setRequestHeader('Content-Type', 'application/json');
                    xhr.send(JSON.stringify({{
                        "fileId": "{atcfl_no}",
                        "fileUploadType": "02",
                        "atcflTaskColNm": "lastFile",
                        "atcflSeTaskComCdNm": "Y"
                    }}));
                    if (xhr.status === 200) return xhr.responseText;
                    return 'ERR:' + xhr.status;
                }} catch(e) {{ return 'ERR:' + e.message; }}
            """)
            if result and result.startswith("ERR:"):
                logger.warning(
                    f"[{self.site_name}] 파일 정보 API 오류: {result}"
                )
                return None
            if result:
                data = json.loads(result)
                files = data.get("payload", [])
                if files:
                    return files[0]
        except Exception as e:
            logger.warning(f"[{self.site_name}] 파일 정보 조회 실패: {e}")
        return None
