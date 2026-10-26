package corecli

import (
	"path/filepath"
	"strings"
)

// 설치 배치(#159). Core·GUI·트레이 도우미는 Linux(/usr/bin)·Windows(설치 폴더)에서 같은 폴더에 있다.
// macOS는 한 앱 번들 안에 두되, 트레이는 Dock에 보이지 않는 별도 번들(LSUIElement)이라 경로가 다르다.
//
//	Agent Office Collector.app/Contents/MacOS/agent-office-collector-gui
//	Agent Office Collector.app/Contents/MacOS/agent-office-collector
//	Agent Office Collector.app/Contents/Library/LoginItems/Agent Office Collector Tray.app/Contents/MacOS/agent-office-collector-tray
const (
	// GUIName·TrayName은 확장자를 뺀 실행 파일 이름이다.
	GUIName  = "agent-office-collector-gui"
	TrayName = "agent-office-collector-tray"
	// TrayBundle은 macOS 트레이 도우미 번들 이름이다(GUI 번들의 Contents/Library/LoginItems 아래).
	TrayBundle = "Agent Office Collector Tray.app"
)

// ExeName은 OS별 실행 파일 이름이다.
func ExeName(goos, base string) string {
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

// SiblingCandidates는 실행 파일 self 옆에서 name(확장자 제외)을 찾을 후보 경로다(순서대로).
// 같은 폴더가 먼저이고, macOS 번들 안이면 GUI 번들 ↔ 트레이 번들 위치를 더한다.
func SiblingCandidates(self, goos, name string) []string {
	if self == "" {
		return nil
	}
	dir := filepath.Dir(self)
	out := []string{filepath.Join(dir, ExeName(goos, name))}
	if goos != "darwin" || filepath.Base(dir) != "MacOS" || filepath.Base(filepath.Dir(dir)) != "Contents" {
		return out
	}
	contents := filepath.Dir(dir)
	bundle := filepath.Dir(contents)
	// 트레이 번들 안이면 바깥 GUI 번들의 Contents/MacOS.
	if filepath.Base(bundle) == TrayBundle && filepath.Base(filepath.Dir(bundle)) == "LoginItems" {
		outer := filepath.Dir(filepath.Dir(filepath.Dir(bundle))) // .../Contents
		if filepath.Base(outer) == "Contents" {
			out = append(out, filepath.Join(outer, "MacOS", name))
		}
	}
	// GUI 번들 안이면 안쪽 트레이 번들.
	if strings.HasSuffix(bundle, ".app") && name == TrayName {
		out = append(out, filepath.Join(contents, "Library", "LoginItems", TrayBundle, "Contents", "MacOS", name))
	}
	return out
}

// FindSibling은 SiblingCandidates 중 실제 파일인 첫 경로다.
func FindSibling(self, goos, name string) (string, bool) {
	for _, p := range SiblingCandidates(self, goos, name) {
		if isFile(p) {
			return p, true
		}
	}
	return "", false
}
