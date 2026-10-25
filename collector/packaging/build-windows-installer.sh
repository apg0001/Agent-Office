#!/usr/bin/env bash
# Windows 설치 프로그램(NSIS) 빌드(#159). Linux의 Go 1.24 컨테이너에서 교차 빌드한다(호스트에 Go 불필요).
#
#   collector/packaging/build-windows-installer.sh <버전(SemVer)> [출력 디렉터리]
#
# Core·GUI(Wails v2 Windows 프런트엔드는 cgo 없음)·트레이(fyne.io/systray Windows는 순수 Go)를 windows/amd64로
# 빌드하고 Debian nsis(makensis 3.x)로 agent-office-collector-setup-<버전>-windows-amd64.exe를 만든다.
# 서명하지 않는다(SmartScreen 경고 우회는 docs/installation/collector-gui.md).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VERSION=${1:?버전}
OUT=${2:-$ROOT/dist/collector-$VERSION}
[[ "$VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)(-[0-9A-Za-z.]+)?$ ]] || { echo "SemVer가 아님: $VERSION" >&2; exit 2; }
VIVERSION="${BASH_REMATCH[1]}.0"
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
GO_IMAGE=golang:1.24-bookworm
KEYS=${AGENT_OFFICE_UPDATE_KEYS:-}
NAME="agent-office-collector-setup-$VERSION-windows-amd64.exe"

docker run --rm ${DOCKER_CPUS:+--cpus "$DOCKER_CPUS"} -e VERSION="$VERSION" -e VIVERSION="$VIVERSION" -e KEYS="$KEYS" \
  -e NAME="$NAME" -e OWNER="$(id -u):$(id -g)" \
  -v "$ROOT/collector:/src/collector:ro" -v "$OUT:/out" "$GO_IMAGE" sh -euc '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq >/dev/null
    apt-get install -y -qq --no-install-recommends nsis >/dev/null
    dpkg-query -W -f="\${Package} \${Version}\n" nsis
    cp -r /src/collector /tmp/c
    S=/tmp/stage && mkdir -p $S
    export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
    cd /tmp/c/core && go build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION -X github.com/apg0001/Agent-Office/collector/core/internal/update.TrustedKeys=$KEYS" \
      -o $S/agent-office-collector.exe ./cmd/agent-office-collector
    cd /tmp/c/gui && go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui" -o $S/agent-office-collector-gui.exe .
    go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$VERSION" -o $S/agent-office-collector-tray.exe ./cmd/agent-office-collector-tray
    cp /src/collector/packaging/uninstall.ps1 /src/collector/packaging/README.txt $S/
    makensis -V2 -INPUTCHARSET UTF8 -DVERSION="$VERSION" -DVIVERSION="$VIVERSION" -DSTAGE="$S" -DOUTFILE="/out/$NAME" \
      /src/collector/packaging/windows/installer.nsi
    chown "$OWNER" "/out/$NAME"
  '
ls -l "$OUT/$NAME"
