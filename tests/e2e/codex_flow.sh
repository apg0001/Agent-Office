#!/usr/bin/env bash
# 실제 Codex CLI → 사용자 범위 hooks.json(`hooks install --tool codex`) → Collector → 서버까지의 Linux 흐름(#170).
#
#   tests/e2e/codex_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
#
# 안전장치
# - 사용자 실제 ~/.codex는 쓰지 않는다: 임시 CODEX_HOME을 쓰고 실행 전후 ~/.codex/config.toml·hooks.json
#   SHA-256을 비교한다.
# - 자격 증명을 쓰지 않는다: 임시 CODEX_HOME에 가짜 API 키로 로그인하고 모델은 로컬 합성 서버
#   (tests/e2e/codex/mock_responses.py)다. 실제 OpenAI 요청은 없다.
# - Hook 신뢰를 우회하지 않는다(--dangerously-bypass-hook-trust 미사용). 사용자처럼 대화형 TUI의
#   "Trust all and continue"로 신뢰한다(tests/e2e/codex/tui_trust.py).
# - Collector는 임시 홈, transcript 감시는 빈 폴더. 프롬프트의 표식 문자열이 서버 DB에 없어야 한다.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
COLLECTOR=$(readlink -f "${3:?Collector 실행 파일}")
WORK=$(mktemp -d)
PORT=${AGENT_OFFICE_E2E_PORT:-18690}
MOCK_PORT=${AGENT_OFFICE_E2E_MOCK_PORT:-18695}
BASE="http://127.0.0.1:$PORT"
SENTINEL="AO_E2E_SENTINEL_$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
cat >"$WORK/env" <<ENV
POSTGRES_PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -dc A-Za-z0-9)
AGENT_OFFICE_SERVER_IMAGE=${1:?서버 이미지}
AGENT_OFFICE_WEB_IMAGE=${2:?웹 이미지}
AGENT_OFFICE_HTTP_PORT=$PORT
AGENT_OFFICE_HTTPS_PORT=$((PORT + 1))
ENV
compose() { docker compose -p ao-codex-e2e --env-file "$WORK/env" -f "$ROOT/deploy/compose.production.yaml" "$@"; }
export AGENT_OFFICE_COLLECTOR_HOME="$WORK/collector-home"
export CODEX_HOME="$WORK/codex-home"
export CODEX_SQLITE_HOME="$CODEX_HOME"
cleanup() {
  [ -n "${RUN_PID:-}" ] && kill "$RUN_PID" 2>/dev/null && wait "$RUN_PID" 2>/dev/null
  [ -n "${MOCK_PID:-}" ] && kill "$MOCK_PID" 2>/dev/null
  compose down --volumes >/dev/null 2>&1 || true
  if [ "${AO_E2E_KEEP:-}" = 1 ]; then echo "[codex-e2e] 작업 폴더 보존: $WORK" >&2; else rm -rf "$WORK"; fi
}
trap cleanup EXIT
log() { printf '[codex-e2e] %s\n' "$*"; }
digest() { sha256sum "$1" 2>/dev/null | cut -d' ' -f1 || echo none; }

before="$(digest "$HOME/.codex/config.toml") $(digest "$HOME/.codex/hooks.json")"

compose up -d --wait --wait-timeout 400 >/dev/null 2>&1
for c in $(compose ps -q); do docker update --cpus 1 "$c" >/dev/null; done
compose exec -T server alembic -c /app/alembic.ini upgrade head >/dev/null 2>&1
TOKEN=$(compose exec -T server agent-office-bootstrap | tail -1)
COOKIE=$(curl -s -D - -o "$WORK/admin.json" -H 'Content-Type: application/json' -H "Origin: $BASE" \
  -d "{\"bootstrap_token\":\"$TOKEN\",\"email\":\"e2e@example.com\",\"password\":\"e2e test passphrase\",\"display_name\":\"E2E\"}" \
  "$BASE/api/v1/setup/admin" | sed -n 's/^[Ss]et-[Cc]ookie: ao_session=\([^;]*\).*/\1/p' | head -1)
CSRF=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['csrf_token'])" "$WORK/admin.json")
CODE=$(curl -s -X POST -H "Cookie: ao_session=$COOKIE" -H "X-CSRF-Token: $CSRF" -H "Origin: $BASE" \
  "$BASE/api/v1/devices/registration-codes" | python3 -c "import json,sys;print(json.load(sys.stdin)['code'])")
log "장치 등록"
printf '%s\n' "$CODE" | "$COLLECTOR" register --server "$BASE" --code-stdin --name pi-codex-e2e >/dev/null

mkdir -p "$WORK/empty-projects"
python3 - "$AGENT_OFFICE_COLLECTOR_HOME/config/config.json" "$WORK/empty-projects" <<'PY'
import json, sys
path, root = sys.argv[1], sys.argv[2]
cfg = json.load(open(path))
cfg["claude_projects_dir"] = root
json.dump(cfg, open(path, "w"))
PY
"$COLLECTOR" run >"$WORK/run.log" 2>&1 &
RUN_PID=$!

