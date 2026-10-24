#!/usr/bin/env bash
# Collector Core 배포물 빌드(#42). Go 1.24 컨테이너 안에서 실행한다(호스트에 Go 불필요).
#
#   collector/packaging/build.sh <버전(SemVer)> [출력 디렉터리]
#
# 결과: 6개 대상(linux·darwin·windows × amd64·arm64)의 정적 바이너리 압축본, Linux deb·rpm,
# SHA256SUMS. 서명은 하지 않는다(서명·공증은 docs/installation/collector-packages.md).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VERSION=${1:?버전}
OUT=${2:-$ROOT/dist/collector-$VERSION}
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "SemVer가 아님: $VERSION" >&2; exit 2; }
mkdir -p "$OUT"
GO_IMAGE=golang:1.24-bookworm
NFPM_IMAGE=goreleaser/nfpm:v2.41.1

# 업데이트 manifest 서명 공개 키(#44). 비우면 이 빌드는 자체 업데이트를 하지 않는다.
KEYS=${AGENT_OFFICE_UPDATE_KEYS:-}
docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/go-cache -e GOMODCACHE=/tmp/go-mod \
  -v "$ROOT/collector:/src/collector:ro" -v "$OUT:/out" "$GO_IMAGE" sh -euc "
    cp -r /src/collector/core /tmp/core && cd /tmp/core
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
      os=\${target%/*}; arch=\${target#*/}; ext=''; [ \"\$os\" = windows ] && ext=.exe
      dir=/tmp/stage/agent-office-collector-$VERSION-\$os-\$arch
      mkdir -p \"\$dir\"
      CGO_ENABLED=0 GOOS=\$os GOARCH=\$arch go build -trimpath \
        -ldflags \"-s -w -X main.version=$VERSION -X github.com/apg0001/Agent-Office/collector/core/internal/update.TrustedKeys=$KEYS\" -o \"\$dir/agent-office-collector\$ext\" ./cmd/agent-office-collector
      if [ \"\$os\" = windows ]; then
        cp /src/collector/packaging/uninstall.ps1 \"\$dir/\"
      else
        cp /src/collector/packaging/uninstall.sh \"\$dir/agent-office-collector-uninstall\"
        chmod 0755 \"\$dir/agent-office-collector-uninstall\"
      fi
      cp /src/collector/packaging/README.txt \"\$dir/\"
    done
    cd /tmp/stage
    for dir in */; do
      name=\${dir%/}
      case \"\$name\" in
        *windows*) (cd \"\$name\" && tar --version >/dev/null; python3 -c 'import shutil,sys; shutil.make_archive(sys.argv[1], \"zip\", sys.argv[2])' \"/out/\$name\" .) ;;
        *) tar -C /tmp/stage --owner=0 --group=0 --mtime='2026-01-01' -czf \"/out/\$name.tar.gz\" \"\$name\" ;;
      esac
      case \"\$name\" in *linux*) cp -r \"\$name\" \"/out/.stage-\$name\" ;; esac
    done
  "

for arch in amd64 arm64; do
  stage="$OUT/.stage-agent-office-collector-$VERSION-linux-$arch"
  for packager in deb rpm; do
    docker run --rm -u "$(id -u):$(id -g)" -v "$ROOT/collector/packaging:/pkg:ro" -v "$stage:/stage:ro" -v "$OUT:/out" \
      -e VERSION="$VERSION" -e ARCH="$arch" "$NFPM_IMAGE" package --config /pkg/nfpm.yaml --packager "$packager" --target /out/ >/dev/null
  done
  rm -rf "$stage"
done

(cd "$OUT" && sha256sum -- *.tar.gz *.zip *.deb *.rpm >SHA256SUMS)
echo "$OUT"
