package notifyhub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 예전 LogPush 는 fmt %q 로 JSON 을 손조립했다. Go 의 \x1b 이스케이프는
// JSON 으로는 불법이라 제어문자가 든 에러 메시지를 허브가 400 으로 버렸고,
// 상태코드도 안 봐서 아무도 몰랐다. 두 결함 모두 여기서 잠근다.
func TestLogPushEncodesControlChars(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATION_HUB_URL", srv.URL+"/notifications/push")
	t.Setenv("NOTIFICATION_SECRET", "s")

	msg := "esc \x1b[31m quote \" newline \n 한글"
	if err := LogPush("safety-alarm-bot", "error", msg, "detail"); err != nil {
		t.Fatal(err)
	}

	var m map[string]string
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("hub received invalid JSON: %v (body=%q)", err, got)
	}
	if m["message"] != msg {
		t.Errorf("message = %q, want %q", m["message"], msg)
	}
	if m["source"] != "safety-alarm-bot" || m["level"] != "error" || m["details"] != "detail" {
		t.Errorf("fields drifted: %+v", m)
	}
}

func TestLogPushSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()
	t.Setenv("NOTIFICATION_HUB_URL", srv.URL+"/notifications/push")
	t.Setenv("NOTIFICATION_SECRET", "s")

	if err := LogPush("src", "info", "m", ""); err == nil {
		t.Error("HTTP 400 이 무음으로 넘어갔다 — 상태코드를 봐야 한다")
	}
}
