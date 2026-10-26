#!/usr/bin/env bash
# 채널 Release 갱신(#180). 관리자 승인을 받은 게시 워크플로(publish-collector.yml, publish_channel=true) 또는
# 관리자가 직접 실행한다. 외부 공개 단계이므로 승인 없이 실행하지 않는다.
#
#   collector/packaging/update-channel.sh <버전> <manifest.json 폴더>
#
# 1) 버전 Release(collector-v<버전>) 초안을 공개한다(manifest의 설치 파일 주소가 이 Release 자산이라 초안이면
#    사용자가 받을 수 없다). 사전 배포 버전은 prerelease로 둔다.
# 2) 채널 Release(사전 배포 → collector-beta, 아니면 collector-stable)가 없으면 만들고(latest로 표시하지 않음)
#    서명된 manifest.json·manifest.json.sig를 덮어쓴다. 트레이가 확인하는 기본 주소가 이 채널 Release다.
set -euo pipefail
VERSION=${1:?버전}
DIR=${2:?manifest 폴더}
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "SemVer가 아님: $VERSION" >&2; exit 2; }
for f in manifest.json manifest.json.sig; do
  [ -s "$DIR/$f" ] || { echo "$f 없음" >&2; exit 1; }
done
channel=stable
pre=(--prerelease=false)
if [[ "$VERSION" == *-* ]]; then
  channel=beta
  pre=(--prerelease)
fi
grep -q "\"version\": \"$VERSION\"" "$DIR/manifest.json" || { echo "manifest 버전이 $VERSION이 아님" >&2; exit 1; }
grep -q "\"channel\": \"$channel\"" "$DIR/manifest.json" || { echo "manifest 채널이 $channel이 아님" >&2; exit 1; }

tag="collector-v$VERSION"
gh release edit "$tag" --draft=false "${pre[@]}"

ctag="collector-$channel"
if ! gh release view "$ctag" >/dev/null 2>&1; then
  gh release create "$ctag" --latest=false "${pre[@]}" --target "${GITHUB_SHA:-develop}" \
    --title "Collector $channel channel" \
    --notes "Collector $channel 채널 업데이트 정보(manifest.json, ed25519 서명 .sig)입니다. 트레이가 이 파일로 새 버전을 확인합니다. 설치 파일은 각 버전 Release(collector-v*)에 있습니다."
fi
gh release upload "$ctag" "$DIR/manifest.json" "$DIR/manifest.json.sig" --clobber
echo "$ctag manifest → $VERSION"
