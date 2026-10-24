package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/fakecore"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

func TestMain(m *testing.M) {
	fakecore.Main()
	os.Exit(m.Run())
}

type harness struct {
	app    *App
	log    string
	home   string
	opened []string
}

func newHarness(t *testing.T, scenario string) *harness {
	t.Helper()
	h := &harness{home: t.TempDir(), log: filepath.Join(t.TempDir(), "calls.jsonl")}
	r := &corecli.Runner{Bin: os.Args[0], Env: fakecore.Env(h.log, scenario)}
	h.app = New(Options{
		Getenv: func(k string) string {
			if k == prefs.EnvHome {
				return h.home
			}
			return ""
		},
		Hostname: func() (string, error) { return "dev-box", nil },
		OpenURL:  func(_ context.Context, u string) { h.opened = append(h.opened, u) },
		Runner:   r,
	})
	OnStartup(h.app)(context.Background())
	return h
}

func (h *harness) calls() [][]string {
	var out [][]string
	for _, c := range fakecore.ReadCalls(h.log) {
		out = append(out, c.Args)
	}
	return out
}

// 화면으로 가는 값 전체에 토큰 형식 문자열이 없어야 한다.
func assertNoToken(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), fakecore.SentinelToken) {
		t.Fatalf("token reached the UI: %s", b)
	}
}

func TestSettingsAndServer(t *testing.T) {
	h := newHarness(t, "")
	s := h.app.Settings()
	if s.ServerURL != "" || s.DeviceName != "dev-box" || !s.CoreFound || s.PrefsState != "ok" {
		t.Fatalf("initial settings: %+v", s)
	}
	if r := h.app.OpenRegistrationPage(); r.OK || r.Code != "server_not_set" {
		t.Errorf("open before server: %+v", r)
	}
	if r := h.app.SaveServer("http://office.example"); r.OK || r.Code != "insecure_server_url" {
		t.Errorf("insecure: %+v", r)
	}
	if r := h.app.SaveServer("https://office.example/"); !r.OK {
		t.Fatalf("save: %+v", r)
	}
	if s := h.app.Settings(); s.ServerURL != "https://office.example" {
		t.Errorf("saved: %+v", s)
	}
	if r := h.app.OpenRegistrationPage(); !r.OK || !slices.Equal(h.opened, []string{"https://office.example/devices"}) {
		t.Errorf("open: %+v %v", r, h.opened)
	}
	// 손상된 설정 파일은 무시하고 빈 값으로 시작한다.
	path, _ := h.app.prefsPath()
	_ = os.WriteFile(path, []byte("{"), 0o600)
	if s := h.app.Settings(); s.PrefsState != "invalid" || s.ServerURL != "" {
		t.Errorf("invalid prefs: %+v", s)
	}
}

func TestRegisterFlow(t *testing.T) {
	h := newHarness(t, "")
	if r := h.app.Register(fakecore.GoodCode, "PC", false); r.Code != "server_not_set" {
		t.Errorf("no server: %+v", r)
	}
	h.app.SaveServer("https://office.example")
	if r := h.app.Register("bad", "PC", false); r.Code != "invalid_registration_code" {
		t.Errorf("bad code: %+v", r)
	}
	if r := h.app.Register("WXYZ-2345", "PC", false); r.OK || r.Code != "register_failed" || !strings.Contains(r.Output, "등록 코드") {
		t.Errorf("rejected code: %+v", r)
	}
	r := h.app.Register("abcd-efgh", " 새 PC ", false)
	if !r.OK || r.Output != "" {
		t.Fatalf("register: %+v", r)
	}
	assertNoToken(t, r)
	if s := h.app.Settings(); s.DeviceName != "새 PC" {
		t.Errorf("name not remembered: %+v", s)
	}
	// GUI 설정 파일에는 코드·토큰이 남지 않는다.
	path, _ := h.app.prefsPath()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "ABCD") || strings.Contains(string(b), "aod_") {
		t.Errorf("prefs contain secrets: %s", b)
	}
}

func TestRegisterRedactsLeakedToken(t *testing.T) {
	h := newHarness(t, "leak")
	h.app.SaveServer("https://office.example")
	r := h.app.Register(fakecore.GoodCode, "PC", false)
	if r.OK {
		t.Fatal("expected failure")
	}
	assertNoToken(t, r)
	if !strings.Contains(r.Output, "aod_[가림]") {
		t.Errorf("redaction marker missing: %q", r.Output)
	}
}

