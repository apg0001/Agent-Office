#!/usr/bin/env bash
# CI 전용 업데이트 서명 테스트 키(#180). 실제 배포 키가 아니다(실제 키는 collector-publish Environment 비밀 값).
#
#   collector/packaging/ci-test-key.sh <키 폴더>
#
# 비밀 키를 <키 폴더>/test.key(0600)에 만들고, 공개 키를 GITHUB_ENV의 AGENT_OFFICE_UPDATE_KEYS로 넘겨 같은 작업의
# 빌드(build.sh·build-windows-installer.sh·macos/build-pkg.sh)가 이 키를 신뢰하게 한다. 비밀 키는
# ci-test-manifest.sh가 서명 직후 지운다. go가 없으면 Docker golang:1.24-bookworm으로 실행한다.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DIR=${1:?키 폴더}
mkdir -p "$DIR"
chmod 700 "$DIR"
DIR=$(cd "$DIR" && pwd)
. "$ROOT/collector/packaging/release-tool.sh"
pub=$(release_tool "$DIR" keygen "$DIR/test.key")
[[ "$pub" =~ ^[A-Za-z0-9+/=]{40,}$ ]] || { echo "공개 키 형식이 아님" >&2; exit 1; }
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "AGENT_OFFICE_UPDATE_KEYS=$pub" >>"$GITHUB_ENV"
else
  echo "AGENT_OFFICE_UPDATE_KEYS=$pub"
fi
