#!/usr/bin/env bash
# 일회성: bot_seen 의 dedup 네임스페이스를 합성 이름에서 하위 소스 이름으로 옮긴다.
#
# ⚠ 2026-09-08 사고: Runner 가 bot_seen 키로 MultiSource 의 이름
#   (`multi[하위소스 목록]`)을 썼다. 크롤러를 하나 켜거나 끄면 그 이름이 바뀌어
#   남은 소스 전부의 seen 이력이 고아가 되고, 다음 폴에서 백로그가 전부 신규로
#   보인 뒤 MaxItemsPerPoll(10) 을 넘는 분량이 **발송 없이 seen 처리돼 영구 유실**
#   된다. 이 DB 에 이미 네임스페이스가 둘로 갈라져 있었다:
#     multi[moel]                    160행 (2026-05-12~09-07)
#     multi[kosha,...,moel] 7개 소스   80행 (08-30~09-02)
#   2026-08-30 한 번의 실행에서 70행이 새 키로 들어갔는데 발송은 10건뿐이었다.
#
#   프레임워크 v0.6.0 이 하위 소스별 dedup 으로 바뀌었으므로, 새 바이너리를 올리기
#   **전에** 기존 행을 새 키로 옮겨 둬야 한다. 안 그러면 첫 폴이 정확히 그 사고를
#   한 번 더 낸다.
#
# 키 유도: item_id 가 `<소스이름>:<원본id>` 형식이다(internal/source/adapter.go:57).
#   접두사가 없는 옛 행(2026-05-12 초기 16건)은 그 시점 유일 소스였던 moel 로 본다.
#
# 사용법:
#   scripts/migrate-dedup-namespace.sh data/safety-alarm.db          # 미리보기
#   scripts/migrate-dedup-namespace.sh data/safety-alarm.db --apply  # 적용
set -euo pipefail

DB="${1:?사용법: $0 <db경로> [--apply]}"
APPLY="${2:-}"
LEGACY_SOURCE="moel"   # 접두사 없는 초기 행의 출처

if [ ! -f "$DB" ]; then
  echo "DB 가 없다: $DB" >&2
  exit 1
fi

echo "=== 현재 네임스페이스 ==="
sqlite3 "$DB" "SELECT source, COUNT(*) FROM bot_seen GROUP BY source;" | sed 's/^/  /'

echo "=== 옮길 결과(미리보기) ==="
sqlite3 "$DB" "
  SELECT CASE WHEN instr(item_id, ':') > 0
              THEN substr(item_id, 1, instr(item_id, ':') - 1)
              ELSE '$LEGACY_SOURCE' END AS new_source,
         COUNT(*)
  FROM bot_seen
  WHERE source LIKE 'multi[%'
  GROUP BY new_source;" | sed 's/^/  /'

if [ "$APPLY" != "--apply" ]; then
  echo
  echo "미리보기다. 적용하려면 --apply 를 붙일 것."
  exit 0
fi

BAK="$DB.bak-$(date +%Y%m%d-%H%M%S)"
cp "$DB" "$BAK"
echo "백업: $BAK"

# ⚠ UPDATE 로 옮기면 (bot_key, source, item_id) PK 가 충돌할 수 있다
#   (두 합성 네임스페이스에 같은 item_id 가 있는 경우). INSERT OR IGNORE 로 새 행을
#   만든 뒤 옛 행을 지운다 — 그러면 충돌분은 조용히 하나로 합쳐진다.
sqlite3 "$DB" "
BEGIN;
INSERT OR IGNORE INTO bot_seen (bot_key, source, item_id, seen_at)
  SELECT bot_key,
         CASE WHEN instr(item_id, ':') > 0
              THEN substr(item_id, 1, instr(item_id, ':') - 1)
              ELSE '$LEGACY_SOURCE' END,
         item_id,
         seen_at
  FROM bot_seen
  WHERE source LIKE 'multi[%';
DELETE FROM bot_seen WHERE source LIKE 'multi[%';
COMMIT;
"

# ⚠ 합쳐진 건수를 반드시 보고한다. 두 옛 네임스페이스에 같은 아이템이 있으면
#   새 키에서 하나로 합쳐지는데, 조용히 넘어가면 "행이 줄었다" 를 사고로 오해하거나
#   반대로 진짜 유실을 못 알아챈다.
AFTER=$(sqlite3 "$DB" "SELECT COUNT(*) FROM bot_seen;")
BEFORE=$(sqlite3 "$BAK" "SELECT COUNT(*) FROM bot_seen;")
MERGED=$((BEFORE - AFTER))
echo "행 수: ${BEFORE} → ${AFTER} (중복 병합 ${MERGED}건 — 두 옛 네임스페이스에 같은 아이템이 있던 경우)"

echo "=== 적용 후 ==="
sqlite3 "$DB" "SELECT source, COUNT(*) FROM bot_seen GROUP BY source;" | sed 's/^/  /'
echo
echo "완료. 이제 프레임워크 v0.6.0 바이너리를 올려도 백로그가 신규로 보이지 않는다."
