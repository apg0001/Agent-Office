package hooks

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

var updateGolden = flag.Bool("update", false, "골든 파일 갱신")

var now = time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC)

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s 불일치\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func mustSpec(t *testing.T, tool, goos, exe string) Spec {
	t.Helper()
	s, err := Generate(Params{Tool: tool, GOOS: goos, Exe: exe})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGenerateGolden(t *testing.T) {
	cases := []struct{ tool, goos, exe, file string }{
		{ToolClaude, "linux", "/home/u/My Tools/it's/agent-office-collector", "claude-linux.json.golden"},
		{ToolClaude, "darwin", "/Users/u/Applications/Agent Office/agent-office-collector", "claude-darwin.json.golden"},
		{ToolClaude, "windows", `C:\Program Files\AgentOffice\agent-office-collector.exe`, "claude-windows.json.golden"},
		{ToolCodex, "windows", `C:\Program Files\AgentOffice\agent-office-collector.exe`, "codex-windows.json.golden"},
		{ToolCodex, "linux", "/usr/local/bin/agent-office-collector", "codex-linux.json.golden"},
		{ToolCodex, "darwin", "/Users/u/Applications/Agent Office/agent-office-collector", "codex-darwin.json.golden"},
	}
	for _, c := range cases {
		s := mustSpec(t, c.tool, c.goos, c.exe)
		out := s.JSON()
		if !json.Valid(out) {
			t.Fatalf("%s: JSON 아님", c.file)
		}
		golden(t, c.file, out)
		if !IsOwnedCommand(s.Command, c.tool) {
			t.Errorf("%s: 생성 명령을 소유로 인식하지 못함: %s", c.file, s.Command)
		}
		other := map[string]string{ToolClaude: ToolCodex, ToolCodex: ToolClaude}[c.tool]
		if IsOwnedCommand(s.Command, other) {
			t.Errorf("%s: 다른 도구 명령으로 오인", c.file)
		}
	}
}

func TestCommandRejectsUnsafePaths(t *testing.T) {
	bad := []struct{ goos, exe string }{
		{"linux", "relative/agent-office-collector"},
		{"linux", "/tmp/a\nrm -rf /"},
		{"darwin", ""},
		{"windows", `\\server\share\agent-office-collector.exe`},
		{"windows", `C:\a"b\agent-office-collector.exe`},
		{"windows", `C:\%PATH%\agent-office-collector.exe`},
		{"windows", `C:\a$b\agent-office-collector.exe`},
		{"plan9", "/bin/agent-office-collector"},
	}
	for _, b := range bad {
		if _, err := Command(b.goos, b.exe, ToolClaude); err == nil {
			t.Errorf("%s %q 허용됨", b.goos, b.exe)
		}
	}
	if _, err := Generate(Params{Tool: "gemini", GOOS: "linux", Exe: "/x"}); !errors.Is(err, ErrUnsupportedTool) {
		t.Errorf("알 수 없는 도구: %v", err)
	}
}

// Windows 명령은 Hook을 실행하는 셸(cmd·PowerShell·Git Bash)과 관계없이 같은 뜻이어야 한다(#225).
// 안전 문자뿐인 경로(설치 폴더를 8.3 짧은 경로로 바꾼 것)는 따옴표 없이, 아니면 기존 큰따옴표 형식.
func TestWindowsCommandIsQuoteFreeWhenSafe(t *testing.T) {
	cases := []struct{ exe, want string }{
		{`C:\Users\USER\AppData\Local\Programs\AGENTO~1\agent-office-collector.exe`, `C:/Users/USER/AppData/Local/Programs/AGENTO~1/agent-office-collector.exe hook codex`},
		{`C:\Program Files\AgentOffice\agent-office-collector.exe`, `"C:/Program Files/AgentOffice/agent-office-collector.exe" hook codex`},
		{`C:\Users\홍길동\agent-office-collector.exe`, `"C:/Users/홍길동/agent-office-collector.exe" hook codex`},
		{`C:\a(b)\agent-office-collector.exe`, `"C:/a(b)/agent-office-collector.exe" hook codex`},
	}
	for _, c := range cases {
		got, err := Command("windows", c.exe, ToolCodex)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v", c.exe, got, err)
		}
		if !IsOwnedCommand(got, ToolCodex) {
			t.Errorf("%q: 소유 판정 실패", got)
		}
	}
	for _, bad := range []string{"", "C:", "C:/", "c:x/y", "1:/a", "C:/a b", "C:/a&b", "C:/a;b", "C:/a'b", "C:/a@b"} {
		if AnyShellSafe(bad) {
			t.Errorf("AnyShellSafe(%q) = true", bad)
		}
	}
}

