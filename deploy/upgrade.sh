#!/usr/bin/env bash
# Agent Office 서버·웹 이미지 업그레이드(#60). 관리자가 이 호스트에서 직접 실행한다.
#
#   deploy/upgrade.sh --server <이미지@digest> --web <이미지@digest> [--env-file deploy/.env.production] [--yes]
#
# 순서: 계획 표시 → 수동 승인 → 배포 전 백업 → 새 이미지로 최신 백업 복원 DB에 마이그레이션 사전 실행
#       → 이미지 교체·마이그레이션 → health·smoke → 실패하면 롤백(필요하면 DB 다운그레이드).
# 출력에는 이미지 이름·마이그레이션 버전·결과 코드만 나온다(DB 암호·키는 다루지 않음).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
COMPOSE_FILE="$ROOT/deploy/compose.production.yaml"
ENV_FILE="$ROOT/deploy/.env.production"
PROJECT="agent-office"
SERVER_IMAGE=""
WEB_IMAGE=""
ASSUME_YES=0
HISTORY="$ROOT/deploy/.deploy-history"

usage() {
  sed -n '2,4p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER_IMAGE=$2; shift 2 ;;
    --web) WEB_IMAGE=$2; shift 2 ;;
    --env-file) ENV_FILE=$2; shift 2 ;;
    --project) PROJECT=$2; shift 2 ;;
    --yes) ASSUME_YES=1; shift ;;
    *) usage ;;
  esac
done
if [ -z "$SERVER_IMAGE" ] || [ -z "$WEB_IMAGE" ] || [ ! -f "$ENV_FILE" ]; then
  usage
fi

