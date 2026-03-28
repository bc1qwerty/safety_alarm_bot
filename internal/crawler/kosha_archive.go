package crawler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const (
	archiveBaseURL      = "https://portal.kosha.or.kr"
	archiveListAPI      = archiveBaseURL + "/api/portal24/bizV/p/VCPDG01007/selectMediaList"
	archiveFileListAPI  = archiveBaseURL + "/api/portal24/bizA/p/files/getFileList"
	archiveFileDownload = archiveBaseURL + "/api/portal24/bizA/p/files/download"
	archiveThumbnailAPI = archiveBaseURL + "/api/portal24/bizV/p/VCPDG01007/viewThumbnail"
	tgFileLimit         = 50 * 1024 * 1024 // 50MB
)

var archiveShpCd = map[string]string{
	"ops":      "12",
	"video":    "02",
	"booklet":  "14",
}

var archiveDetailURL = map[string]string{
	"ops":      archiveBaseURL + "/archive/cent-archive/master-arch/master-list1/master-detail1",
	"video":    archiveBaseURL + "/archive/cent-archive/master-arch/master-list2/master-detail2",
	"booklet":  archiveBaseURL + "/archive/cent-archive/master-arch/master-list3/master-detail3",
}

// KoshaArchiveCrawler crawls 안전보건공단 자료실 (OPS/booklet/video).
type KoshaArchiveCrawler struct {
	BaseCrawler
	ContentType string
	shpCd       string
}

func NewKoshaArchiveCrawler(contentType string) *KoshaArchiveCrawler {
	return &KoshaArchiveCrawler{
		BaseCrawler: BaseCrawler{Name: "kosha_archive_" + contentType},
		ContentType: contentType,
		shpCd:       archiveShpCd[contentType],
	}
}

// archiveListResponse represents the API response for media list.
type archiveListResponse struct {
	Payload struct {
		List []archiveItem `json:"list"`
	} `json:"payload"`
}

type archiveItem struct {
	MedSeq       int    `json:"medSeq"`
	MedName      string `json:"medName"`
	ContsAtcflNo string `json:"contsAtcflNo"`
	ThumbAtcflNo string `json:"thumbAtcflNo"`
	YtbURLAddr   string `json:"ytbUrlAddr"`
}

// archiveFileInfo represents file metadata from the file list API.
type archiveFileInfo struct {
	AtcflSeq        int    `json:"atcflSeq"`
	AtcflSz         int64  `json:"atcflSz"`
	OrgnlAtchFileNm string `json:"orgnlAtchFileNm"`
}

func (c *KoshaArchiveCrawler) FetchPosts() ([]Post, error) {
	items, err := c.fetchList()
	if err != nil {
		return nil, fmt.Errorf("[%s] list fetch failed: %w", c.Name, err)
	}
	if len(items) == 0 {
		log.Printf("[%s] empty list", c.Name)
		return nil, nil
	}

	var posts []Post
	for _, item := range items {
		medSeq := fmt.Sprintf("%d", item.MedSeq)
		title := item.MedName
		detailURL := fmt.Sprintf("%s?medSeq=%s", archiveDetailURL[c.ContentType], medSeq)

		post := Post{
			PostID: medSeq,
			Title:  title,
			URL:    detailURL,
			Source:  "\uC548\uC804\uBCF4\uAC74\uACF5\uB2E8 \uC790\uB8CC\uC2E4", // 안전보건공단 자료실
		}

		switch c.ContentType {
		case "ops":
			if item.ThumbAtcflNo != "" {
				post.ImageData = c.downloadThumbnail(item.ThumbAtcflNo)
			}
		case "booklet":
			if item.ContsAtcflNo != "" {
				fileBytes, fileName := c.downloadFile(item.ContsAtcflNo)
				if fileBytes != nil {
					post.FileData = fileBytes
					post.FileName = fileName
				}
			}
		case "video":
			if item.YtbURLAddr != "" {
				post.URL = item.YtbURLAddr
			} else if item.ContsAtcflNo != "" {
				fi := c.getFileInfo(item.ContsAtcflNo)
				if fi != nil && fi.AtcflSz <= tgFileLimit {
					blob := c.downloadBlob(item.ContsAtcflNo, fi.AtcflSeq)
					if blob != nil {
						post.FileData = blob
						post.FileName = fi.OrgnlAtchFileNm
						if post.FileName == "" {
							post.FileName = "video"
						}
					}
				}
			}
		}

		posts = append(posts, post)
	}

	log.Printf("[%s] %d posts parsed", c.Name, len(posts))
	return posts, nil
}

func (c *KoshaArchiveCrawler) GetNewPosts() ([]Post, error) {
	posts, err := c.FetchPosts()
	if err != nil {
		return nil, err
	}
	return FilterNewPosts(c.Name, posts), nil
}

func (c *KoshaArchiveCrawler) fetchList() ([]archiveItem, error) {
	body := map[string]interface{}{
		"shpCd":           c.shpCd,
		"searchCondition": "all",
		"searchValue":     nil,
		"ascDesc":         "desc",
		"page":            1,
		"rowsPerPage":     10,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", archiveListAPI, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", archiveBaseURL+"/")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result archiveListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Payload.List, nil
}

func (c *KoshaArchiveCrawler) downloadThumbnail(thumbAtcflNo string) []byte {
	url := fmt.Sprintf("%s?atcflNo=%s,1", archiveThumbnailAPI, thumbAtcflNo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[%s] thumbnail request create failed: %v", c.Name, err)
		return nil
	}
	req.Header.Set("Referer", archiveBaseURL+"/")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[%s] thumbnail download failed: %v", c.Name, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] thumbnail read failed: %v", c.Name, err)
		return nil
	}
	return data
}

func (c *KoshaArchiveCrawler) getFileInfo(atcflNo string) *archiveFileInfo {
	body := map[string]interface{}{
		"fileId":              atcflNo,
		"fileUploadType":      "02",
		"atcflTaskColNm":      "lastFile",
		"atcflSeTaskComCdNm":  "Y",
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", archiveFileListAPI, bytes.NewReader(jsonBody))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", archiveBaseURL+"/")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[%s] file info request failed: %v", c.Name, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result struct {
		Payload []archiveFileInfo `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[%s] file info parse failed: %v", c.Name, err)
		return nil
	}
	if len(result.Payload) == 0 {
		return nil
	}
	return &result.Payload[0]
}

func (c *KoshaArchiveCrawler) downloadFile(atcflNo string) ([]byte, string) {
	fi := c.getFileInfo(atcflNo)
	if fi == nil {
		return nil, ""
	}
	if fi.AtcflSz > tgFileLimit {
		return nil, ""
	}
	seq := fi.AtcflSeq
	if seq == 0 {
		seq = 1
	}
	origName := fi.OrgnlAtchFileNm
	if origName == "" {
		origName = "file"
	}
	data := c.downloadBlob(atcflNo, seq)
	if data == nil {
		return nil, ""
	}
	return data, origName
}

func (c *KoshaArchiveCrawler) downloadBlob(atcflNo string, seq int) []byte {
	body := map[string]interface{}{
		"fileId":    atcflNo,
		"seq":       seq,
		"taskSeCd":  "10",
		"isDirect":  "N",
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", archiveFileDownload, bytes.NewReader(jsonBody))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", archiveBaseURL+"/")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[%s] file download failed: %v", c.Name, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] file read failed: %v", c.Name, err)
		return nil
	}
	return data
}
