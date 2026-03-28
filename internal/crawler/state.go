package crawler

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bc1qwerty/safety-alarm-bot/internal/config"
)

// LoadLastID reads the last saved post ID for a given site name.
func LoadLastID(siteName string) string {
	data, err := os.ReadFile(config.LastPostIDsPath)
	if err != nil {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m[siteName]
}

// SaveLastID writes the last post ID for a given site name.
func SaveLastID(siteName, postID string) {
	_ = os.MkdirAll(filepath.Dir(config.LastPostIDsPath), 0o755)

	m := make(map[string]string)
	data, err := os.ReadFile(config.LastPostIDsPath)
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}

	m[siteName] = postID

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Printf("[state] JSON marshal error: %v", err)
		return
	}
	if err := os.WriteFile(config.LastPostIDsPath, out, 0o644); err != nil {
		log.Printf("[state] write error: %v", err)
	}
}

// FilterNewPosts returns posts newer than the last saved ID and updates state.
// Posts are expected in newest-first order. It stops at the first post whose
// ID is <= lastID (numeric comparison).
func FilterNewPosts(siteName string, posts []Post) []Post {
	lastID := LoadLastID(siteName)

	var newPosts []Post
	for _, p := range posts {
		if lastID != "" {
			cur, err1 := strconv.Atoi(p.PostID)
			last, err2 := strconv.Atoi(lastID)
			if err1 == nil && err2 == nil && cur <= last {
				break
			}
		}
		newPosts = append(newPosts, p)
	}

	if len(newPosts) > 0 {
		SaveLastID(siteName, newPosts[0].PostID)
		log.Printf("[%s] %d new post(s) found", siteName, len(newPosts))
	} else {
		log.Printf("[%s] no new posts", siteName)
	}

	return newPosts
}
