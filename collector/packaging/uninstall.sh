#!/bin/sh
# 사용자별 Collector 정리(#42). 기본은 우리가 설치한 Hook과 자동 시작만 지우고 설정·토큰·큐는 남긴다.
#   agent-office-collector-uninstall [--purge] [--yes]
#   --purge: 설정(장치 토큰 포함)·상태·큐도 지운다. Claude·Codex 원본 기록은 절대 지우지 않는다.
set -eu
PURGE=0
YES=0
for arg in "$@"; do
  case "$arg" in
    --purge) PURGE=1 ;;
    --yes) YES=1 ;;
    *) echo "사용법: agent-office-collector-uninstall [--purge] [--yes]" >&2; exit 2 ;;
  esac
done
BIN=$(command -v agent-office-collector || echo "$(dirname "$0")/agent-office-collector")
if [ "$YES" -ne 1 ]; then
  printf 'Agent Office Hook·자동 시작을 지웁니다%s. 계속할까요? [y/N] ' "$( [ "$PURGE" -eq 1 ] && echo '(설정·토큰·큐 포함)')"
  read -r answer
  [ "$answer" = y ] || [ "$answer" = Y ] || { echo "취소"; exit 1; }
fi
# 실행 중인 수집을 멈추고, 우리가 표시한 항목만 지운다(남의 Hook·서비스는 그대로).
"$BIN" hooks uninstall --tool claude --yes >/dev/null 2>&1 || true
"$BIN" service uninstall >/dev/null 2>&1 || true
if [ "$PURGE" -eq 1 ]; then
  # Core의 경로 규칙(collector/core/internal/paths)과 같다. 우리 폴더 이름으로 끝날 때만 지운다.
  if [ -n "${AGENT_OFFICE_COLLECTOR_HOME:-}" ]; then
    set -- "$AGENT_OFFICE_COLLECTOR_HOME/config" "$AGENT_OFFICE_COLLECTOR_HOME/state"
  elif [ "$(uname -s)" = Darwin ]; then
    set -- "$HOME/Library/Application Support/AgentOffice/Collector"
  else
    set -- "${XDG_CONFIG_HOME:-$HOME/.config}/agent-office/collector" \
      "${XDG_STATE_HOME:-$HOME/.local/state}/agent-office/collector"
  fi
  for dir in "$@"; do
    case "$dir" in
      */agent-office/collector | */AgentOffice/Collector | */config | */state) rm -rf -- "$dir" ;;
    esac
  done
fi
echo "정리 완료$( [ "$PURGE" -eq 1 ] && echo '(설정·토큰·큐 삭제)')"
