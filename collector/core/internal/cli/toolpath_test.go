package cli

import (
	"path/filepath"
	"testing"
)

func TestToolSearchDirsCoverGUILaunches(t *testing.T) {
	glob := func(pattern string) ([]string, error) {
		return []string{filepath.Join("/h", ".nvm", "versions", "node", "v20.1.0", "bin"), filepath.Join("/h", ".nvm", "versions", "node", "v22.3.0", "bin")}, nil
	}
	env := func(k string) string { return map[string]string{"APPDATA": `C:\U\AppData\Roaming`}[k] }
	mac := toolSearchDirs("darwin", "/h", env, glob)
	for _, want := range []string{"/opt/homebrew/bin", filepath.Join("/h", ".local", "bin"), filepath.Join("/h", ".claude", "local")} {
		if !contains(mac, want) {
			t.Errorf("darwin dirs miss %s: %v", want, mac)
		}
	}
	// nvm은 최신 버전 먼저.
	if i, j := index(mac, filepath.Join("/h", ".nvm", "versions", "node", "v22.3.0", "bin")), index(mac, filepath.Join("/h", ".nvm", "versions", "node", "v20.1.0", "bin")); i < 0 || j < 0 || i > j {
		t.Errorf("nvm order: %v", mac)
	}
	win := toolSearchDirs("windows", `C:\U`, env, glob)
	if !contains(win, filepath.Join(`C:\U\AppData\Roaming`, "npm")) || contains(win, "/opt/homebrew/bin") {
		t.Errorf("windows dirs: %v", win)
	}
}

func contains(xs []string, x string) bool { return index(xs, x) >= 0 }

func index(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
