#!/usr/bin/env bash
# 운영 Compose를 새 볼륨으로 띄우고 실제 브라우저로 주요 화면 흐름을 확인한다.
#   tests/e2e/run.sh <서버 이미지> <웹 이미지>
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
PORT=${AGENT_OFFICE_E2E_PORT:-18580}
cat >"$WORK/env" <<ENV
POSTGRES_PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -dc A-Za-z0-9)
AGENT_OFFICE_SERVER_IMAGE=${1:?서버 이미지}
AGENT_OFFICE_WEB_IMAGE=${2:?웹 이미지}
AGENT_OFFICE_HTTP_PORT=$PORT
AGENT_OFFICE_HTTPS_PORT=$((PORT + 1))
ENV
compose() { docker compose -p ao-e2e --env-file "$WORK/env" -f "$ROOT/deploy/compose.production.yaml" "$@"; }
cleanup() { compose down --volumes >/dev/null 2>&1 || true; rm -rf "$WORK"; }
trap cleanup EXIT

compose up -d --wait --wait-timeout 400 >/dev/null 2>&1
compose exec -T server alembic -c /app/alembic.ini upgrade head >/dev/null 2>&1
TOKEN=$(compose exec -T server agent-office-bootstrap | tail -1)
# localhost는 브라우저가 안전한 출처로 보므로 Secure 쿠키가 평문 HTTP에서도 동작한다.
docker run --rm --network host --ipc=host -v "$ROOT/tests/e2e:/e2e:ro" -w /tmp/e2e \
  mcr.microsoft.com/playwright:v1.49.1-noble sh -c \
  "npm init -y >/dev/null && npm i -s playwright@1.49.1 >/dev/null 2>&1 && cp /e2e/web_flows.mjs . && node web_flows.mjs http://localhost:$PORT $TOKEN"
