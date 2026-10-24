package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

// EnvRealCore는 실제 Core 실행 파일 경로다. CI와 문서의 검증 절차는 Core를 빌드해 이 값을 넣는다.
const EnvRealCore = "AGENT_OFFICE_TEST_CORE_BIN"

const realSentinelToken = "aod_SENTINEL_TOKEN_real_core_DO_NOT_SHOW"

// 실제 Core와의 계약 테스트: GUI가 기대하는 JSON 필드·명령 인자·표준 입력 코드 전달이 Core와 맞는지
// 임시 디렉터리(HOME·Claude 설정·Collector 설정 모두 격리)에서 확인한다.
func TestRealCoreContract(t *testing.T) {
	bin := os.Getenv(EnvRealCore)
	if bin == "" {
		t.Skip(EnvRealCore + " 미설정: 실제 Core 계약 테스트는 Core를 빌드한 CI·수동 검증에서 실행한다")
	}
	if runtime.GOOS == "windows" {
		t.Skip("가짜 claude 스크립트가 POSIX 셸 전용")
	}
	var registered struct{ code, name string }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/devices/register":
			var req map[string]string
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &req)
			registered.code, registered.name = req["registration_code"], req["name"]
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"device_id":"0d6b2a10-7a4c-4b8e-9f00-1234567890ab","device_token":"` + realSentinelToken + `"}`))
		case "/api/v1/system/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	claudeDir := filepath.Join(root, "claude")
	toolBin := filepath.Join(root, "bin")
	for _, d := range []string{home, claudeDir, toolBin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// PATH의 가짜 claude: 버전 한 줄만 출력한다.
	if err := os.WriteFile(filepath.Join(toolBin, "claude"), []byte("#!/bin/sh\necho '2.1.308 (Claude Code)'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(claudeDir, "settings.json")
	const userSettings = `{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"command","command":"my-notifier --done"}]}]}}`
	if err := os.WriteFile(settings, []byte(userSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	aoHome := filepath.Join(root, "ao")
	env := []string{
		"HOME=" + home,
		"PATH=" + toolBin + ":/usr/bin:/bin",
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"CLAUDE_CONFIG_DIR=" + claudeDir,
		prefs.EnvHome + "=" + aoHome,
	}
	a := New(Options{
		Getenv: func(k string) string {
			if k == prefs.EnvHome {
				return aoHome
			}
			return ""
		},
		Runner: &corecli.Runner{Bin: bin, Env: env},
	})

	v := a.Status()
	if !v.OK || v.Report.Registration != "not_registered" || v.Report.ProcessRunning {
		t.Fatalf("initial status: %+v", v)
	}

	if r := a.SaveServer(srv.URL); !r.OK {
		t.Fatalf("save server: %+v", r)
	}
	r := a.Register("abcd efgh", "GUI 계약 PC", false)
	if !r.OK {
		t.Fatalf("register: %+v", r)
	}
	if registered.code != "ABCD-EFGH" || registered.name != "GUI 계약 PC" {
		t.Errorf("server saw %+v", registered)
	}
	if r := a.Register("ABCD-EFGH", "GUI 계약 PC", false); r.OK || r.Code != "already_registered" {
		t.Errorf("second register without force: %+v", r)
	}

	v = a.Status()
	if !v.OK || v.Report.Registration != "registered" || v.Report.ServerScheme != "http" || !v.Report.ServerLoopback {
		t.Errorf("registered status: %+v", v.Report)
	}
	if r := a.Pause(); !r.OK {
		t.Errorf("pause: %+v", r)
	}
	if v := a.Status(); !v.Report.Paused {
		t.Error("not paused")
	}
	if r := a.Resume(); !r.OK {
		t.Errorf("resume: %+v", r)
	}
	if d := a.Diagnose(false); !d.OK || d.Report.Server != "reachable" || d.Report.GoVersion == "" {
		t.Errorf("diagnose: %+v", d.Report)
	}

	tools := a.Tools()
	if !tools.OK || len(tools.Tools) != 2 {
		t.Fatalf("tools: %+v", tools)
	}
	c := tools.Tools[0]
	if c.Tool != "claude" || !c.Detected || c.Version != "2.1.308" || c.HookState != "not_installed" || c.Foreign != 1 {
		t.Errorf("claude tool: %+v", c)
	}
	if tools.Tools[1].Detected || tools.Tools[1].HookInstall != "print_only" {
		t.Errorf("codex tool: %+v", tools.Tools[1])
	}

	if p := a.HookPreview(); !p.OK || !strings.Contains(p.Output, "+ SessionStart") {
		t.Fatalf("hook preview: %+v", p)
	}
	if b, _ := os.ReadFile(settings); string(b) != userSettings {
		t.Fatal("preview modified settings.json")
	}
	if r := a.HookInstall(true); !r.OK {
		t.Fatalf("hook install: %+v", r)
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), "hook claude") || !strings.Contains(string(b), "my-notifier --done") {
		t.Errorf("settings after install:\n%s", b)
	}
	if tools := a.Tools(); tools.Tools[0].HookState != "installed" {
		t.Errorf("hook state after install: %+v", tools.Tools[0])
	}
	if r := a.HookUninstall(true); !r.OK {
		t.Errorf("hook uninstall: %+v", r)
	}
	if r := a.CodexHookPrint(); !r.OK || !json.Valid([]byte(r.Output)) {
		t.Errorf("codex print: %+v", r)
	}

	if runtime.GOOS == "linux" {
		if r := a.ServicePreview(); !r.OK || !strings.Contains(r.Output, "[Unit]") {
			t.Errorf("service preview: %+v", r)
		}
		// --activate 없이 파일만 쓴다(컨테이너·CI에는 systemd 사용자 세션이 없음).
		if r := a.ServiceInstall(false, true); !r.OK {
			t.Errorf("service install: %+v", r)
		}
		if v := a.Status(); v.Report.Service != "installed" {
			t.Errorf("service state: %q", v.Report.Service)
		}
	}

	// 어떤 화면 값에도 토큰이 없고, GUI 설정 파일에도 없다.
	for _, x := range []any{a.Status(), a.Diagnose(true), a.Tools(), a.Settings()} {
		if b, _ := json.Marshal(x); strings.Contains(string(b), realSentinelToken) || strings.Contains(string(b), home) {
			t.Errorf("UI value leaks token or home path: %s", b)
		}
	}
	pp, _ := a.prefsPath()
	if b, _ := os.ReadFile(pp); strings.Contains(string(b), "aod_") || strings.Contains(string(b), "ABCD") {
		t.Errorf("gui.json contains secrets: %s", b)
	}
}
