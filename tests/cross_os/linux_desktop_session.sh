#!/usr/bin/env bash
# Linux 설치 실측 세션(#159, #180): Xvfb + dbus-run-session + 최소 StatusNotifierWatcher에서 실측 스크립트를 돌린다.
# 일회용 러너 전용(스크립트가 AGENT_OFFICE_SMOKE_DISPOSABLE=1을 요구).
#   tests/cross_os/linux_desktop_session.sh <스크립트.py> [인자...]
#   예: tests/cross_os/linux_desktop_session.sh tests/cross_os/installer_smoke.py <버전> <Core deb> <desktop deb>
# 필요 패키지: xvfb dbus dbus-x11 python3-dbus python3-gi
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SCRIPT=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
shift
WORK=$(mktemp -d)
export AO_SNI_ITEMS="$WORK/sni-items" AO_SNI_CLIENT="$ROOT/tests/poc/collector/desktop/sni_client.py" ROOT SCRIPT
printf '%s\0' "$@" >"$WORK/args"
cat >"$WORK/session.sh" <<'EOS'
set -e
Xvfb :99 -screen 0 1280x900x24 >/dev/null 2>&1 &
export DISPLAY=:99
/usr/bin/python3 "$ROOT/tests/poc/collector/desktop/sni_watcher.py" "$AO_SNI_ITEMS" &
sleep 2
xargs -0 /usr/bin/python3 "$SCRIPT" <"$(dirname "$AO_SNI_ITEMS")/args"
EOS
dbus-run-session -- bash "$WORK/session.sh"
