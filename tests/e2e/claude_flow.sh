#!/usr/bin/env bash
# 실제 Claude Code → Hook → Collector → 서버 → 사무실 실시간까지의 Linux 흐름(#62 Linux 부분).
#
#   tests/e2e/claude_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
#   SCENARIO=subagent tests/e2e/claude_flow.sh …   # 계획(TodoWrite)·하위 에이전트(Task)까지
#
# 안전장치: 사용자 ~/.claude/settings.json은 바꾸지 않는다(--setting-sources project + 임시 --settings,
# 실행 전후 SHA-256 비교). Collector는 임시 홈을 쓰고 transcript 감시 위치는 빈 폴더라
# 사용자의 다른 Claude 세션을 수집하지 않는다. 프롬프트에 넣은 표식 문자열이 서버 DB에 없어야 한다.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
COLLECTOR=$(readlink -f "${3:?Collector 실행 파일}")
WORK=$(mktemp -d)
PORT=${AGENT_OFFICE_E2E_PORT:-18680}
BASE="http://127.0.0.1:$PORT"
SENTINEL="AO_E2E_SENTINEL_$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
cat >"$WORK/env" <<ENV
POSTGRES_PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -dc A-Za-z0-9)
AGENT_OFFICE_SERVER_IMAGE=${1:?서버 이미지}
AGENT_OFFICE_WEB_IMAGE=${2:?웹 이미지}
AGENT_OFFICE_HTTP_PORT=$PORT
AGENT_OFFICE_HTTPS_PORT=$((PORT + 1))
ENV
compose() { docker compose -p ao-claude-e2e --env-file "$WORK/env" -f "$ROOT/deploy/compose.production.yaml" "$@"; }
export AGENT_OFFICE_COLLECTOR_HOME="$WORK/collector-home"
cleanup() {
  [ -n "${RUN_PID:-}" ] && kill "$RUN_PID" 2>/dev/null && wait "$RUN_PID" 2>/dev/null
  compose down --volumes >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
log() { printf '[claude-e2e] %s\n' "$*"; }

before=$(sha256sum "$HOME/.claude/settings.json" 2>/dev/null | cut -d' ' -f1 || echo none)

compose up -d --wait --wait-timeout 400 >/dev/null 2>&1
compose exec -T server alembic -c /app/alembic.ini upgrade head >/dev/null 2>&1
TOKEN=$(compose exec -T server agent-office-bootstrap | tail -1)
COOKIE=$(curl -s -D - -o "$WORK/admin.json" -H 'Content-Type: application/json' -H "Origin: $BASE" \
  -d "{\"bootstrap_token\":\"$TOKEN\",\"email\":\"e2e@example.com\",\"password\":\"e2e test passphrase\",\"display_name\":\"E2E\"}" \
  "$BASE/api/v1/setup/admin" | sed -n 's/^[Ss]et-[Cc]ookie: ao_session=\([^;]*\).*/\1/p' | head -1)
CSRF=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['csrf_token'])" "$WORK/admin.json")
CODE=$(curl -s -X POST -H "Cookie: ao_session=$COOKIE" -H "X-CSRF-Token: $CSRF" -H "Origin: $BASE" \
  "$BASE/api/v1/devices/registration-codes" | python3 -c "import json,sys;print(json.load(sys.stdin)['code'])")
log "장치 등록"
printf '%s\n' "$CODE" | "$COLLECTOR" register --server "$BASE" --code-stdin --name pi-e2e >/dev/null

# transcript 감시를 빈 폴더로 돌려 Hook만 수집한다.
mkdir -p "$WORK/empty-projects"
CONFIG="$AGENT_OFFICE_COLLECTOR_HOME/config/config.json"
python3 - "$CONFIG" "$WORK/empty-projects" <<'PY'
import json, sys
path, root = sys.argv[1], sys.argv[2]
cfg = json.load(open(path))
cfg["claude_projects_dir"] = root
json.dump(cfg, open(path, "w"))
PY
"$COLLECTOR" run >"$WORK/run.log" 2>&1 &
RUN_PID=$!

# 이 실행에만 쓰는 Hook 설정. Collector 홈을 명령에 넣어 hook이 같은 spool을 쓰게 한다.
python3 - "$WORK/settings.json" "$COLLECTOR" "$AGENT_OFFICE_COLLECTOR_HOME" <<'PY'
import json, shlex, sys
out, exe, home = sys.argv[1:4]
command = f"AGENT_OFFICE_COLLECTOR_HOME={shlex.quote(home)} {shlex.quote(exe)} hook claude"
events = ["SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
          "PermissionRequest", "SubagentStart", "SubagentStop", "Stop", "SessionEnd"]
