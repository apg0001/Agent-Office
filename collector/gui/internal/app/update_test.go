package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

func TestUpdatePrefsDefaultsAndSave(t *testing.T) {
	h := newHarness(t, "")
	if v := h.app.UpdatePrefs(); v.Mode != prefs.UpdateNotify || v.Channel != "" || v.Effective != "beta" && v.Effective != "stable" {
		t.Fatalf("default %+v", v)
	}
	for _, bad := range [][2]string{{"always", ""}, {"notify", "nightly"}} {
		if r := h.app.SaveUpdatePrefs(bad[0], bad[1]); r.OK {
			t.Fatalf("accepted %v", bad)
		}
	}
	if r := h.app.SaveUpdatePrefs(prefs.UpdateDownload, "stable"); !r.OK {
		t.Fatal(r)
	}
	if v := h.app.UpdatePrefs(); v.Mode != prefs.UpdateDownload || v.Effective != "stable" {
		t.Fatalf("saved %+v", v)
	}
	// 다른 설정(서버 주소)은 그대로 둔다.
	h.app.SaveServer("https://office.example")
	h.app.SaveUpdatePrefs(prefs.UpdateManual, "")
	if s := h.app.Settings(); s.ServerURL != "https://office.example" {
		t.Fatal("server lost")
	}
	if r := h.app.InstallerCheck(); !r.OK {
		t.Fatal(r)
	}
	// 채널을 고르지 않으면 Core 버전(0.0.0-dev = 사전 배포)으로 beta를 쓴다.
	calls := h.calls()
	if last := calls[len(calls)-1]; !slices.Contains(last, "beta") {
		t.Fatalf("channel %q", last)
	}
}

func TestInstallerViews(t *testing.T) {
	h := newHarness(t, "")
	if v := h.app.InstallerCheck(); v.Status != "available" || v.Version != "0.2.0" || !v.OK || v.Message == "" {
		t.Fatalf("check %+v", v)
	}
	if v := h.app.InstallerDownload(); v.Status != "downloaded" {
		t.Fatalf("download %+v", v)
	}
	if v := h.app.InstallerInstall(false); v.OK || v.Code != "confirmation_required" {
		t.Fatalf("unconfirmed %+v", v)
	}
	// 트레이 도우미가 옆에 없으면(개발 빌드) 설치를 시작하지 않는다.
	if v := h.app.InstallerInstall(true); v.OK || v.Code != "tray_missing" {
		t.Fatalf("no tray %+v", v)
	}
	dir := t.TempDir()
	gui := filepath.Join(dir, "agent-office-collector-gui")
	tray := filepath.Join(dir, "agent-office-collector-tray")
	if goos == "windows" {
		gui, tray = gui+".exe", tray+".exe"
	}
	os.WriteFile(gui, nil, 0o755)
	os.WriteFile(tray, nil, 0o755)
	h.app.opts.Executable = func() (string, error) { return gui, nil }
	if v := h.app.InstallerInstall(true); v.Status != "installer_started" {
		t.Fatalf("install %+v", v)
	}
	last := h.calls()[len(h.calls())-1]
	res, _ := h.app.UpdateResultPath()
	if !slices.Contains(last, "--yes") || !slices.Contains(last, "--relaunch") || !slices.Contains(last, res) {
		t.Fatalf("install args %q", last)
	}

	cli := newHarness(t, "update_cli")
	if v := cli.app.InstallerCheck(); v.OK || v.Code != "not_installer_install" || !strings.Contains(v.Message, "update apply") {
		t.Fatalf("cli %+v", v)
	}
	// Core가 이상한 값을 내도 화면에는 정해진 형식만 간다.
	leak := newHarness(t, "update_leak")
	v := leak.app.InstallerCheck()
	assertNoToken(t, v)
	if v.Version != "" || v.Code != "unexpected" {
		t.Fatalf("leak %+v", v)
	}
}

func TestPostInstallLegacyAndOutcome(t *testing.T) {
	h := newHarness(t, "")
	v := h.app.PostInstall()
	if !v.OK || v.Status != "attention" || !v.CodexRetrust || v.Service != "reinstalled" || len(v.Hooks) != 2 || v.LegacyFiles != 2 {
		t.Fatalf("post-install %+v", v)
	}
	l := h.app.LegacyStatus()
	if !l.OK || !l.Eligible || len(l.Files) != 1 || l.Files[0].Name != "agent-office-collector" {
		t.Fatalf("legacy %+v", l)
	}
	if r := h.app.LegacyRemove(false); r.OK {
		t.Fatal("removed without confirmation")
	}
	if r := h.app.LegacyRemove(true); !r.OK || !slices.Contains(h.calls()[len(h.calls())-1], "--yes") {
		t.Fatalf("remove %+v", r)
	}

	if h.app.TakeUpdateOutcome() != nil {
		t.Fatal("outcome without file")
	}
	path, _ := h.app.UpdateResultPath()
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{"schema":1,"from":"0.1.0","to":"0.2.0","code":"installed"}`), 0o600)
	if o := h.app.TakeUpdateOutcome(); o == nil || o.To != "0.2.0" || o.Code != "installed" {
		t.Fatalf("outcome %+v", o)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("outcome not consumed")
	}
	h.app.SetLastVersion("0.2.0")
	h.app.SetLastVersion("../bad")
	if h.app.LastVersion() != "0.2.0" {
		t.Fatal("last version")
	}
}

func TestSetupFlow(t *testing.T) {
	h := newHarness(t, "")
	if s := h.app.SetupStatus(); !s.Show || s.ServerSet || !s.Registered {
		t.Fatalf("initial %+v", s)
	}
	// 동의 화면: 미리보기를 본 도구만 설치한다.
	if v := h.app.SetupApply(SetupChoices{Tools: []string{"claude"}}, true); v.OK || v.Hooks[0].Result.Code != "preview_required" {
		t.Fatalf("no preview %+v", v)
	}
	prev := h.app.SetupPreview([]string{"claude", "codex"})
	if len(prev) != 2 || !prev[0].Result.OK || !prev[1].Result.OK {
		t.Fatalf("preview %+v", prev)
	}
	if v := h.app.SetupApply(SetupChoices{Tools: []string{"claude", "codex"}, Service: true}, false); v.OK {
		t.Fatal("applied without confirmation")
	}
	v := h.app.SetupApply(SetupChoices{Tools: []string{"claude", "codex"}, Service: true}, true)
	if !v.OK || !v.CodexTrust || v.Service == nil || !v.Service.OK || v.Tray != nil {
		t.Fatalf("apply %+v", v)
	}
	calls := h.calls()
	for _, want := range [][]string{
		{"hooks", "install", "--tool", "claude", "--yes"}, {"hooks", "install", "--tool", "codex", "--yes"},
		{"service", "install", "--activate"},
	} {
		if !slices.ContainsFunc(calls, func(c []string) bool { return slices.Equal(c, want) }) {
			t.Fatalf("missing %q in %q", want, calls)
		}
	}
	if r := h.app.SetupFinish(true); !r.OK {
		t.Fatal(r)
	}
	if s := h.app.SetupStatus(); s.Show || s.State != "dismissed" {
		t.Fatalf("after %+v", s)
	}
}
