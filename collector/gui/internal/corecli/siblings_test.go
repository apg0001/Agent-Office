package corecli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSiblingCandidates(t *testing.T) {
	if got := SiblingCandidates("/usr/bin/agent-office-collector-gui", "linux", TrayName); !slices.Equal(got, []string{filepath.Join("/usr/bin", "agent-office-collector-tray")}) {
		t.Errorf("linux: %v", got)
	}
	if got := SiblingCandidates("/opt/ao/agent-office-collector-gui.exe", "windows", TrayName); len(got) != 1 || filepath.Base(got[0]) != "agent-office-collector-tray.exe" {
		t.Errorf("windows: %v", got)
	}
	app := filepath.Join("/Applications", "Agent Office Collector.app", "Contents")
	tray := filepath.Join(app, "Library", "LoginItems", TrayBundle, "Contents", "MacOS", TrayName)
	if got := SiblingCandidates(filepath.Join(app, "MacOS", GUIName), "darwin", TrayName); !slices.Contains(got, tray) {
		t.Errorf("darwin gui→tray: %v", got)
	}
	for _, name := range []string{GUIName, "agent-office-collector"} {
		if got := SiblingCandidates(tray, "darwin", name); !slices.Contains(got, filepath.Join(app, "MacOS", name)) {
			t.Errorf("darwin tray→%s: %v", name, got)
		}
	}
	if got := SiblingCandidates(filepath.Join("/opt", "x", "MacOS", "gui"), "darwin", TrayName); len(got) != 1 {
		t.Errorf("not a bundle: %v", got)
	}
	if SiblingCandidates("", "linux", TrayName) != nil {
		t.Error("empty self")
	}
}

func TestFindSiblingInBundle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Agent Office Collector.app", "Contents")
	trayDir := filepath.Join(root, "Library", "LoginItems", TrayBundle, "Contents", "MacOS")
	for _, d := range []string{filepath.Join(root, "MacOS"), trayDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	core := filepath.Join(root, "MacOS", "agent-office-collector")
	tray := filepath.Join(trayDir, TrayName)
	for _, f := range []string{core, tray} {
		if err := os.WriteFile(f, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := Locate(func(string) string { return "" }, tray, "darwin"); err != nil || got != core {
		t.Errorf("core from tray bundle: %q %v", got, err)
	}
	if got, ok := FindSibling(filepath.Join(root, "MacOS", GUIName), "darwin", TrayName); !ok || got != tray {
		t.Errorf("tray from gui: %q", got)
	}
	if _, ok := FindSibling(filepath.Join(root, "MacOS", GUIName), "darwin", "missing"); ok {
		t.Error("missing found")
	}
}