func TestStatusPauseResumeDiagnose(t *testing.T) {
	h := newHarness(t, "")
	v := h.app.Status()
	if !v.OK || v.Report.Run.State != "running" || v.Report.Queue.Depth != 2 {
		t.Fatalf("status: %+v", v)
	}
	if r := h.app.Pause(); !r.OK || r.Code != "paused" {
		t.Errorf("pause: %+v", r)
	}
	if r := h.app.Resume(); !r.OK || r.Code != "resumed" {
		t.Errorf("resume: %+v", r)
	}
	if d := h.app.Diagnose(true); !d.OK || d.Report.Server != "skipped" {
		t.Errorf("diagnose: %+v", d)
	}
	want := [][]string{{"status", "--json"}, {"pause"}, {"resume"}, {"diagnose", "--json", "--offline"}}
	if got := h.calls(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("calls %q", got)
	}

	g := newHarness(t, "garbage")
	if v := g.app.Status(); v.OK || v.Code != "core_output_invalid" {
		t.Errorf("garbage: %+v", v)
	}
}

func TestCoreMissing(t *testing.T) {
	a := New(Options{
		Getenv: func(k string) string {
			return map[string]string{prefs.EnvHome: t.TempDir(), corecli.EnvBin: "/nonexistent/agent-office-collector"}[k]
		},
		Executable: func() (string, error) { return "", errors.New("x") },
	})
	if s := a.Settings(); s.CoreFound {
		t.Error("core found")
	}
	if v := a.Status(); v.OK || v.Code != "core_missing" {
		t.Errorf("status: %+v", v)
	}
	if r := a.Pause(); r.Code != "core_missing" || !strings.Contains(r.Message, "AGENT_OFFICE_COLLECTOR_BIN") {
		t.Errorf("pause: %+v", r)
	}
}

// Hook 설치는 미리보기 → 명시적 승인 순서를 지켜야 Core에 --yes가 간다.
func TestHookConsentFlow(t *testing.T) {
	h := newHarness(t, "")
	if r := h.app.HookInstall(true); r.OK || r.Code != "preview_required" {
		t.Errorf("install without preview: %+v", r)
	}
	if r := h.app.HookInstall(false); r.Code != "confirmation_required" {
		t.Errorf("install without confirm: %+v", r)
	}
	p := h.app.HookPreview()
	if !p.OK || !strings.Contains(p.Output, "+ SessionStart") {
		t.Fatalf("preview: %+v", p)
	}
	if r := h.app.HookInstall(false); r.OK {
		t.Error("installed without confirmation")
	}
	if r := h.app.HookInstall(true); !r.OK || r.Code != "hook_installed" {
		t.Errorf("install: %+v", r)
	}
	// 한 번 승인은 한 번만 쓴다.
	if r := h.app.HookInstall(true); r.Code != "preview_required" {
		t.Errorf("second install reused consent: %+v", r)
	}
	if r := h.app.HookUninstall(false); r.OK {
		t.Error("uninstall without confirmation")
	}
	if r := h.app.HookUninstall(true); !r.OK {
		t.Errorf("uninstall: %+v", r)
	}
	if r := h.app.CodexHookPrint(); !r.OK || !json.Valid([]byte(r.Output)) {
		t.Errorf("codex print: %+v", r)
	}
	yes := 0
	for _, c := range h.calls() {
		if slices.Contains(c, "--yes") {
			yes++
		}
	}
	if yes != 2 {
		t.Errorf("--yes calls = %d, want 2 (install, uninstall): %q", yes, h.calls())
	}

	tools := h.app.Tools()
	if !tools.OK || len(tools.Tools) != 2 || tools.Tools[0].Version != "2.1.308" {
		t.Errorf("tools: %+v", tools)
	}
}

func TestServiceFlow(t *testing.T) {
	h := newHarness(t, "")
	if r := h.app.ServicePreview(); !r.OK || !strings.Contains(r.Output, "[Unit]") {
		t.Errorf("preview: %+v", r)
	}
	if r := h.app.ServiceInstall(true, false); r.OK {
		t.Error("installed without confirmation")
	}
	if r := h.app.ServiceInstall(true, true); !r.OK || !strings.Contains(r.Output, "--activate") {
		t.Errorf("install: %+v", r)
	}
	if r := h.app.ServiceInstall(false, true); !r.OK || strings.Contains(r.Output, "--activate") {
		t.Errorf("install without activate: %+v", r)
	}
}

func TestRedactAndMessages(t *testing.T) {
	if got := redact(" x aod_abcdefgh123_- y "); got != "x aod_[가림] y" {
		t.Errorf("redact: %q", got)
	}
	if got := redact("aod_short"); got != "aod_short" {
		t.Errorf("short kept: %q", got)
	}
	if messageFor("nope") != messages["unexpected"] {
		t.Error("unknown code message")
	}
	for _, code := range []string{"core_missing", "core_timeout", "core_start_failed", "core_output_invalid", "invalid_server_url",
		"insecure_server_url", "invalid_registration_code", "invalid_device_name"} {
		if _, ok := messages[code]; !ok {
			t.Errorf("message missing for %s", code)
		}
	}
}
