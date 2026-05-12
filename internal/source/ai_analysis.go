package source

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bc1qwerty/txid-bot-framework/pkg/core"
)

// AnalyzeNotice asks the configured LLM CLI for a one-line take on a
// safety notice — typically "이 공지가 현재 사용자의 건축 프로젝트에
// 영향이 있는가, 있다면 어떻게" 한 줄. Returns "" when disabled, on
// timeout, or on any error. Callers MUST treat the result as best-
// effort context.
//
// Configured via env:
//
//	SAFETY_AI_CLI       "claude" | "gemini" | "" (disables)
//	SAFETY_AI_HINT      free-form context about the user's projects
//	                    (e.g., "주택재개발 / 강북구 / 6층 이하 / 골조 단계")
//	SAFETY_AI_TIMEOUT   per-call wall clock (Go duration, default 25s)
//
// Output is capped at 140 runes and one line so prepending to the
// Telegram message never breaks the HTML layout.
func AnalyzeNotice(item core.Item) string {
	cli := strings.ToLower(strings.TrimSpace(os.Getenv("SAFETY_AI_CLI")))
	switch cli {
	case "", "none", "off":
		return ""
	}
	if _, err := exec.LookPath(cli); err != nil {
		return ""
	}

	timeout := 25 * time.Second
	if v := os.Getenv("SAFETY_AI_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			timeout = d
		}
	}

	prompt := buildSafetyPrompt(item)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var args []string
	switch cli {
	case "claude":
		args = []string{"--print", prompt}
	case "gemini":
		args = []string{"--prompt", prompt}
	default:
		args = []string{prompt}
	}

	cmd := exec.CommandContext(ctx, cli, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return ""
	}
	if idx := strings.IndexByte(line, '\n'); idx > 0 {
		line = line[:idx]
	}
	runes := []rune(line)
	if len(runes) > 140 {
		line = string(runes[:137]) + "..."
	}
	return line
}

func buildSafetyPrompt(item core.Item) string {
	var b strings.Builder
	b.WriteString("아래 한국 건설 안전 공지가 현재 사용자의 진행 중 건축 프로젝트에 ")
	b.WriteString("실질 영향을 주는지 한 줄로만 답하라 ")
	b.WriteString("(최대 100자, 한국어 존댓말, em-dash와 이모지 금지, 단정적 표현 금지, ")
	b.WriteString("영향이 없으면 '직접 영향 없음'으로 시작).\n\n")
	b.WriteString("출처: ")
	b.WriteString(item.Category)
	b.WriteString("\n제목: ")
	b.WriteString(item.Title)
	if item.URL != "" {
		b.WriteString("\nURL: ")
		b.WriteString(item.URL)
	}
	if hint := strings.TrimSpace(os.Getenv("SAFETY_AI_HINT")); hint != "" {
		b.WriteString("\n사용자 컨텍스트: ")
		b.WriteString(hint)
	}
	b.WriteString("\n\n출력은 평가 한 줄만, 접두어 없이.")
	return b.String()
}
