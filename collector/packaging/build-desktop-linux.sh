#!/usr/bin/env bash
# Linux 데스크톱 패키지(GUI·트레이) 빌드(#159). Go 1.24 컨테이너 안에서 실행한다(호스트에 Go 불필요).
#
#   collector/packaging/build-desktop-linux.sh <버전(SemVer)> [출력 디렉터리]
#
# 결과: 실행 중인 호스트 아키텍처(amd64 또는 arm64)용 agent-office-collector-desktop deb·rpm.
# GUI는 cgo·GTK·webkit2gtk가 필요해 교차 빌드하지 않는다(arm64는 arm64 러너에서 실행).
# Ubuntu 22.04(glibc 2.35)에서도 돌도록 GUI는 ubuntu:22.04(jammy) 컨테이너에서 webkit2gtk-4.1로 빌드한다(#180).
# Go는 golang:1.24-bookworm 이미지의 공식 배포(정적 링크)를 복사해 쓴다. 빌드 뒤 GUI가 요구하는 GLIBC 심볼
# 최고 버전이 2.35 이하인지, 트레이가 정적 실행 파일인지 확인한다.
# Core 패키지(agent-office-collector)는 build.sh가 만든다. 서명은 하지 않는다.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VERSION=${1:?버전}
OUT=${2:-$ROOT/dist/collector-$VERSION}
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "SemVer가 아님: $VERSION" >&2; exit 2; }
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
GO_IMAGE=golang:1.24-bookworm
BUILD_IMAGE=ubuntu:22.04
MAX_GLIBC=2.35
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

# Go 도구 모음(공식 배포, 정적)을 꺼낸다.
cid=$(docker create "$GO_IMAGE")
docker cp "$cid:/usr/local/go" "$STAGE/go" >/dev/null
docker rm "$cid" >/dev/null
# apt 설치에 root가 필요하다. 산출물은 끝에 호출한 사용자 소유로 돌린다.
docker run --rm ${DOCKER_CPUS:+--cpus "$DOCKER_CPUS"} -e VERSION="$VERSION" -e OWNER="$(id -u):$(id -g)" -e MAX_GLIBC="$MAX_GLIBC" \
  -v "$ROOT/collector:/src/collector:ro" -v "$STAGE:/stage" "$BUILD_IMAGE" sh -euc '
    export DEBIAN_FRONTEND=noninteractive PATH=/stage/go/bin:$PATH GOTOOLCHAIN=local GOCACHE=/tmp/gocache GOPATH=/tmp/gopath
    apt-get update -qq >/dev/null
    apt-get install -y -qq --no-install-recommends ca-certificates gcc libc6-dev libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config binutils file >/dev/null
    (. /etc/os-release && echo "build base: $PRETTY_NAME")  # 하위 셸: os-release의 VERSION이 빌드 버전을 덮지 않게
    dpkg-query -W -f="\${Package} \${Version}\n" libc6 libgtk-3-dev libwebkit2gtk-4.1-dev
    go version
    cp -r /src/collector/gui /tmp/gui && cd /tmp/gui
    go build -trimpath -tags desktop,production,webkit2_41 -ldflags "-s -w" -o /stage/agent-office-collector-gui .
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /stage/agent-office-collector-tray ./cmd/agent-office-collector-tray
    /stage/agent-office-collector-tray version
    need=$(objdump -T /stage/agent-office-collector-gui | grep -o "GLIBC_[0-9][0-9.]*" | sed "s/GLIBC_//" | sort -V | tail -1)
    echo "GUI GLIBC 최고 요구 버전: $need (허용 $MAX_GLIBC)"
    top=$(printf "%s\n%s\n" "$need" "$MAX_GLIBC" | sort -V | tail -1)
    [ "$top" = "$MAX_GLIBC" ] || { echo "GLIBC $need 요구: Ubuntu 22.04에서 실행 불가" >&2; exit 1; }
    file /stage/agent-office-collector-tray | grep -q "statically linked" || { echo "트레이가 정적 실행 파일이 아님" >&2; exit 1; }
    chown -R "$OWNER" /stage
  '
rm -rf "$STAGE/go"
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
