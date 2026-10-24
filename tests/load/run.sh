#!/usr/bin/env bash
# 운영 Compose를 새 볼륨으로 띄우고 부하·재연결을 실측한다(#63).
#
#   tests/load/run.sh <서버 이미지> <웹 이미지> [office_load.py 인자...]
#
# 결과: 표준 출력에 부하 결과 JSON과 컨테이너 자원 최대값. 끝나면 스택과 볼륨을 지운다.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SERVER_IMAGE=${1:?서버 이미지}
WEB_IMAGE=${2:?웹 이미지}
shift 2
WORK=$(mktemp -d)
PROJECT=ao-load
PORT=${AGENT_OFFICE_LOAD_PORT:-18380}
cat >"$WORK/env" <<ENV
POSTGRES_PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -dc A-Za-z0-9)
AGENT_OFFICE_SERVER_IMAGE=$SERVER_IMAGE
AGENT_OFFICE_WEB_IMAGE=$WEB_IMAGE
AGENT_OFFICE_HTTP_PORT=$PORT
AGENT_OFFICE_HTTPS_PORT=$((PORT + 1))
ENV
compose() { docker compose -p "$PROJECT" --env-file "$WORK/env" -f "$ROOT/deploy/compose.production.yaml" "$@"; }
cleanup() {
  if [ -n "${SAMPLER:-}" ]; then kill "$SAMPLER" 2>/dev/null || true; fi
  [ "${KEEP:-0}" = 1 ] || compose down --volumes >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

compose up -d --wait --wait-timeout 400 >/dev/null 2>&1
compose exec -T server alembic -c /app/alembic.ini upgrade head >/dev/null 2>&1
TOKEN=$(compose exec -T server agent-office-bootstrap | tail -1)
COOKIE=$(curl -s -D - -o /dev/null -H 'Content-Type: application/json' -H "Origin: http://127.0.0.1:$PORT" \
  -d "{\"bootstrap_token\":\"$TOKEN\",\"email\":\"load@example.com\",\"password\":\"load test passphrase\",\"display_name\":\"Load\"}" \
  "http://127.0.0.1:$PORT/api/v1/setup/admin" | sed -n 's/^[Ss]et-[Cc]ookie: ao_session=\([^;]*\).*/\1/p' | head -1)
[ -n "$COOKIE" ] || { echo "관리자 세션을 만들지 못함" >&2; exit 1; }
echo "$COOKIE" >"$WORK/cookie"

# 컨테이너 자원 표본(2초 간격).
mapfile -t CONTAINERS < <(compose ps -q)
(
  while true; do
    docker stats --no-stream --format '{{.Name}} {{.CPUPerc}} {{.MemUsage}}' \
      "${CONTAINERS[@]}" 2>/dev/null >>"$WORK/stats" || true
    sleep 2
  done
) &
SAMPLER=$!

# 장애 주입: RESTART_AFTER초 뒤 서버 컨테이너를 재시작한다(재연결·재전송 회복 측정).
if [ -n "${RESTART_AFTER:-}" ]; then
  ( sleep "$RESTART_AFTER"; compose restart server >/dev/null 2>&1; echo "[load] 서버 재시작 완료" >&2 ) &
fi

# 브라우저 FPS: FPS_SECONDS를 주면 부하가 도는 동안 Playwright Chromium으로 잰다.
if [ -n "${FPS_SECONDS:-}" ]; then
  (
    sleep 20
    docker run --rm --network host --ipc=host -v "$ROOT/tests/load:/load:ro" -w /tmp/fps \
      mcr.microsoft.com/playwright:v1.49.1-noble sh -c \
      "npm init -y >/dev/null && npm i -s playwright@1.49.1 >/dev/null 2>&1 && cp /load/fps.mjs . && node fps.mjs http://localhost:$PORT $(cat "$WORK/cookie") $FPS_SECONDS" \
      >"$WORK/fps.json" 2>"$WORK/fps.err" || true
  ) &
  FPS_JOB=$!
fi

docker run --rm --network host -v "$ROOT/tests/load:/load:ro" agent-office-test:py313 \
  python /load/office_load.py --base "http://127.0.0.1:$PORT" --cookie "$(cat "$WORK/cookie")" "$@"

if [ -n "${FPS_JOB:-}" ]; then
  wait "$FPS_JOB" || true
  echo "--- 브라우저 FPS"
  cat "$WORK/fps.json" 2>/dev/null || tail -5 "$WORK/fps.err"
fi

kill "$SAMPLER" 2>/dev/null || true
SAMPLER=""
echo "--- 컨테이너별 최대 CPU%·메모리"
awk '{gsub("%","",$2); name=$1; sub(/^ao-load-/,"",name); sub(/-1$/,"",name);
      if ($2+0 > cpu[name]) cpu[name]=$2+0; if (!(name in first)) first[name]=$3; mem[name]=$3}
     END {for (n in cpu) printf "%s cpu_max=%.1f%% mem_first=%s mem_last=%s\n", n, cpu[n], first[n], mem[n]}' \
  "$WORK/stats" | sort