hooks = {e: [{"matcher": "*", "hooks": [{"type": "command", "command": command, "timeout": 5}]}] for e in events}
json.dump({"hooks": hooks}, open(out, "w"))
PY
mkdir -p "$WORK/workspace"
SCENARIO=${SCENARIO:-basic}
if [ "$SCENARIO" = subagent ]; then
  # Claude Code 2.1.x에서 TodoWrite는 지연 로딩 도구라 ToolSearch로 먼저 불러온다(실측).
  PROMPT="Call the TodoWrite tool now to create a todo list with exactly two items (first in_progress, second pending). Then use the Task tool to launch a general-purpose subagent whose only job is to run the shell command 'echo $SENTINEL > note.txt' with the Bash tool. When the subagent finishes, mark both todos completed and reply with the single word done."
  TOOLS="ToolSearch Task Bash TodoWrite"
else
  PROMPT="Run the shell command 'echo $SENTINEL > note.txt' with the Bash tool, then reply with the single word done."
  TOOLS="Bash"
fi
log "claude -p 실행($SCENARIO)"
(cd "$WORK/workspace" && claude -p "$PROMPT" \
  --setting-sources project --settings "$WORK/settings.json" --allowedTools "$TOOLS" --output-format json \
  >"$WORK/claude.json" 2>"$WORK/claude.err") || { log "claude 실패"; tail -3 "$WORK/claude.err"; exit 1; }

log "서버 반영 대기"
for _ in $(seq 1 30); do
  sessions=$(curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office" | python3 -c "import json,sys;print(len(json.load(sys.stdin)['sessions']))")
  [ "$sessions" -ge 1 ] && break
  sleep 2
done
sleep 4
curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office" >"$WORK/office.json"
AGENT=$(python3 -c "import json,sys;print(next(a['id'] for a in json.load(open(sys.argv[1]))['agents'] if a['role']=='lead'))" "$WORK/office.json")
curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office/agents/$AGENT/timeline?limit=50" >"$WORK/timeline.json"
curl -s -H "Cookie: ao_session=$COOKIE" "$BASE/api/v1/office/agents/$AGENT" >"$WORK/detail.json"

leak=$(compose exec -T postgres sh -c 'pg_dump -U agent_office agent_office' | grep -c "$SENTINEL" || true)
after=$(sha256sum "$HOME/.claude/settings.json" 2>/dev/null | cut -d' ' -f1 || echo none)
python3 - "$WORK/office.json" "$WORK/timeline.json" "$leak" "$before" "$after" "$("$COLLECTOR" status --json | python3 -c 'import json,sys;d=json.load(sys.stdin);print(json.dumps((d.get("run") or {}).get("counters",{})))')" "$WORK/detail.json" "$SCENARIO" <<'PY'
import json, sys
office, timeline = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
leak, before, after, counters = int(sys.argv[3]), sys.argv[4], sys.argv[5], json.loads(sys.argv[6])
detail, scenario = json.load(open(sys.argv[7])), sys.argv[8]
lead = next(a for a in office["agents"] if a["role"] == "lead")
result = {
    "scenario": scenario,
    "sessions": len(office["sessions"]),
    "agents": len(office["agents"]),
    "subagents_under_lead": sum(1 for a in office["agents"] if a["parent_agent_id"] == lead["id"]),
    "plan_steps": [s["status"] for s in detail.get("plan_steps", [])],
    "lifecycle": [s["lifecycle"] for s in office["sessions"]],
    "timeline_event_types": sorted({e["event_type"] for e in timeline["entries"]}),
    "sentinel_in_database": leak,
    "user_settings_unchanged": before == after,
    "collector_counters": {k: v for k, v in counters.items() if v},
}
print(json.dumps(result, ensure_ascii=False, indent=1))
ok = (result["sessions"] >= 1 and leak == 0 and before == after
      and "tool.started" in result["timeline_event_types"])
if scenario == "subagent":
    ok = ok and result["subagents_under_lead"] >= 1 and len(result["plan_steps"]) >= 1
sys.exit(0 if ok else 1)
PY
