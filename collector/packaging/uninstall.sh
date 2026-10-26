#!/bin/sh
# 사용자별 Collector 정리(#42, #159). 기본은 우리가 설치한 Hook과 자동 시작만 지우고 설정·토큰·큐는 남긴다.
#   agent-office-collector-uninstall [--purge] [--remove-app] [--yes]
#   --purge: 설정(장치 토큰 포함)·상태·큐·GUI 설정도 지운다. Claude·Codex 원본 기록은 절대 지우지 않는다.
#   --remove-app: (macOS .pkg 설치) 정리 뒤 /Applications/Agent Office Collector.app을 지운다.
#                 /Applications에 쓸 권한이 없으면 sudo로 그 앱만 지운다. sudo로 이 스크립트 전체를 실행하지 않는다.
set -eu
PURGE=0
YES=0
REMOVE_APP=0
for arg in "$@"; do
  case "$arg" in
    --purge) PURGE=1 ;;
    --yes) YES=1 ;;
    --remove-app) REMOVE_APP=1 ;;
    *) echo "사용법: agent-office-collector-uninstall [--purge] [--remove-app] [--yes]" >&2; exit 2 ;;
  esac
done
APP="/Applications/Agent Office Collector.app"
APP_ID=io.github.apg0001.agent-office.collector-gui
PKG_ID=io.github.apg0001.agent-office.collector
if [ "$REMOVE_APP" -eq 1 ] && [ "$(uname -s)" != Darwin ]; then
  echo "--remove-app은 macOS 전용입니다. Linux는 apt·dnf로 패키지를 제거하세요." >&2
  exit 2
fi
# 같은 폴더(압축본·deb), macOS 앱 번들(Contents/Resources → Contents/MacOS), PATH 순서로 Core를 찾는다.
HERE=$(dirname "$0")
if [ -x "$HERE/agent-office-collector" ]; then
  BIN="$HERE/agent-office-collector"
elif [ -x "$HERE/../MacOS/agent-office-collector" ]; then
  BIN="$HERE/../MacOS/agent-office-collector"
else
  BIN=$(command -v agent-office-collector || echo "$HERE/agent-office-collector")
fi
if [ "$YES" -ne 1 ]; then
  printf 'Agent Office Hook·자동 시작을 지웁니다%s%s. 계속할까요? [y/N] ' "$( [ "$PURGE" -eq 1 ] && echo '(설정·토큰·큐 포함)')" "$( [ "$REMOVE_APP" -eq 1 ] && echo ', 앱도 삭제')"
  read -r answer
  [ "$answer" = y ] || [ "$answer" = Y ] || { echo "취소"; exit 1; }
fi
# 실행 중인 수집을 멈추고, 우리가 표시한 항목만 지운다(남의 Hook·서비스는 그대로).
"$BIN" hooks uninstall --tool claude --yes >/dev/null 2>&1 || true
"$BIN" hooks uninstall --tool codex --yes >/dev/null 2>&1 || true
# --activate가 있어야 OS 서비스 관리자 등록 해제·실행 중인 Collector 중지까지 한다(없으면 정의 파일만 지움).
"$BIN" service uninstall --activate >/dev/null 2>&1 || true
# 트레이 도우미·GUI 창(#159): 이 사용자의 프로세스만 끝낸다(Core는 위에서 멈췄다).
# Linux 프로세스 이름은 15자로 잘리므로 -x 대신 명령줄로 찾는다.
if command -v pkill >/dev/null 2>&1; then
  pkill -u "$(id -u)" -f '(^|/)agent-office-collector-tray( |$)' >/dev/null 2>&1 || true
  pkill -u "$(id -u)" -f '(^|/)agent-office-collector-gui( |$)' >/dev/null 2>&1 || true
fi
# 트레이(GUI) 자동 시작(#43, #159): 소유 표시가 있는 일반 파일만 지운다.
for f in "${XDG_CONFIG_HOME:-$HOME/.config}/autostart/agent-office-collector-gui.desktop" \
  "$HOME/Library/LaunchAgents/io.github.apg0001.agent-office.collector-gui.plist"; do
  if [ -f "$f" ] && [ ! -L "$f" ] && grep -q 'agent-office-collector-gui:managed-autostart:v1' "$f"; then
    rm -f -- "$f"
  fi
done
if [ "$PURGE" -eq 1 ]; then
  # OS 보안 저장소의 장치 토큰 항목(#41)을 먼저 지운다. 서버 폐기는 하지 않는다(웹 /devices 또는 logout).
  "$BIN" logout --local-only >/dev/null 2>&1 || true
  # Core의 경로 규칙(collector/core/internal/paths)과 같다. 우리 폴더 이름으로 끝날 때만 지운다.
  # GUI 설정(gui.json: 서버 주소·PC 이름, tray.lock)은 Go os.UserConfigDir 아래 AgentOffice/CollectorGUI다.
  if [ -n "${AGENT_OFFICE_COLLECTOR_HOME:-}" ]; then
    set -- "$AGENT_OFFICE_COLLECTOR_HOME/config" "$AGENT_OFFICE_COLLECTOR_HOME/state" "$AGENT_OFFICE_COLLECTOR_HOME/gui"
  elif [ "$(uname -s)" = Darwin ]; then
    set -- "$HOME/Library/Application Support/AgentOffice/Collector" "$HOME/Library/Application Support/AgentOffice/CollectorGUI"
  else
    set -- "${XDG_CONFIG_HOME:-$HOME/.config}/agent-office/collector" \
      "${XDG_STATE_HOME:-$HOME/.local/state}/agent-office/collector" \
      "${XDG_CONFIG_HOME:-$HOME/.config}/AgentOffice/CollectorGUI"
  fi
  for dir in "$@"; do
    case "$dir" in
      */agent-office/collector | */AgentOffice/Collector | */AgentOffice/CollectorGUI | */config | */state | */gui) rm -rf -- "$dir" ;;
    esac
  done
fi
if [ "$REMOVE_APP" -eq 1 ]; then
  # 우리 번들 ID인 앱만 지운다(같은 이름의 다른 앱은 건드리지 않음).
  if [ -d "$APP" ] && [ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Contents/Info.plist" 2>/dev/null)" = "$APP_ID" ]; then
    if [ -w /Applications ] && [ -w "$APP" ]; then rm -rf -- "$APP"; else sudo rm -rf -- "$APP"; fi
  fi
  if pkgutil --pkg-info "$PKG_ID" >/dev/null 2>&1; then
    sudo pkgutil --forget "$PKG_ID" >/dev/null 2>&1 || true
  fi
fi
echo "정리 완료$( [ "$PURGE" -eq 1 ] && echo '(설정·토큰·큐 삭제)')$( [ "$REMOVE_APP" -eq 1 ] && echo '(앱 삭제)')"