func TestIsOwnedCommand(t *testing.T) {
	owned := []string{
		`'/opt/x/agent-office-collector' hook claude`,
		`/usr/bin/agent-office-collector hook claude --mode print`,
		`"C:/Program Files/AgentOffice/Agent-Office-Collector.EXE" hook claude`,
		`'/a/it'\''s/agent-office-collector' hook claude`,
		`C:/Users/USER/AppData/Local/Programs/AGENTO~1/agent-office-collector.exe hook claude`,
	}
	for _, c := range owned {
		if !IsOwnedCommand(c, ToolClaude) {
			t.Errorf("소유 아님으로 판정: %s", c)
		}
	}
	foreign := []string{
		`python3 /home/u/hook.py`,
		`/usr/bin/agent-office-collector run`,
		`/usr/bin/agent-office-collector-old hook claude`,
		`echo agent-office-collector hook claude`,
		`/usr/bin/agent-office-collector hook codex`,
		`'unterminated hook claude`,
		``,
	}
	for _, c := range foreign {
		if IsOwnedCommand(c, ToolClaude) {
			t.Errorf("남의 명령을 소유로 판정: %s", c)
		}
	}
}

const foreignSettings = `{
  "model": "sonnet",
  "permissions": {"allow": ["Bash(git status)"], "deny": []},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "python3 ~/guard.py", "timeout": 10}]}
    ],
    "Notification": [
      {"hooks": [{"type": "command", "command": "notify-send done"}]}
    ]
  },
  "statusLine": {"type": "command", "command": "echo hi", "padding": 0},
  "x-unknown": [1.50, true, null, {"nested": "<&>"}]
}
`

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("결과가 JSON이 아님: %v\n%s", err, data)
	}
	return v
}

func linuxSpec(t *testing.T, exe string) Spec {
	return mustSpec(t, ToolClaude, "linux", exe)
}

