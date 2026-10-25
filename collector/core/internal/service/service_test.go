package service

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
)

var update = flag.Bool("update", false, "골든 파일 갱신")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("%s mismatch:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestGenerateGoldenPerOS(t *testing.T) {
	cases := []struct {
		name string
		p    Params
	}{
		{"systemd.service.golden", Params{GOOS: "linux", Exe: `/opt/agent office/bin/agent-office-collector`, Path: "/home/u/.config/systemd/user/" + UnitName}},
		{"launchd.plist.golden", Params{GOOS: "darwin", Exe: "/Applications/Agent Office & Co/agent-office-collector", Path: "/Users/u/Library/LaunchAgents/" + LaunchdLabel + ".plist", UID: 501}},
		{"task.xml.golden", Params{GOOS: "windows", Exe: `C:\Program Files\Agent Office\agent-office-collector.exe`, Path: `C:\l\AgentOffice\Collector\service\agent-office-collector-task.xml`, WindowsUser: `PC\user`}},
	}
	for _, c := range cases {
		def, err := Generate(c.p)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		golden(t, c.name, def.Text)
		if !Owned(def.Content) {
			t.Fatalf("%s: marker missing from content", c.name)
		}
		if len(def.Activate) == 0 || len(def.Deactivate) == 0 {
			t.Fatalf("%s: no activation commands", c.name)
		}
	}
}

