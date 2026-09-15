package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bc1qwerty/txid-bot-framework/pkg/store"
)

// sourceListLifetime 은 이 봇이 읽는 목록이 옛 글을 계속 노출하는 기간이다.
// MOEL 공지·KOSHA 자료실은 12개월치를 그대로 보여 준다 — dedup 보존이 이보다
// 짧으면 목록에 남아 있는 글의 dedup 행만 먼저 사라져 재발송된다.
const sourceListLifetime = 365 * 24 * time.Hour

// dedup 보존기간은 소스 목록 수명보다 길어야 한다. 이 값이 짧아지면 2026-09-15
// 사고(정리가 194행을 지워 월간지 백호 9건 재발송)가 그대로 재현된다.
func TestDedupRetainOutlivesSourceList(t *testing.T) {
	if dedupRetain <= sourceListLifetime {
		t.Fatalf("dedupRetain=%s 가 소스 목록 수명 %s 이하다 — 목록에 남아 있는 글이 재발송된다",
			dedupRetain, sourceListLifetime)
	}
}

// 실제 store 로 «구 동작이 틀렸고 새 동작이 맞다» 를 보인다. 목록 수명 안(120일
// 전)에 본 항목을 두고 정리를 돌린다: 90일 보존은 그 행을 지워 IsSeen 이
// false 가 되고(= 다음 폴에서 재발송), dedupRetain 은 그대로 유지한다.
func TestCleanupRetention_OldBehaviorRedispatches(t *testing.T) {
	const (
		src    = "moel"
		itemID = "moel-20260517-0001"
	)
	seenAt := time.Now().Add(-120 * 24 * time.Hour).Unix()

	open := func(t *testing.T) *store.Store {
		t.Helper()
		st, err := store.Open(filepath.Join(t.TempDir(), "safety-alarm.db"), hubChannel)
		if err != nil {
			t.Fatalf("store open: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })
		if err := st.MarkSeen(src, itemID); err != nil {
			t.Fatalf("MarkSeen: %v", err)
		}
		if _, err := st.DB().Exec(
			`UPDATE bot_seen SET seen_at = ? WHERE bot_key = ? AND source = ? AND item_id = ?`,
			seenAt, st.BotKey(), src, itemID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		return st
	}

	// 구 동작 주입: 하드코딩돼 있던 90일.
	old := open(t)
	if err := old.Cleanup(90 * 24 * time.Hour); err != nil {
		t.Fatalf("cleanup(90d): %v", err)
	}
	if seen, err := old.IsSeen(src, itemID); err != nil {
		t.Fatalf("IsSeen: %v", err)
	} else if seen {
		t.Fatal("90일 보존인데도 행이 남았다 — 이 테스트가 사고를 재현하지 못한다")
	}

	// 새 동작.
	fixed := open(t)
	if err := fixed.Cleanup(dedupRetain); err != nil {
		t.Fatalf("cleanup(dedupRetain): %v", err)
	}
	if seen, err := fixed.IsSeen(src, itemID); err != nil {
		t.Fatalf("IsSeen: %v", err)
	} else if !seen {
		t.Fatalf("dedupRetain=%s 인데 120일 전 행이 지워졌다 — 재발송된다", dedupRetain)
	}
}