func TestInstallPreservesForeignAndUninstallRestores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeFile(t, path, foreignSettings, 0o640)
	before := readJSON(t, path)
	s := linuxSpec(t, "/opt/Agent Office/agent-office-collector")

	// 미리보기는 쓰지 않는다.
	res, err := Install(path, s, now, false)
	if err != nil || res.Wrote || len(res.Changes) != len(s.Events) {
		t.Fatalf("미리보기: %+v %v", res, err)
	}
	if got, _ := os.ReadFile(path); string(got) != foreignSettings {
		t.Fatal("미리보기가 파일을 바꿈")
	}

	res, err = Install(path, s, now, true)
	if err != nil || !res.Wrote {
		t.Fatalf("설치: %+v %v", res, err)
	}
	for _, c := range res.Changes {
		if c.Action != ActionAdd {
			t.Errorf("첫 설치는 add여야 함: %+v", c)
		}
	}
	// 백업: 원본 바이트 그대로, 0600.
	if res.Backup == "" || !strings.HasPrefix(filepath.Base(res.Backup), "settings.json"+BackupInfix+"20261023T010203Z") {
		t.Fatalf("백업 이름: %q", res.Backup)
	}
	if b, _ := os.ReadFile(res.Backup); string(b) != foreignSettings {
		t.Error("백업 내용이 원본과 다름")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if bi, _ := os.Stat(res.Backup); bi.Mode().Perm() != 0o600 {
			t.Errorf("백업 권한 %v", bi.Mode().Perm())
		}
		if fi.Mode().Perm() != 0o640 {
			t.Errorf("파일 권한 보존 실패: %v", fi.Mode().Perm())
		}
	}
	installed := readJSON(t, path).(map[string]any)
	for _, k := range []string{"model", "permissions", "statusLine", "x-unknown"} {
		if !reflect.DeepEqual(installed[k], before.(map[string]any)[k]) {
			t.Errorf("알 수 없는 키 %s가 바뀜", k)
		}
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"python3 ~/guard.py", "notify-send done", `"<&>"`, "1.50"} {
		if !strings.Contains(text, want) {
			t.Errorf("남의 값 %q 손실", want)
		}
	}
	// 키 순서 보존: model이 hooks보다 앞, x-unknown이 마지막.
	if !(strings.Index(text, `"model"`) < strings.Index(text, `"hooks"`) && strings.Index(text, `"statusLine"`) < strings.Index(text, `"x-unknown"`)) {
		t.Error("최상위 키 순서가 바뀜")
	}
	pre := installed["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse 그룹 수 %d", len(pre))
	}
	st, err := Inspect(path, s)
	if err != nil || st.State != StateInstalled || st.Foreign != 2 || st.Current != len(s.Events) {
		t.Fatalf("상태: %+v %v", st, err)
	}

	// 두 번째 설치는 변경 없음.
	res, err = Install(path, s, now.Add(time.Second), true)
	if err != nil || res.Wrote || len(res.Changes) != 0 || res.Backup != "" {
		t.Fatalf("멱등 설치: %+v %v", res, err)
	}
	if again, _ := os.ReadFile(path); string(again) != text {
		t.Error("멱등 설치가 파일을 바꿈")
	}

	res, err = Uninstall(path, s, now.Add(2*time.Second), true)
	if err != nil || !res.Wrote || res.Backup == "" {
		t.Fatalf("제거: %+v %v", res, err)
	}
	if after := readJSON(t, path); !reflect.DeepEqual(after, before) {
		t.Errorf("제거 후 원본과 의미가 다름\n%v\n%v", after, before)
	}
	res, err = Uninstall(path, s, now.Add(3*time.Second), true)
	if err != nil || res.Wrote || len(res.Changes) != 0 {
		t.Fatalf("두 번째 제거: %+v %v", res, err)
	}
	backups, _ := filepath.Glob(path + BackupInfix + "*")
	if len(backups) != 2 {
		t.Errorf("백업 개수 %d", len(backups))
	}
}

func TestInstallCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude", "settings.json")
	s := linuxSpec(t, "/usr/local/bin/agent-office-collector")
	res, err := Install(path, s, now, true)
	if err != nil || !res.Wrote || res.Backup != "" {
		t.Fatalf("%+v %v", res, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("새 파일 권한 %v", fi.Mode().Perm())
		}
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(s.JSON()) {
		t.Errorf("빈 상태에서 설치 결과가 print와 다름\n%s", got)
	}
	if _, err := Uninstall(path, s, now, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("제거 후 %s", got)
	}
}

func TestReplaceMovedExecutableAndPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	old := linuxSpec(t, "/old/agent-office-collector")
	if _, err := Install(path, old, now, true); err != nil {
		t.Fatal(err)
	}
	cur := linuxSpec(t, "/new/agent-office-collector")
	if st, _ := Inspect(path, cur); st.State != StateModified {
		t.Errorf("이동한 실행 파일 상태 %s", st.State)
	}
	res, err := Install(path, cur, now, true)
	if err != nil || len(res.Changes) != len(cur.Events) || res.Changes[0].Action != ActionReplace {
		t.Fatalf("%+v %v", res, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "/old/") || strings.Count(string(data), "/new/agent-office-collector") != len(cur.Events) {
		t.Errorf("교체 결과\n%s", data)
	}

	// 사용자가 이벤트 하나를 지우면 partial.
	var doc map[string]map[string]any
	_ = json.Unmarshal(data, &doc)
	delete(doc["hooks"], "Stop")
	b, _ := json.Marshal(doc)
	writeFile(t, path, string(b), 0o600)
	st, _ := Inspect(path, cur)
	if st.State != StatePartial || st.Missing != 1 {
		t.Errorf("partial 기대: %+v", st)
	}
	res, _ = Install(path, cur, now, false)
	if len(res.Changes) != 1 || res.Changes[0] != (Change{Event: "Stop", Action: ActionAdd}) {
		t.Errorf("누락 이벤트만 추가해야 함: %+v", res.Changes)
	}
}

func TestOwnedHandlerSharingForeignGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	content := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"},{"type":"command","command":"'/x/agent-office-collector' hook claude","timeout":5}]}]}}`
	writeFile(t, path, content, 0o600)
	s := linuxSpec(t, "/x/agent-office-collector")
	if _, err := Uninstall(path, s, now, true); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, path)
	want := map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "say done"}}}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("남의 핸들러만 남아야 함: %v", got)
	}
}

func TestRefusesUnparseableAndOddShapes(t *testing.T) {
	s := linuxSpec(t, "/x/agent-office-collector")
	cases := map[string]string{
		"comment":   "// c\n{}",
		"trailing":  `{"a":1} {"b":2}`,
		"duplicate": `{"hooks":{},"hooks":{}}`,
		"array":     `[]`,
		"broken":    `{"model":`,
		"hooksType": `{"hooks":[]}`,
		"eventType": `{"hooks":{"Stop":{"x":1}}}`,
	}
	for name, content := range cases {
		path := filepath.Join(t.TempDir(), "settings.json")
		writeFile(t, path, content, 0o600)
		_, err := Install(path, s, now, true)
		if !errors.Is(err, ErrParse) && !errors.Is(err, ErrShape) {
			t.Errorf("%s: 거부해야 함: %v", name, err)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Errorf("%s: 파일이 바뀜", name)
		}
		if b, _ := filepath.Glob(path + BackupInfix + "*"); len(b) != 0 {
			t.Errorf("%s: 거부 시 백업이 생김", name)
		}
		if st, err := Inspect(path, s); err == nil || st.State != StateUnreadable {
			t.Errorf("%s: 상태 %+v %v", name, st, err)
		}
	}
}

func TestRefusesReadOnlyAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX 권한·심볼릭 링크 전용")
	}
	s := linuxSpec(t, "/x/agent-office-collector")
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	writeFile(t, path, `{"model":"x"}`, 0o444)
	res, err := Install(path, s, now, false)
	if err != nil || len(res.Changes) == 0 {
		t.Fatalf("읽기 전용도 미리보기는 가능해야 함: %v", err)
	}
	if _, err := Install(path, s, now, true); !errors.Is(err, ErrReadOnly) {
		t.Errorf("읽기 전용: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"model":"x"}` {
		t.Error("읽기 전용 파일이 바뀜")
	}

	real := filepath.Join(dir, "real.json")
	writeFile(t, real, `{}`, 0o600)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if _, err := Install(link, s, now, true); !errors.Is(err, ErrNotRegular) {
		t.Errorf("심볼릭 링크: %v", err)
	}

	if os.Geteuid() != 0 {
		ro := filepath.Join(dir, "rodir")
		_ = os.Mkdir(ro, 0o700)
		p := filepath.Join(ro, "settings.json")
		writeFile(t, p, `{}`, 0o600)
		_ = os.Chmod(ro, 0o500)
		defer os.Chmod(ro, 0o700)
		if _, err := Install(p, s, now, true); err == nil {
			t.Error("쓸 수 없는 디렉터리에서 성공")
		}
		if got, _ := os.ReadFile(p); string(got) != `{}` {
			t.Error("쓸 수 없는 디렉터리의 파일이 바뀜")
		}
	}
}

func TestParseVersionAndAssess(t *testing.T) {
	cases := map[string]string{
		"2.1.308 (Claude Code)\n":                 "2.1.308",
		"codex-cli 0.162.0-alpha.17.2\r\nextra":   "0.162.0-alpha.17.2",
		"codex-cli 0.163.0\n":                     "0.163.0",
		"no version here":                         "",
		"\n2.1.308":                               "",
		"/home/secret/path 1.2 error":             "",
		"warning: v1.2.3. something else 9.9.9\n": "1.2.3",
	}
	for in, want := range cases {
		if got := ParseVersion([]byte(in)); got != want {
			t.Errorf("ParseVersion(%q)=%q want %q", in, got, want)
		}
	}
	if s := AssessVersion(ToolClaude, "2.1.308", "linux"); s.Level != adapter.SupportVerified || s.Mode != "print" {
		t.Errorf("claude linux: %+v", s)
	}
	if s := AssessVersion(ToolClaude, "2.1.308", "windows"); s.Level != adapter.SupportUnverified {
		t.Errorf("claude windows: %+v", s)
	}
	if s := AssessVersion(ToolCodex, "0.162.0-alpha.17.2", "windows"); s.Level != adapter.SupportVerified || s.Mode != "exec" {
		t.Errorf("codex windows: %+v", s)
	}
	if s := AssessVersion(ToolCodex, "0.163.0", "windows"); s.Level != adapter.SupportUnverified {
		t.Errorf("codex 0.163 windows: %+v", s)
	}
	if s := AssessVersion(ToolCodex, "0.163.0", "linux"); s.Level != adapter.SupportVerified || s.Mode != "exec" {
		t.Errorf("codex 0.163 linux(#170): %+v", s)
	}
	if s := AssessVersion(ToolCodex, "0.163.0", "darwin"); s.Level != adapter.SupportUnverified {
		t.Errorf("codex 0.163 darwin: %+v", s)
	}
	if s := AssessVersion(ToolClaude, "", "linux"); s.Level != adapter.SupportUnverified {
		t.Errorf("빈 버전: %+v", s)
	}
}