func TestSystemdQuotingAndWindowsEncoding(t *testing.T) {
	def, err := Generate(Params{GOOS: "linux", Exe: `/x/a"b%c$d\e`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def.Text, `ExecStart="/x/a\"b%%c$$d\\e" run`) {
		t.Fatalf("quoting: %s", def.Text)
	}
	w, err := Generate(Params{GOOS: "windows", Exe: `C:\a<b>.exe`})
	if err != nil {
		t.Fatal(err)
	}
	if w.Content[0] != 0xFF || w.Content[1] != 0xFE || len(w.Content)%2 != 0 {
		t.Fatal("windows task XML must be UTF-16LE with BOM")
	}
	units := make([]uint16, (len(w.Content)-2)/2)
	for i := range units {
		units[i] = uint16(w.Content[2+2*i]) | uint16(w.Content[3+2*i])<<8
	}
	decoded := string(utf16.Decode(units))
	if !strings.Contains(decoded, "\r\n") || !strings.Contains(decoded, `C:\a&lt;b&gt;.exe`) {
		t.Fatal("windows content must be CRLF and XML-escaped")
	}
	if strings.Contains(w.Text, "<UserId>") {
		t.Fatal("UserId must be omitted when unknown")
	}
}

func TestGenerateRejectsBadInput(t *testing.T) {
	for _, p := range []Params{
		{GOOS: "linux", Exe: "relative/bin"},
		{GOOS: "linux", Exe: "/x\nExecStartPre=/bin/evil"},
		{GOOS: "windows", Exe: `\\no-drive`},
	} {
		if _, err := Generate(p); err != ErrInvalidExecutable {
			t.Fatalf("%+v: %v", p, err)
		}
	}
	if _, err := Generate(Params{GOOS: "plan9", Exe: "/x"}); err != ErrUnsupportedOS {
		t.Fatalf("plan9: %v", err)
	}
}

func TestLocation(t *testing.T) {
	env := func(m map[string]string) paths.Env { return func(k string) string { return m[k] } }
	d := paths.Dirs{Home: "/home/u", State: "/s"}
	if p, _ := Location("linux", env(nil), d); p != "/home/u/.config/systemd/user/"+UnitName {
		t.Fatal(p)
	}
	if p, _ := Location("linux", env(map[string]string{"XDG_CONFIG_HOME": "/xc"}), d); p != "/xc/systemd/user/"+UnitName {
		t.Fatal(p)
	}
	if p, _ := Location("darwin", env(nil), paths.Dirs{Home: "/Users/u"}); p != "/Users/u/Library/LaunchAgents/"+LaunchdLabel+".plist" {
		t.Fatal(p)
	}
	if p, _ := Location("windows", env(nil), paths.Dirs{Home: `C:\u`, State: `C:\l\AO`}); p != `C:\l\AO\service\agent-office-collector-task.xml` {
		t.Fatal(p)
	}
}

type recorder struct {
	calls [][]string
	query []byte
	fail  string
}

func (r *recorder) run(args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	if len(args) > 1 && args[1] == "/Query" {
		if r.query == nil {
			return nil, errors.New("not found")
		}
		return r.query, nil
	}
	if r.fail != "" && strings.Join(args, " ") == r.fail {
		return nil, errors.New("failed")
	}
	return nil, nil
}

func linuxDef(t *testing.T, dir string) Definition {
	t.Helper()
	def, err := Generate(Params{GOOS: "linux", Exe: "/usr/bin/agent-office-collector", Path: filepath.Join(dir, "systemd", "user", UnitName)})
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func TestInstallUninstallOwnedOnly(t *testing.T) {
	dir := t.TempDir()
	def := linuxDef(t, dir)
	rec := &recorder{}
	res, err := Install(def, true, rec.run)
	if err != nil || res.Replaced || !res.Activated {
		t.Fatalf("install: %+v %v", res, err)
	}
	if len(rec.calls) != 2 || rec.calls[1][2] != "enable" {
		t.Fatalf("calls %v", rec.calls)
	}
	res, err = Install(def, false, rec.run)
	if err != nil || !res.Replaced {
		t.Fatalf("reinstall: %+v %v", res, err)
	}
	if _, err := Uninstall(def, true, rec.run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(def.Path); !os.IsNotExist(err) {
		t.Fatal("unit not removed")
	}
	if _, err := Uninstall(def, false, rec.run); err != ErrNotInstalled {
		t.Fatalf("second uninstall: %v", err)
	}
}

func TestForeignFileIsNeverTouched(t *testing.T) {
	dir := t.TempDir()
	def := linuxDef(t, dir)
	if err := os.MkdirAll(filepath.Dir(def.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("[Service]\nExecStart=/usr/bin/something-else\n")
	if err := os.WriteFile(def.Path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	if _, err := Install(def, true, rec.run); err != ErrForeign {
		t.Fatalf("install over foreign: %v", err)
	}
	if _, err := Uninstall(def, true, rec.run); err != ErrForeign {
		t.Fatalf("uninstall foreign: %v", err)
	}
	if got, _ := os.ReadFile(def.Path); string(got) != string(foreign) {
		t.Fatal("foreign file modified")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("commands ran for foreign file: %v", rec.calls)
	}
}

func TestWindowsForeignTaskIsNotReplaced(t *testing.T) {
	dir := t.TempDir()
	def, err := Generate(Params{GOOS: "windows", Exe: `C:\ao.exe`, Path: filepath.Join(dir, "task.xml")})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{query: []byte("<Task>someone else</Task>")}
	if _, err := Install(def, true, rec.run); err != ErrForeign {
		t.Fatalf("foreign task: %v", err)
	}
	if _, err := os.Stat(def.Path); !os.IsNotExist(err) {
		t.Fatal("definition written despite foreign task")
	}
	rec = &recorder{}
	if _, err := Install(def, true, rec.run); err != nil {
		t.Fatal(err)
	}
	// 등록된 작업이 남의 것으로 바뀌었으면 삭제하지 않는다.
	rec = &recorder{query: []byte("<Task>replaced</Task>")}
	if _, err := Uninstall(def, true, rec.run); err != ErrForeign {
		t.Fatalf("uninstall foreign task: %v", err)
	}
	rec = &recorder{query: def.Content}
	if _, err := Uninstall(def, true, rec.run); err != nil {
		t.Fatal(err)
	}
	var deleted bool
	for _, c := range rec.calls {
		if c[1] == "/Delete" {
			deleted = true
		}
	}
	if !deleted {
		t.Fatalf("owned task not deleted: %v", rec.calls)
	}
}

func TestActivationFailureIsReported(t *testing.T) {
	def := linuxDef(t, t.TempDir())
	rec := &recorder{fail: "systemctl --user enable --now " + UnitName}
	_, err := Install(def, true, rec.run)
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("err %v", err)
	}
}