log "임시 CODEX_HOME 준비(가짜 API 키)"
mkdir -p "$CODEX_HOME" "$WORK/workspace"
printf 'e2e-dummy-api-key\n' | codex login --with-api-key >/dev/null 2>&1

log "hooks install --tool codex (미리보기 → --yes)"
"$COLLECTOR" hooks install --tool codex >"$WORK/preview.txt"
grep -q -- '--yes' "$WORK/preview.txt"
[ ! -e "$CODEX_HOME/hooks.json" ]
"$COLLECTOR" hooks install --tool codex --yes >"$WORK/install.txt"
"$COLLECTOR" hooks status --tool codex --json >"$WORK/status.json"

log "codex exec(신뢰 전): Hook이 실행되면 안 됨"
python3 -I "$ROOT/tests/e2e/codex/mock_responses.py" "$MOCK_PORT" "$WORK/mock.log" &
MOCK_PID=$!
sleep 1
run_exec() {
  (cd "$WORK/workspace" && CODEX_API_KEY=dummy timeout 180 codex exec --skip-git-repo-check \
    -c "openai_base_url=\"http://127.0.0.1:$MOCK_PORT/v1\"" \
    "Run the shell command true once, then reply done. $SENTINEL" >/dev/null 2>"$WORK/exec-$1.err" </dev/null)
}
run_exec untrusted
sleep 16  # status.json은 15초마다 갱신된다
hooks_untrusted=$("$COLLECTOR" status --json | python3 -c 'import json,sys;print(((json.load(sys.stdin).get("run") or {}).get("counters") or {}).get("enqueued_hook",0))')
office_untrusted=$(curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office" | python3 -c "import json,sys;print(len(json.load(sys.stdin)['sessions']))")

log "대화형 TUI에서 Trust all"
python3 -I "$ROOT/tests/e2e/codex/tui_trust.py" "$CODEX_HOME" "$WORK/workspace"
trusted=$(grep -c 'trusted_hash' "$CODEX_HOME/config.toml" || true)

log "codex exec(신뢰 후)"
run_exec trusted
log "서버 반영 대기"
for _ in $(seq 1 30); do
  sessions=$(curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office" | python3 -c "import json,sys;print(len(json.load(sys.stdin)['sessions']))")
  [ "$sessions" -ge 1 ] && break
  sleep 2
done
sleep 4
curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office" >"$WORK/office.json"
# TUI 신뢰 세션도 SessionEnd를 남기므로 모든 lead 에이전트의 타임라인을 합친다.
echo '{"entries": []}' >"$WORK/timeline.json"
for AGENT in $(python3 -c "import json,sys;print(' '.join(a['id'] for a in json.load(open(sys.argv[1]))['agents'] if a['role']=='lead'))" "$WORK/office.json"); do
  curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office/agents/$AGENT/timeline?limit=50" >"$WORK/one.json"
  python3 - "$WORK/timeline.json" "$WORK/one.json" <<'PY'
import json, sys
acc, one = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
acc["entries"] += one.get("entries", [])
json.dump(acc, open(sys.argv[1], "w"))
PY
done

log "uninstall"
"$COLLECTOR" hooks uninstall --tool codex --yes >/dev/null
leak=$(compose exec -T postgres sh -c 'pg_dump -U agent_office agent_office' | grep -c "$SENTINEL" || true)
after="$(digest "$HOME/.codex/config.toml") $(digest "$HOME/.codex/hooks.json")"
python3 - "$WORK/office.json" "$WORK/timeline.json" "$leak" "$before" "$after" "$WORK/status.json" \
  "$hooks_untrusted" "$office_untrusted" "$trusted" "$CODEX_HOME/hooks.json" "$(codex --version 2>/dev/null)" <<'PY'
import json, sys
office, timeline = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
leak, before, after = int(sys.argv[3]), sys.argv[4], sys.argv[5]
status = json.load(open(sys.argv[6]))["tools"][0]
hooks_untrusted, office_untrusted, trusted = int(sys.argv[7]), int(sys.argv[8]), int(sys.argv[9])
remaining = json.load(open(sys.argv[10])) if __import__("os").path.exists(sys.argv[10]) else None
result = {
    "codex_version": sys.argv[11],
    "hook_status_after_install": status["hook_state"],
    "events_before_trust": {"enqueued_hook": hooks_untrusted, "sessions": office_untrusted},
    "trusted_hash_entries": trusted,
    "sessions": len(office["sessions"]),
    "providers": sorted({s.get("provider", "?") for s in office["sessions"]}),
    "timeline_event_types": sorted({e["event_type"] for e in timeline["entries"]}),
    "hooks_json_after_uninstall": remaining,
    "sentinel_in_database": leak,
    "user_codex_unchanged": before == after,
}
print(json.dumps(result, ensure_ascii=False, indent=1))
ok = (status["hook_state"] == "installed" and hooks_untrusted == 0 and office_untrusted == 0
      and trusted == 9 and result["sessions"] >= 1 and leak == 0 and before == after
      and "tool.started" in result["timeline_event_types"] and remaining in ({}, None))
sys.exit(0 if ok else 1)
PY
