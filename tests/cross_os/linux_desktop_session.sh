#!/usr/bin/env bash
# Linux 설치 실측 세션(#159): Xvfb + dbus-run-session + 최소 StatusNotifierWatcher에서 installer_smoke.py를 돌린다.
# 일회용 러너 전용(installer_smoke.py가 AGENT_OFFICE_SMOKE_DISPOSABLE=1을 요구).
#   tests/cross_os/linux_desktop_session.sh <버전> <Core deb> <desktop deb>
# 필요 패키지: xvfb dbus dbus-x11 python3-dbus python3-gi
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
export VERSION=$1 CORE_DEB=$2 DESKTOP_DEB=$3
WORK=$(mktemp -d)
export AO_SNI_ITEMS="$WORK/sni-items" AO_SNI_CLIENT="$ROOT/tests/poc/collector/desktop/sni_client.py" ROOT
cat >"$WORK/session.sh" <<'EOS'
set -e
Xvfb :99 -screen 0 1280x900x24 >/dev/null 2>&1 &
export DISPLAY=:99
/usr/bin/python3 "$ROOT/tests/poc/collector/desktop/sni_watcher.py" "$AO_SNI_ITEMS" &
sleep 2
/usr/bin/python3 "$ROOT/tests/cross_os/installer_smoke.py" "$VERSION" "$CORE_DEB" "$DESKTOP_DEB"
EOS
dbus-run-session -- bash "$WORK/session.sh"
