#!/usr/bin/env bash
# macOS 설치 패키지(.pkg) 빌드(#159). macOS(Xcode 명령행 도구, Go 1.24)에서 실행한다.
#
#   collector/packaging/macos/build-pkg.sh <버전(SemVer)> [출력 디렉터리]
#
# arm64·x86_64를 각각 빌드해 lipo로 합친 유니버설 바이너리로 앱 번들을 만든다.
#
#   /Applications/Agent Office Collector.app
#     Contents/MacOS/agent-office-collector-gui        Wails 창(CFBundleExecutable)
#     Contents/MacOS/agent-office-collector            Core(헤드리스 수집, launchd 사용자 에이전트로 실행)
#     Contents/Resources/agent-office-collector-uninstall  사용자별 정리(--remove-app으로 앱 삭제)
#     Contents/Library/LoginItems/Agent Office Collector Tray.app   메뉴 막대 트레이(LSUIElement, Dock 없음)
#
# 서명은 ad-hoc(`codesign -s -`)뿐이고 공증하지 않는다. 내려받은 .pkg는 Gatekeeper가 막으므로
# 우클릭 → 열기 또는 시스템 설정 → 개인정보 보호 및 보안 → "그래도 열기"가 필요하다(collector-gui.md).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
VERSION=${1:?버전}
OUT=${2:-$ROOT/dist/collector-$VERSION}
[[ "$VERSION" =~ ^([0-9]+\.[0-9]+\.[0-9]+)(-[0-9A-Za-z.]+)?$ ]] || { echo "SemVer가 아님: $VERSION" >&2; exit 2; }
SHORT=${BASH_REMATCH[1]}
[ "$(uname -s)" = Darwin ] || { echo "macOS에서 실행하세요" >&2; exit 2; }
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
KEYS=${AGENT_OFFICE_UPDATE_KEYS:-}
APP_ID=io.github.apg0001.agent-office.collector-gui
TRAY_ID=io.github.apg0001.agent-office.collector-tray
PKG_ID=io.github.apg0001.agent-office.collector
NAME="agent-office-collector-$VERSION-macos-universal.pkg"
export MACOSX_DEPLOYMENT_TARGET=11.0

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/bin"
mkdir -p "$BIN"

build() { # build <goarch> <cgo> <dir> <out> [go build args...]
  local arch=$1 cgo=$2 dir=$3 out=$4
  shift 4
  (cd "$ROOT/collector/$dir" && GOOS=darwin GOARCH=$arch CGO_ENABLED=$cgo go build -trimpath -o "$out" "$@")
}
for arch in arm64 amd64; do
  build "$arch" 0 core "$BIN/core-$arch" \
    -ldflags "-s -w -X main.version=$VERSION -X github.com/apg0001/Agent-Office/collector/core/internal/update.TrustedKeys=$KEYS" \
    ./cmd/agent-office-collector
  build "$arch" 1 gui "$BIN/gui-$arch" -tags desktop,production -ldflags "-s -w" .
  build "$arch" 1 gui "$BIN/tray-$arch" -ldflags "-s -w -X main.version=$VERSION" ./cmd/agent-office-collector-tray
done
for b in core gui tray; do
  lipo -create -output "$BIN/$b" "$BIN/$b-arm64" "$BIN/$b-amd64"
  lipo -info "$BIN/$b"
done

ROOTDIR="$WORK/root"
APP="$ROOTDIR/Agent Office Collector.app"
TRAYAPP="$APP/Contents/Library/LoginItems/Agent Office Collector Tray.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$TRAYAPP/Contents/MacOS"
install -m 0755 "$BIN/gui" "$APP/Contents/MacOS/agent-office-collector-gui"
install -m 0755 "$BIN/core" "$APP/Contents/MacOS/agent-office-collector"
# 스크립트는 Contents/MacOS가 아니라 Resources에 둔다(MacOS 폴더의 서명 없는 비Mach-O는 번들 서명을 깨뜨림).
install -m 0755 "$ROOT/collector/packaging/uninstall.sh" "$APP/Contents/Resources/agent-office-collector-uninstall"
install -m 0644 "$ROOT/collector/packaging/README.txt" "$APP/Contents/Resources/README.txt"
install -m 0755 "$BIN/tray" "$TRAYAPP/Contents/MacOS/agent-office-collector-tray"

