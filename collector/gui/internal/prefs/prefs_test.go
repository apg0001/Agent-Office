package prefs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPath(t *testing.T) {
	p, err := Path(func(k string) string {
		if k == EnvHome {
			return "/x/ao"
		}
		return ""
	}, nil)
	if err != nil || p != filepath.Join("/x/ao", "gui", "gui.json") {
		t.Errorf("override: %q %v", p, err)
	}
	p, err = Path(func(string) string { return "" }, func() (string, error) { return "/cfg", nil })
	if err != nil || p != filepath.Join("/cfg", "AgentOffice", "CollectorGUI", "gui.json") {
		t.Errorf("default: %q %v", p, err)
	}
	if _, err := Path(func(string) string { return "" }, func() (string, error) { return "", errors.New("no home") }); err == nil {
		t.Error("expected error")
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui", "gui.json")
	if p, err := Load(path); err != nil || p.ServerURL != "" {
		t.Fatalf("missing file: %+v %v", p, err)
	}
	if err := Save(path, Prefs{ServerURL: "https://office.example", DeviceName: "내 PC"}); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil || p.ServerURL != "https://office.example" || p.DeviceName != "내 PC" || p.SchemaVersion != SchemaVersion {
		t.Fatalf("load: %+v %v", p, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		di, _ := os.Stat(filepath.Dir(path))
		if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
			t.Errorf("perms file %v dir %v", fi.Mode().Perm(), di.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestLoadInvalidAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gui.json")
	for _, bad := range []string{"{", `{"schema_version":99}`, "[]"} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(dir, "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); !errors.Is(err, ErrInvalid) {
		t.Errorf("symlink load: %v", err)
	}
	if err := Save(link, Prefs{ServerURL: "https://x.example"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("symlink save: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != `{"schema_version":1}` {
		t.Error("symlink target modified")
	}
}
