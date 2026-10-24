#!/usr/bin/env bash
# 공식 이미지 방식과 소스 빌드 방식을 같은 호스트에서 깨끗하게 설치해 동작을 비교한다(#69).
#
#   tests/install/compare.sh <서버 이미지> <웹 이미지>
#
# 이미지 방식: 주어진 이미지(공식 이미지 또는 CI가 만든 것과 같은 Dockerfile 빌드)로 운영 Compose.
# 소스 방식: 운영 Compose + deploy/compose.build.yaml로 이 저장소에서 빌드.
# 각각 새 볼륨으로 띄우고 마이그레이션 → bootstrap 토큰 → probe.py 결과를 비교한 뒤 모두 지운다.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SERVER_IMAGE=${1:?서버 이미지}
WEB_IMAGE=${2:?웹 이미지}
WORK=$(mktemp -d)
cleanup() {
  local project
  for project in ao-parity-image ao-parity-source; do
    [ -f "$WORK/$project.env" ] || continue
    docker compose -p "$project" --env-file "$WORK/$project.env" -f "$ROOT/deploy/compose.production.yaml" \
      -f "$ROOT/deploy/compose.build.yaml" down --volumes >/dev/null 2>&1 || true
  done
  rm -rf "$WORK"
}
trap cleanup EXIT

run() {
  local project=$1 port=$2 server=$3 web=$4 build=$5; shift 5
  cat >"$WORK/$project.env" <<ENV
POSTGRES_PASSWORD=$(head -c 18 /dev/urandom | base64 | tr -dc A-Za-z0-9)
AGENT_OFFICE_SERVER_IMAGE=$server
AGENT_OFFICE_WEB_IMAGE=$web
AGENT_OFFICE_HTTP_PORT=$port
AGENT_OFFICE_HTTPS_PORT=$((port + 1))
ENV
  local compose=(docker compose -p "$project" --env-file "$WORK/$project.env" -f "$ROOT/deploy/compose.production.yaml" "$@")
  local up=(up -d --wait --wait-timeout 400)
  [ "$build" = build ] && up+=(--build)
  "${compose[@]}" "${up[@]}" >/dev/null 2>&1
  "${compose[@]}" exec -T server alembic -c /app/alembic.ini upgrade head >/dev/null 2>&1
  local token
  token=$("${compose[@]}" exec -T server agent-office-bootstrap | tail -1)
  python3 "$ROOT/tests/install/probe.py" "http://127.0.0.1:$port" "$token" >"$WORK/$project.json"
}

echo "[parity] 이미지 방식 설치"
run ao-parity-image 18180 "$SERVER_IMAGE" "$WEB_IMAGE" no-build
echo "[parity] 소스 빌드 방식 설치"
run ao-parity-source 18280 agent-office-server:source agent-office-web:source build \
  -f "$ROOT/deploy/compose.build.yaml"
if diff -u "$WORK/ao-parity-image.json" "$WORK/ao-parity-source.json"; then
  echo "[parity] 동일"
  cat "$WORK/ao-parity-image.json"
else
  echo "[parity] 차이 있음"
  exit 1
fi