compose() { docker compose -p "$PROJECT" --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }
log() { printf '[upgrade] %s\n' "$*"; }
record() { printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" >>"$HISTORY"; }
env_value() { sed -n "s/^$1=//p" "$ENV_FILE" | tail -1; }

# 이미지가 기대하는 DB 마이그레이션 head. 백업 도구가 없는 예전 이미지도 Alembic으로 읽는다.
expected_revision() {
  docker run --rm --entrypoint python "$1" -c "
from alembic.config import Config
from alembic.script import ScriptDirectory
c = Config(); c.set_main_option('script_location', 'agent_office.db:migrations')
print(ScriptDirectory.from_config(c).get_current_head())"
}

db_revision() {
  compose exec -T server python -c "
from agent_office.db.engine import get_engine
from sqlalchemy import text
with get_engine().connect() as c:
    print(c.execute(text('SELECT version_num FROM alembic_version')).scalar() or '')" 2>/dev/null || echo ""
}

# 새 서버 이미지로 일회성 명령을 실행한다(서비스 환경 변수·볼륨 그대로, 다른 서비스는 띄우지 않음).
with_new_image() { AGENT_OFFICE_SERVER_IMAGE="$SERVER_IMAGE" compose run --rm --no-deps -T server "$@"; }

smoke() {
  local bind port base
  bind=$(env_value AGENT_OFFICE_BIND_ADDRESS); bind=${bind:-127.0.0.1}
  [ "$bind" = "0.0.0.0" ] && bind=127.0.0.1
  port=$(env_value AGENT_OFFICE_HTTP_PORT); port=${port:-8080}
  base="http://$bind:$port"
  # 준비 상태(#59)가 없는 예전 이미지로 롤백한 경우에는 생존 확인으로 대신한다.
  ready() {
    local code
    code=$(curl -s -o /dev/null -w '%{http_code}' "$base/api/v1/system/ready")
    [ "$code" = 200 ] || { [ "$code" = 404 ] && curl -fsS -o /dev/null "$base/api/v1/system/health"; }
  }
  for _ in $(seq 1 30); do
    ready && break
    sleep 2
  done
  ready && curl -fsS -o /dev/null "$base/" && curl -fsS -o /dev/null "$base/setup"
}

set_images() {
  local server=$1 web=$2 tmp
  tmp=$(mktemp "$ENV_FILE.XXXXXX")
  sed -e "s|^AGENT_OFFICE_SERVER_IMAGE=.*|AGENT_OFFICE_SERVER_IMAGE=$server|" \
      -e "s|^AGENT_OFFICE_WEB_IMAGE=.*|AGENT_OFFICE_WEB_IMAGE=$web|" "$ENV_FILE" >"$tmp"
  chmod --reference="$ENV_FILE" "$tmp"
  mv "$tmp" "$ENV_FILE"
}

OLD_SERVER=$(env_value AGENT_OFFICE_SERVER_IMAGE)
OLD_WEB=$(env_value AGENT_OFFICE_WEB_IMAGE)
log "새 이미지 확인"
docker image inspect "$SERVER_IMAGE" >/dev/null 2>&1 || docker pull -q "$SERVER_IMAGE" >/dev/null
docker image inspect "$WEB_IMAGE" >/dev/null 2>&1 || docker pull -q "$WEB_IMAGE" >/dev/null
OLD_REV=$(expected_revision "$OLD_SERVER")
NEW_REV=$(expected_revision "$SERVER_IMAGE")
DB_REV=$(db_revision)

cat <<PLAN
[upgrade] 배포 계획
  서버: $OLD_SERVER
     → $SERVER_IMAGE
  웹:   $OLD_WEB
     → $WEB_IMAGE
  DB 마이그레이션: 현재 ${DB_REV:-알 수 없음}, 이전 이미지 기대 $OLD_REV, 새 이미지 기대 $NEW_REV
PLAN
if [ "$ASSUME_YES" -ne 1 ]; then
  printf '[upgrade] 진행하려면 deploy 를 입력하세요: '
  read -r answer
  [ "$answer" = "deploy" ] || { log "취소"; exit 1; }
fi

# 백업 기능 이전 이미지로 처음 만든 볼륨은 root 소유라 비루트 서버가 쓰지 못한다. 소유자만 맞춘다.
AGENT_OFFICE_SERVER_IMAGE="$SERVER_IMAGE" compose run --rm --no-deps -T -u 0 --cap-add CHOWN \
  --entrypoint chown server 65532:65532 /backups /backups-offsite >/dev/null

log "1/5 배포 전 백업(새 이미지의 백업 도구 사용)"
with_new_image agent-office-backup create || { log "백업 실패: 배포 중단"; record "aborted backup_failed $SERVER_IMAGE"; exit 1; }

log "2/5 최신 백업을 임시 DB에 복원하고 새 마이그레이션 사전 실행"
with_new_image agent-office-backup migrate-check || {
  log "마이그레이션 사전 실행 실패: 배포 중단(운영 DB는 그대로)"
  record "aborted migration_dry_run_failed $SERVER_IMAGE"
  exit 1
}

cp -p "$ENV_FILE" "$ENV_FILE.bak.$(date -u +%Y%m%dT%H%M%SZ)"

rollback() {
  log "롤백 시작"
  local current
  current=$(db_revision)
  if [ -n "$current" ] && [ "$current" != "$OLD_REV" ]; then
    log "DB를 이전 이미지 버전($OLD_REV)으로 다운그레이드(새 이미지의 다운그레이드 스크립트 사용)"
    if ! compose exec -T server alembic -c /app/alembic.ini downgrade "$OLD_REV"; then
      log "다운그레이드 실패. docs/operations/deploy.md의 '백업으로 복원' 절차를 따르세요."
      record "rollback_failed downgrade $SERVER_IMAGE"
      exit 1
    fi
  fi
  set_images "$OLD_SERVER" "$OLD_WEB"
  compose up -d --wait --wait-timeout 300 >/dev/null 2>&1
  if smoke; then
    log "롤백 완료: 이전 이미지로 동작 중"
    record "rolled_back $SERVER_IMAGE -> $OLD_SERVER"
  else
    log "롤백 후에도 점검 실패. docs/operations/deploy.md의 장애 절차를 따르세요."
    record "rollback_unhealthy $OLD_SERVER"
  fi
  exit 1
}

log "3/5 이미지 교체"
set_images "$SERVER_IMAGE" "$WEB_IMAGE"
compose up -d --wait --wait-timeout 300 >/dev/null 2>&1 || rollback

log "4/5 마이그레이션 적용"
compose exec -T server alembic -c /app/alembic.ini upgrade head || rollback

log "5/5 health·smoke"
smoke || rollback

log "배포 완료 (DB $(db_revision))"
record "deployed $OLD_SERVER -> $SERVER_IMAGE"
