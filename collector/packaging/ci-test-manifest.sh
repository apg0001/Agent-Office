#!/usr/bin/env bash
# CI 전용 업데이트 실측 세트(#180): <폴더>의 설치 파일로 SHA256SUMS·manifest.json(beta, 기준 주소
# http://127.0.0.1:8765)을 만들고 ci-test-key.sh의 테스트 키로 서명한 뒤 비밀 키를 지운다.
#
#   collector/packaging/ci-test-manifest.sh <키 폴더> <설치 파일 폴더> <버전>
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
KEYDIR=$(cd "${1:?키 폴더}" && pwd)
DIR=$(cd "${2:?설치 파일 폴더}" && pwd)
VERSION=${3:?버전}
BASE=http://127.0.0.1:8765
trap 'rm -rf "$KEYDIR"' EXIT
. "$ROOT/collector/packaging/release-tool.sh"
sum() { if command -v sha256sum >/dev/null 2>&1; then sha256sum -- "$@"; else shasum -a 256 -- "$@"; fi; }
(
  cd "$DIR"
  files=()
  for f in *.exe *.pkg *.deb *.rpm; do
    if [ -f "$f" ]; then files+=("$f"); fi
  done
  [ "${#files[@]}" -gt 0 ] || { echo "설치 파일 없음" >&2; exit 1; }
  sum "${files[@]}" >SHA256SUMS
)
release_tool "$DIR" manifest "$DIR" "$VERSION" beta "$BASE"
# 비밀 키 폴더도 컨테이너에 같은 경로로 보이게 한다(release_tool은 한 폴더만 마운트하므로 키를 옮겨 둔다).
cp "$KEYDIR/test.key" "$DIR/.test.key"
chmod 600 "$DIR/.test.key"
release_tool "$DIR" sign "$DIR/.test.key" "$DIR/manifest.json"
rm -f "$DIR/.test.key"
python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); print("manifest", m["version"], m["channel"], [(i["os"], i["arch"], i["kind"], i.get("package", "")) for i in m["installers"]])' "$DIR/manifest.json"