# 앱 아이콘(.icns): 256px PNG에서 만든다. 실패해도 기본 아이콘으로 동작한다.
ICONSET="$WORK/AppIcon.iconset"
mkdir -p "$ICONSET"
for s in 16 32 128 256; do
  sips -z $s $s "$ROOT/collector/packaging/assets/agent-office-collector.png" --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/AppIcon.icns" || true

plist() { # plist <파일> <실행 파일> <번들 ID> <이름> <추가 키...>
  local file=$1 exe=$2 id=$3 name=$4
  shift 4
  cat >"$file" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key><string>ko</string>
	<key>CFBundleExecutable</key><string>$exe</string>
	<key>CFBundleIdentifier</key><string>$id</string>
	<key>CFBundleName</key><string>$name</string>
	<key>CFBundleDisplayName</key><string>$name</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$SHORT</string>
	<key>CFBundleVersion</key><string>$SHORT</string>
	<key>LSMinimumSystemVersion</key><string>$MACOSX_DEPLOYMENT_TARGET</string>
	<key>NSHighResolutionCapable</key><true/>
$*
</dict>
</plist>
EOF
  plutil -lint "$file" >/dev/null
}
plist "$APP/Contents/Info.plist" agent-office-collector-gui "$APP_ID" "Agent Office Collector" \
  "	<key>CFBundleIconFile</key><string>AppIcon</string>"
# 트레이는 메뉴 막대 전용 앱이다(Dock·앱 전환기에 보이지 않음).
plist "$TRAYAPP/Contents/Info.plist" agent-office-collector-tray "$TRAY_ID" "Agent Office Collector Tray" \
  "	<key>LSUIElement</key><true/>"

# Apple Silicon은 서명 없는 코드를 실행하지 않는다. ad-hoc으로 안쪽부터 서명한다(공증 아님).
codesign --force --sign - --timestamp=none "$APP/Contents/MacOS/agent-office-collector"
codesign --force --sign - --timestamp=none "$TRAYAPP"
codesign --force --sign - --timestamp=none "$APP"
codesign --verify --deep --strict "$APP"

# 번들 이동(relocation)을 끈다. 켜 두면 같은 번들 ID가 다른 곳(빌드 폴더 등)에 있을 때 그쪽에 설치된다.
COMP="$WORK/component.plist"
pkgbuild --analyze --root "$ROOTDIR" "$COMP" >/dev/null
plutil -replace 0.BundleIsRelocatable -bool NO "$COMP"
pkgbuild --root "$ROOTDIR" --component-plist "$COMP" --identifier "$PKG_ID" --version "$VERSION" \
  --install-location /Applications "$WORK/component.pkg" >/dev/null

cat >"$WORK/distribution.xml" <<EOF
<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
    <title>Agent Office Collector $VERSION</title>
    <options customize="never" require-scripts="false" hostArchitectures="arm64,x86_64"/>
    <domains enable_anywhere="false" enable_currentUserHome="false" enable_localSystem="true"/>
    <volume-check><allowed-os-versions><os-version min="$MACOSX_DEPLOYMENT_TARGET"/></allowed-os-versions></volume-check>
    <choices-outline><line choice="default"><line choice="$PKG_ID"/></line></choices-outline>
    <choice id="default"/>
    <choice id="$PKG_ID" visible="false"><pkg-ref id="$PKG_ID"/></choice>
    <pkg-ref id="$PKG_ID" version="$VERSION" onConclusion="none">component.pkg</pkg-ref>
</installer-gui-script>
EOF
productbuild --distribution "$WORK/distribution.xml" --package-path "$WORK" "$OUT/$NAME" >/dev/null
pkgutil --payload-files "$OUT/$NAME" | head -20 || true
ls -l "$OUT/$NAME"
