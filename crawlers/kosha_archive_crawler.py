"""안전보건공단 자료실 크롤러 (httpx 직접 API 호출).

OPS, 책자, 동영상 3개 타입의 자료를 크롤링.
"""

import base64
import logging

import httpx

from crawlers.base import BaseCrawler, Post

logger = logging.getLogger(__name__)

BASE_URL = "https://portal.kosha.or.kr"
LIST_API = f"{BASE_URL}/api/portal24/bizV/p/VCPDG01007/selectMediaList"
FILE_LIST_API = f"{BASE_URL}/api/portal24/bizA/p/files/getFileList"
FILE_DOWNLOAD_API = f"{BASE_URL}/api/portal24/bizA/p/files/download"
THUMBNAIL_API = f"{BASE_URL}/api/portal24/bizV/p/VCPDG01007/viewThumbnail"

SHP_CD = {"ops": "12", "video": "02", "booklet": "14"}

DETAIL_URL = {
    "ops": f"{BASE_URL}/archive/cent-archive/master-arch/master-list1/master-detail1",
    "video": f"{BASE_URL}/archive/cent-archive/master-arch/master-list2/master-detail2",
    "booklet": f"{BASE_URL}/archive/cent-archive/master-arch/master-list3/master-detail3",
}

TG_FILE_LIMIT = 50 * 1024 * 1024

HEADERS = {
    "Content-Type": "application/json",
    "Referer": BASE_URL + "/",
}


class KoshaArchiveCrawler(BaseCrawler):
    def __init__(self, content_type: str):
        self.content_type = content_type
        self.site_name = f"kosha_archive_{content_type}"
        self._shp_cd = SHP_CD[content_type]

    def fetch_posts(self) -> list[Post]:
        try:
            items = self._fetch_list()
        except Exception as e:
            logger.error(f"[{self.site_name}] 목록 조회 실패: {e}")
            return []

        if not items:
            logger.warning(f"[{self.site_name}] 목록 비어있음")
            return []

        posts = []
        for item in items:
            med_seq = str(item.get("medSeq", ""))
            title = item.get("medName", "").strip()
            atcfl_no = item.get("contsAtcflNo", "")
            thumb_atcfl_no = item.get("thumbAtcflNo", "")
            detail_url = f"{DETAIL_URL[self.content_type]}?medSeq={med_seq}"

            post = Post(post_id=med_seq, title=title, url=detail_url, source="안전보건공단 자료실")

            if self.content_type == "ops" and thumb_atcfl_no:
                post.image_data = self._download_thumbnail(thumb_atcfl_no)

            elif self.content_type == "booklet" and atcfl_no:
                file_bytes, file_name = self._download_file(atcfl_no)
                if file_bytes:
                    post.file_data = file_bytes
                    post.file_name = file_name

            elif self.content_type == "video":
                ytb_url = item.get("ytbUrlAddr")
                if ytb_url:
                    post.url = ytb_url
                elif atcfl_no:
                    file_info = self._get_file_info(atcfl_no)
                    if file_info and file_info.get("atcflSz", 0) <= TG_FILE_LIMIT:
                        file_bytes = self._download_blob(atcfl_no, file_info.get("atcflSeq", 1))
                        if file_bytes:
                            post.file_data = file_bytes
                            post.file_name = file_info.get("orgnlAtchFileNm", "video")

            posts.append(post)

        logger.info(f"[{self.site_name}] {len(posts)}건 파싱 완료")
        return posts

    def _fetch_list(self, page: int = 1, count: int = 10) -> list[dict]:
        with httpx.Client(timeout=15, headers=HEADERS) as client:
            resp = client.post(LIST_API, json={
                "shpCd": self._shp_cd,
                "searchCondition": "all",
                "searchValue": None,
                "ascDesc": "desc",
                "page": page,
                "rowsPerPage": count,
            })
            resp.raise_for_status()
            return resp.json().get("payload", {}).get("list", [])

    def _download_thumbnail(self, thumb_atcfl_no: str) -> bytes | None:
        try:
            with httpx.Client(timeout=30, headers={"Referer": BASE_URL + "/"}) as client:
                resp = client.get(f"{THUMBNAIL_API}?atcflNo={thumb_atcfl_no},1")
                if resp.status_code == 200:
                    return resp.content
        except Exception as e:
            logger.warning(f"[{self.site_name}] 썸네일 다운로드 실패: {e}")
        return None

    def _get_file_info(self, atcfl_no: str) -> dict | None:
        try:
            with httpx.Client(timeout=15, headers=HEADERS) as client:
                resp = client.post(FILE_LIST_API, json={
                    "fileId": atcfl_no,
                    "fileUploadType": "02",
                    "atcflTaskColNm": "lastFile",
                    "atcflSeTaskComCdNm": "Y",
                })
                resp.raise_for_status()
                files = resp.json().get("payload", [])
                return files[0] if files else None
        except Exception as e:
            logger.warning(f"[{self.site_name}] 파일 정보 조회 실패: {e}")
        return None

    def _download_file(self, atcfl_no: str) -> tuple[bytes | None, str]:
        file_info = self._get_file_info(atcfl_no)
        if not file_info:
            return None, ""
        if file_info.get("atcflSz", 0) > TG_FILE_LIMIT:
            return None, ""
        seq = file_info.get("atcflSeq", 1)
        orig_name = file_info.get("orgnlAtchFileNm", "file")
        data = self._download_blob(atcfl_no, seq)
        return (data, orig_name) if data else (None, "")

    def _download_blob(self, atcfl_no: str, seq: int) -> bytes | None:
        try:
            with httpx.Client(timeout=60, headers=HEADERS) as client:
                resp = client.post(FILE_DOWNLOAD_API, json={
                    "fileId": atcfl_no,
                    "seq": seq,
                    "taskSeCd": "10",
                    "isDirect": "N",
                })
                if resp.status_code == 200:
                    return resp.content
        except Exception as e:
            logger.warning(f"[{self.site_name}] 파일 다운로드 실패: {e}")
        return None