// TestPOSIXCommandRunsUnderSh는 생성한 명령을 실제 sh로 실행해 공백·작은따옴표·$ 경로 인용을 확인한다.
func TestPOSIXCommandRunsUnderSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh 전용")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh 없음")
	}
	dir := filepath.Join(t.TempDir(), "My Tools", "it's $HOME")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, ExeBase)
	out := filepath.Join(t.TempDir(), "args")
	writeFile(t, exe, "#!/bin/sh\nprintf '%s|' \"$@\" > '"+out+"'\nexit 0\n", 0o700)
	cmd, err := Command(runtime.GOOS, exe, ToolClaude)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(sh, "-c", cmd).Run(); err != nil {
		t.Fatalf("sh 실행 실패: %v", err)
	}
	if got, _ := os.ReadFile(out); string(got) != "hook|claude|" {
		t.Errorf("인자: %q", got)
	}
}

func TestClaudeSettingsPath(t *testing.T) {
	if got := ClaudeSettingsPath("/h/.claude/projects"); got != "/h/.claude/settings.json" {
		t.Error(got)
	}
	if got := ClaudeSettingsPath(`C:\Users\u\.claude\projects`); got != `C:\Users\u\.claude\settings.json` {
		t.Error(got)
	}
}

func TestCodexHooksPath(t *testing.T) {
	if got := CodexHooksPath(filepath.Join("h", ".codex")); got != filepath.Join("h", ".codex", "hooks.json") {
		t.Error(got)
	}
}

// Codex hooks.json(#170): SessionEnd는 Codex 상한 3초, 이전 print 출력(5초)은 변경됨으로 보고 교체한다.
func TestCodexInstallSessionEndTimeoutAndReplace(t *testing.T) {
	s := mustSpec(t, ToolCodex, "linux", "/usr/local/bin/agent-office-collector")
	if got := s.TimeoutFor(Event{Name: "SessionEnd", Timeout: CodexSessionEndTimeoutSeconds}); got != 3 {
		t.Fatalf("SessionEnd timeout %d", got)
	}
	path := filepath.Join(t.TempDir(), "hooks.json")
	// 이전 hooks print 출력처럼 모든 이벤트 timeout 5.
	old := strings.ReplaceAll(string(s.JSON()), `"timeout": 3`, `"timeout": 5`)
	writeFile(t, path, old, 0o600)
	st, err := Inspect(path, s)
	if err != nil || st.State != StateModified || st.Modified != 1 || st.Current != 8 {
		t.Fatalf("이전 출력 상태: %+v %v", st, err)
	}
	res, err := Install(path, s, now, true)
	if err != nil || !res.Wrote || len(res.Changes) != 1 || res.Changes[0] != (Change{Event: "SessionEnd", Action: ActionReplace}) {
		t.Fatalf("교체: %+v %v", res, err)
	}
	if st, _ := Inspect(path, s); st.State != StateInstalled {
		t.Fatalf("교체 후: %+v", st)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(s.JSON()) {
		t.Errorf("교체 결과가 생성 결과와 다름:\n%s", got)
	}
	res, err = Uninstall(path, s, now, true)
	if err != nil || len(res.Changes) != 9 {
		t.Fatalf("제거: %+v %v", res, err)
	}
	if got, _ := os.ReadFile(path); strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("제거 후: %s", got)
	}
}
