package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
)

func TestLogoutFlow(t *testing.T) {
	h := newHarness(t, "")
	if r := h.app.Logout(false, false); r.OK || r.Code != "confirmation_required" {
		t.Fatalf("without confirmation: %+v", r)
	}
	if len(h.calls()) != 0 {
		t.Fatal("core called without confirmation")
	}
	r := h.app.Logout(false, true)
	if !r.OK || r.Code != "logged_out" {
		t.Fatalf("logout: %+v", r)
	}
	assertNoToken(t, r)
	if r := h.app.Logout(true, true); !r.OK || r.Code != "logged_out_local" {
		t.Fatalf("local only: %+v", r)
	}
	calls := h.calls()
	if len(calls) != 2 || !slices.Equal(calls[0], []string{"logout"}) || !slices.Equal(calls[1], []string{"logout", "--local-only"}) {
		t.Fatalf("calls: %v", calls)
	}

	off := newHarness(t, "logout_offline")
	if r := off.app.Logout(false, true); r.OK || r.Code != "logout_failed" || !strings.Contains(r.Output, "--local-only") {
		t.Fatalf("offline: %+v", r)
	}
	if v := h.app.Status(); v.Report.TokenStorage != "os_keyring" {
		t.Fatalf("token storage: %+v", v.Report)
	}
}

func TestGUIAutostartFlow(t *testing.T) {
	if goos == "windows" {
		t.Skip("POSIX 경로 테스트(Windows 로직은 autostart 패키지에서 메모리 레지스트리로 확인)")
	}
	home := t.TempDir()
	exe := filepath.Join(home, "opt", "agent-office-collector-gui")
	mk := func(exePath string) *App {
		return New(Options{
			GOOS:        "linux",
			Getenv:      func(string) string { return "" },
			Executable:  func() (string, error) { return exePath, nil },
			UserHomeDir: func() (string, error) { return home, nil },
			Runner:      &corecli.Runner{Bin: os.Args[0]},
		})
	}
	a := mk(exe)
	if v := a.GUIAutostartStatus(); !v.OK || v.State != "absent" || v.Location != "~/.config/autostart/agent-office-collector-gui.desktop" {
		t.Fatalf("status: %+v", v)
	}
	if r := a.GUIAutostartInstall(false); r.OK {
		t.Fatal("installed without confirmation")
	}
	if r := a.GUIAutostartInstall(true); !r.OK || strings.Contains(r.Output, home) {
		t.Fatalf("install: %+v", r)
	}
	p := filepath.Join(home, ".config", "autostart", "agent-office-collector-gui.desktop")
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "--autostart") {
		t.Fatalf("desktop file: %v %s", err, b)
	}
	if v := a.GUIAutostartStatus(); v.State != "installed" {
		t.Fatalf("state: %+v", v)
	}
	if v := mk(filepath.Join(home, "moved", "agent-office-collector-gui")).GUIAutostartStatus(); v.State != "outdated" {
		t.Fatalf("moved exe: %+v", v)
	}
	if r := a.GUIAutostartUninstall(true); !r.OK || r.Code != "gui_autostart_removed" {
		t.Fatalf("uninstall: %+v", r)
	}
	if r := a.GUIAutostartUninstall(true); !r.OK || r.Code != "gui_autostart_absent" {
		t.Fatalf("uninstall again: %+v", r)
	}
	if r := mk("/tmp/go-build1/b001/exe/gui").GUIAutostartInstall(true); r.OK || r.Code != "gui_autostart_invalid_exe" {
		t.Fatalf("temp exe: %+v", r)
	}
	// 소유하지 않은 파일은 덮어쓰거나 지우지 않는다.
	if err := os.WriteFile(p, []byte("[Desktop Entry]\nExec=other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := a.GUIAutostartInstall(true); r.OK || r.Code != "gui_autostart_foreign" {
		t.Fatalf("foreign: %+v", r)
	}
	if r := a.GUIAutostartUninstall(true); r.OK || r.Code != "gui_autostart_foreign" {
		t.Fatalf("foreign uninstall: %+v", r)
	}
	for _, code := range []string{"logged_out", "logged_out_local", "logout_failed", "gui_autostart_installed", "gui_autostart_removed",
		"gui_autostart_absent", "gui_autostart_foreign", "gui_autostart_invalid_exe", "gui_autostart_unsupported", "gui_autostart_failed"} {
		if _, ok := messages[code]; !ok {
			t.Errorf("message missing for %s", code)
		}
	}
}
