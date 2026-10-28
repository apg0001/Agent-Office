package main

import (
	"io/fs"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/guictl"
)

// 창이 늦게 떠서 트레이 "종료"가 먼저 도착했으면 트레이를 다시 띄우지 않는다(#177).
func TestCompanionTraySkippedWhenQuitPending(t *testing.T) {
	dir := t.TempDir()
	p, err := guictl.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	calls := 0
	launchTray := func() error { calls++; return nil }

	if !launchCompanionTray(p, launchTray) || calls != 1 {
		t.Fatalf("no request: launched=%d, want 1", calls)
	}
	if err := guictl.Send(dir, guictl.Show); err != nil {
		t.Fatal(err)
	}
	if !launchCompanionTray(p, launchTray) || calls != 2 {
		t.Fatalf("show pending: launched=%d, want 2", calls)
	}
	if err := guictl.Send(dir, guictl.Quit); err != nil {
		t.Fatal(err)
	}
	if launchCompanionTray(p, launchTray) || calls != 2 {
		t.Fatalf("quit pending: launched=%d, want 2", calls)
	}
	if p.Pending() != guictl.Quit {
		t.Fatal("quit request must stay for Serve")
	}
	if !launchCompanionTray(nil, launchTray) || calls != 3 {
		t.Fatalf("no lock: launched=%d, want 3", calls)
	}
}

func readAsset(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(assets, "frontend/assets/"+name)
	if err != nil {
		t.Fatalf("embedded %s: %v", name, err)
	}
	return string(b)
}

// 프런트엔드가 부르는 메서드가 모두 바인딩된 App에 있는지 화면 없이 확인한다.
func TestFrontendCallsExistOnApp(t *testing.T) {
	js := readAsset(t, "app.js")
	typ := reflect.TypeOf(&app.App{})
	calls := regexp.MustCompile(`call\("([A-Za-z]+)"`).FindAllStringSubmatch(js, -1)
	if len(calls) < 10 {
		t.Fatalf("too few calls found: %d", len(calls))
	}
	for _, m := range calls {
		if _, ok := typ.MethodByName(m[1]); !ok {
			t.Errorf("app.js calls %s, which App does not export", m[1])
		}
	}
	if !strings.Contains(js, "window.go.app.App") {
		t.Error("binding namespace changed: Wails exposes the bound struct as window.go.<package>.<type>")
	}
}

// 외부 스크립트·스타일·인라인 스크립트를 쓰지 않는다(오프라인 동작, CSP 'self').
func TestFrontendIsSelfContained(t *testing.T) {
	html := readAsset(t, "index.html")
	if regexp.MustCompile(`(?i)(src|href)\s*=\s*"(https?:)?//`).MatchString(html) {
		t.Error("index.html loads a remote resource")
	}
	for _, m := range regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`).FindAllStringSubmatch(html, -1) {
		if !strings.Contains(m[1], "src=") || strings.TrimSpace(m[2]) != "" {
			t.Errorf("inline script: %q", m[0])
		}
	}
	if !strings.Contains(html, "Content-Security-Policy") {
		t.Error("CSP meta missing")
	}
	for _, name := range []string{"app.js", "style.css"} {
		if body := readAsset(t, name); regexp.MustCompile(`(?i)(fetch\(|XMLHttpRequest|@import|url\(\s*["']?https?:)`).MatchString(body) {
			t.Errorf("%s reaches the network directly", name)
		}
	}
	// 토큰을 다루는 화면 요소가 없다.
	if strings.Contains(strings.ToLower(html+readAsset(t, "app.js")), "device_token") {
		t.Error("frontend references device_token")
	}
}
