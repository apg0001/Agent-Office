#!/usr/bin/env bash
# Linux 데스크톱 패키지(GUI·트레이) 빌드(#159). Go 1.24 컨테이너 안에서 실행한다(호스트에 Go 불필요).
#
#   collector/packaging/build-desktop-linux.sh <버전(SemVer)> [출력 디렉터리]
#
# 결과: 실행 중인 호스트 아키텍처(amd64 또는 arm64)용 agent-office-collector-desktop deb·rpm.
# GUI는 cgo·GTK·webkit2gtk가 필요해 교차 빌드하지 않는다(arm64는 arm64 러너에서 실행).
# Core 패키지(agent-office-collector)는 build.sh가 만든다. 서명은 하지 않는다.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VERSION=${1:?버전}
OUT=${2:-$ROOT/dist/collector-$VERSION}
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "SemVer가 아님: $VERSION" >&2; exit 2; }
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
GO_IMAGE=golang:1.24-bookworm
NFPM_IMAGE=goreleaser/nfpm:v2.41.1
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) echo "지원하지 않는 아키텍처: $(uname -m)" >&2; exit 2 ;;
esac
# nfpm이 deb·rpm 버전에 쓰는 형식(SemVer 사전 배포 `-` → `~`)과 같게 Core 의존 버전을 맞춘다.
DEPVER=${VERSION/-/\~}
STAGE=$(mktemp -d "$OUT/.stage-desktop-XXXXXX")
trap 'rm -rf "$STAGE"' EXIT

# apt 설치에 root가 필요하다. 산출물은 끝에 호출한 사용자 소유로 돌린다.
docker run --rm ${DOCKER_CPUS:+--cpus "$DOCKER_CPUS"} -e VERSION="$VERSION" -e OWNER="$(id -u):$(id -g)" \
  -v "$ROOT/collector:/src/collector:ro" -v "$STAGE:/stage" "$GO_IMAGE" sh -euc '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq >/dev/null
    apt-get install -y -qq --no-install-recommends libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config >/dev/null
    dpkg-query -W -f="\${Package} \${Version}\n" libgtk-3-dev libwebkit2gtk-4.1-dev
    cp -r /src/collector/gui /tmp/gui && cd /tmp/gui
    go build -trimpath -tags desktop,production,webkit2_41 -ldflags "-s -w" -o /stage/agent-office-collector-gui .
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /stage/agent-office-collector-tray ./cmd/agent-office-collector-tray
    /stage/agent-office-collector-tray version
    chown -R "$OWNER" /stage
  '
sed "s/@DEPVER@/$DEPVER/g" "$ROOT/collector/packaging/nfpm-desktop.yaml" >"$STAGE/nfpm-desktop.yaml"
for packager in deb rpm; do
  docker run --rm -u "$(id -u):$(id -g)" -v "$ROOT/collector/packaging:/pkg:ro" -v "$STAGE:/stage:ro" -v "$OUT:/out" \
    -e VERSION="$VERSION" -e ARCH="$ARCH" "$NFPM_IMAGE" package --config /stage/nfpm-desktop.yaml --packager "$packager" --target /out/ >/dev/null
done
# GitHub Release가 파일 이름의 `~`를 `.`으로 바꾸므로 미리 맞춘다(build.sh와 같음).
for f in "$OUT"/agent-office-collector-desktop*~*.deb "$OUT"/agent-office-collector-desktop*~*.rpm; do
  if [ -e "$f" ]; then mv -- "$f" "${f//\~/.}"; fi
done
ls -1 "$OUT" | grep '^agent-office-collector-desktop'
