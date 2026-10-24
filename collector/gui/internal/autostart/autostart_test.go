package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestGenerateLocations(t *testing.T) {
	cases := []struct {
		p    Params
		path string
	}{
		{Params{GOOS: "linux", Exe: "/opt/ao/agent-office-collector-gui", Home: "/home/u"}, "/home/u/.config/autostart/" + DesktopFile},
		{Params{GOOS: "linux", Exe: "/opt/ao/agent-office-collector-gui", Home: "/home/u", Getenv: env(map[string]string{"XDG_CONFIG_HOME": "/cfg"})}, "/cfg/autostart/" + DesktopFile},
		{Params{GOOS: "linux", Exe: "/opt/ao/agent-office-collector-gui", Home: "/home/u", Getenv: env(map[string]string{"XDG_CONFIG_HOME": "relative"})}, "/home/u/.config/autostart/" + DesktopFile},
		{Params{GOOS: "darwin", Exe: "/Applications/AO.app/Contents/MacOS/agent-office-collector-gui", Home: "/Users/u"}, "/Users/u/Library/LaunchAgents/" + LaunchdLabel + ".plist"},
		{Params{GOOS: "windows", Exe: `C:\Program Files\AO\agent-office-collector-gui.exe`}, RunKey + `\` + RunValue},
	}
	for _, c := range cases {
		d, err := Generate(c.p)
		if err != nil {
			t.Fatalf("%s: %v", c.p.GOOS, err)
		}
		if d.Path != c.path {
			t.Errorf("%s path %q", c.p.GOOS, d.Path)
		}
		if !Owned(c.p.GOOS, d.Content) {
			t.Errorf("%s: generated content not owned", c.p.GOOS)
		}
		if !strings.Contains(d.Content, Arg) {
			t.Errorf("%s: missing %s", c.p.GOOS, Arg)
		}
	}
}

func TestGenerateRejects(t *testing.T) {
	for _, p := range []Params{
		{GOOS: "linux", Exe: "relative/gui", Home: "/home/u"},
		{GOOS: "linux", Exe: "/tmp/go-build123/b001/exe/gui", Home: "/home/u"},
		{GOOS: "linux", Exe: "/opt/gui\nExec=evil", Home: "/home/u"},
		{GOOS: "linux", Exe: "/opt/gui", Home: ""},
		{GOOS: "windows", Exe: `C:\a"b\gui.exe`},
		{GOOS: "windows", Exe: `\\server\share\gui.exe`},
		{GOOS: "plan9", Exe: "/opt/gui", Home: "/home/u"},
	} {
		if _, err := Generate(p); err == nil {
			t.Errorf("accepted %+v", p)
		}
	}
}

// AO_AUTOSTART_DUMP 디렉터리가 있으면 생성한 정의를 써 둔다(desktop-file-validate 등 외부 검사용 절차).
func TestDumpForExternalValidation(t *testing.T) {
	dir := os.Getenv("AO_AUTOSTART_DUMP")
	if dir == "" {
		t.Skip("AO_AUTOSTART_DUMP 미설정: 외부 검사기(desktop-file-validate)용 덤프는 수동 절차에서만 만든다")
	}
	for _, p := range []Params{
		{GOOS: "linux", Exe: "/opt/Agent Office/agent-office-collector-gui", Home: "/home/u"},
		{GOOS: "darwin", Exe: "/Applications/Agent Office.app/Contents/MacOS/agent-office-collector-gui", Home: "/Users/u"},
	} {
		d, err := Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(d.Path)), []byte(d.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDesktopExecQuoting(t *testing.T) {
	d, _ := Generate(Params{GOOS: "linux", Exe: `/opt/My Apps/a"b$c%d\e` + "`f/gui", Home: "/home/u"})
	want := `Exec="/opt/My Apps/a\\"b\\$c%%d\\\\e\\` + "`" + `f/gui" --autostart`
	if !strings.Contains(d.Content, want+"\n") {
		t.Fatalf("exec line:\n%s\nwant %s", d.Content, want)
	}
	for _, k := range []string{"[Desktop Entry]", "Type=Application", "X-AgentOffice-Managed=" + Marker, "Terminal=false"} {
		if !strings.Contains(d.Content, k) {
			t.Errorf("missing %q", k)
		}
	}
}

func TestPlistEscapes(t *testing.T) {
	d, _ := Generate(Params{GOOS: "darwin", Exe: "/Apps/A&B <x>/gui", Home: "/Users/u"})
	if !strings.Contains(d.Content, "<string>/Apps/A&amp;B &lt;x&gt;/gui</string>") || !strings.Contains(d.Content, "<!-- "+Marker+" -->") {
		t.Fatalf("plist:\n%s", d.Content)
	}
	if strings.Contains(d.Content, "KeepAlive") {
		t.Error("GUI autostart must not be kept alive by launchd (quit from tray must stay quit)")
	}
}

func TestOwnedWindows(t *testing.T) {
	for v, want := range map[string]bool{
		`"C:\AO\agent-office-collector-gui.exe" --autostart`:           true,
		`"C:\AO\AGENT-OFFICE-COLLECTOR-GUI.EXE" --autostart`:           true,
		`"C:\AO\agent-office-collector-gui.exe"`:                       false,
		`"C:\AO\other.exe" --autostart`:                                false,
		`C:\AO\agent-office-collector-gui.exe --autostart`:             false,
		`"C:\AO\agent-office-collector-gui.exe" --autostart --evil`:    false,
		`"C:\AO\agent-office-collector-gui.exe.bak\x.exe" --autostart`: false,
		``: false,
	} {
		if got := Owned("windows", v); got != want {
			t.Errorf("Owned(%q) = %v", v, got)
		}
	}
}

func TestInstallUninstallFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX 경로 테스트")
	}
	home := t.TempDir()
	def, err := Generate(Params{GOOS: "linux", Exe: "/opt/ao/agent-office-collector-gui", Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if s := State(def, nil); s != "absent" {
		t.Fatalf("state %s", s)
	}
	if err := Uninstall(def, nil); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("uninstall absent: %v", err)
	}
	if replaced, err := Install(def, nil); err != nil || replaced {
		t.Fatalf("install: %v %v", replaced, err)
	}
	if s := State(def, nil); s != "installed" {
		t.Fatalf("state %s", s)
	}
	entries, _ := os.ReadDir(filepath.Dir(def.Path))
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
	// 실행 파일 위치가 바뀌면 outdated, 다시 설치하면 갱신.
	moved, _ := Generate(Params{GOOS: "linux", Exe: "/usr/local/bin/agent-office-collector-gui", Home: home})
	if s := State(moved, nil); s != "outdated" {
		t.Fatalf("state %s", s)
	}
	if replaced, err := Install(moved, nil); err != nil || !replaced {
		t.Fatalf("reinstall: %v %v", replaced, err)
	}
	if err := Uninstall(moved, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(def.Path); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}

	// 소유하지 않은 같은 이름 파일은 덮어쓰거나 지우지 않는다.
	foreign := "[Desktop Entry]\nExec=/usr/bin/something-else\n"
	if err := os.WriteFile(def.Path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(def, nil); !errors.Is(err, ErrForeign) {
		t.Fatalf("install over foreign: %v", err)
	}
	if err := Uninstall(def, nil); !errors.Is(err, ErrForeign) {
		t.Fatalf("uninstall foreign: %v", err)
	}
	if b, _ := os.ReadFile(def.Path); string(b) != foreign {
		t.Fatal("foreign file changed")
	}
	if s := State(def, nil); s != "foreign" {
		t.Fatalf("state %s", s)
	}

	// 심볼릭 링크는 따라가지 않는다.
	_ = os.Remove(def.Path)
	target := filepath.Join(home, "target")
	if err := os.WriteFile(target, []byte(def.Content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, def.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(def, nil); !errors.Is(err, ErrForeign) {
		t.Fatalf("install over symlink: %v", err)
	}
	if err := Uninstall(def, nil); !errors.Is(err, ErrForeign) {
		t.Fatalf("uninstall symlink: %v", err)
	}
}

func TestInstallUninstallWindowsRegistry(t *testing.T) {
	def, _ := Generate(Params{GOOS: "windows", Exe: `C:\AO\agent-office-collector-gui.exe`})
	reg := &Memory{}
	if s := State(def, reg); s != "absent" {
		t.Fatal(s)
	}
	if _, err := Install(def, reg); err != nil {
		t.Fatal(err)
	}
	if reg.Values[RunValue] != `"C:\AO\agent-office-collector-gui.exe" --autostart` || State(def, reg) != "installed" {
		t.Fatalf("value: %q", reg.Values[RunValue])
	}
	if err := Uninstall(def, reg); err != nil || len(reg.Values) != 0 {
		t.Fatalf("uninstall: %v %v", err, reg.Values)
	}
	reg.Values = map[string]string{RunValue: `"C:\Other\tool.exe" /start`}
	if _, err := Install(def, reg); !errors.Is(err, ErrForeign) {
		t.Fatal("overwrote foreign Run value")
	}
	if err := Uninstall(def, reg); !errors.Is(err, ErrForeign) || reg.Values[RunValue] == "" {
		t.Fatal("removed foreign Run value")
	}
	if _, err := Install(def, nil); !errors.Is(err, ErrUnsupportedOS) {
		t.Fatalf("nil registry: %v", err)
	}
	if _, err := Install(def, &Memory{Fail: true}); err == nil {
		t.Fatal("registry failure not reported")
	}
}
