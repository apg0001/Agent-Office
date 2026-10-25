package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/hooks"
)

const userSettings = `{
  "theme": "dark",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "my-notifier --done"}]}]
  }
}
`

func TestHooksInstallStatusUninstall(t *testing.T) {
	te := newTestEnv(t, "u s")
	te.env.ToolVersion = func(tool string) (string, error) {
		if tool == "claude" {
			return "2.1.308", nil
		}
		return "", errors.New("not found")
	}
	te.register("https://office.example")
	settings := hooks.ClaudeSettingsPath(te.dirs.ClaudeProjects)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(userSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	// --yes 없이는 미리보기만.
	if code := te.run("", "hooks", "install"); code != ExitOK {
		t.Fatalf("preview exit %d: %s", code, te.stderr)
	}
	out := te.stdout.String()
	if !strings.Contains(out, "+ SessionStart") || !strings.Contains(out, "--yes") || !strings.Contains(out, "2.1.308") {
		t.Errorf("미리보기 출력:\n%s", out)
	}
	if b, _ := os.ReadFile(settings); string(b) != userSettings {
		t.Fatal("--yes 없이 파일이 바뀜")
	}

	if code := te.run("", "hooks", "install", "--yes"); code != ExitOK {
		t.Fatalf("install exit %d: %s", code, te.stderr)
	}
	out = te.stdout.String()
	if !strings.Contains(out, "백업") || strings.Contains(out, te.home) {
		t.Errorf("설치 출력(홈 경로는 ~로 표시해야 함):\n%s", out)
	}
	cfg, err := config.Load(te.dirs.ConfigFile())
	if err != nil || cfg.ClaudeVersion != "2.1.308" || cfg.ClaudeMode != "" {
		t.Errorf("감지 버전 기록: %+v %v", cfg.ClaudeVersion, err)
	}

	if code := te.run("", "hooks", "status", "--tool", "claude"); code != ExitOK {
		t.Fatalf("status exit %d", code)
	}
	if !strings.Contains(te.stdout.String(), "installed") || !strings.Contains(te.stdout.String(), "다른 Hook 1개") {
		t.Errorf("status:\n%s", te.stdout)
	}
	if strings.Contains(te.stdout.String(), sentinelToken) || strings.Contains(te.stdout.String(), te.home) {
		t.Error("status에 토큰·경로 노출")
	}

	if code := te.run("", "hooks", "install", "--yes"); code != ExitOK || !strings.Contains(te.stdout.String(), "변경 없음") {
		t.Fatalf("두 번째 설치: %d\n%s", code, te.stdout)
	}

	if code := te.run("", "hooks", "uninstall", "--yes"); code != ExitOK {
		t.Fatalf("uninstall exit %d: %s", code, te.stderr)
	}
	var got, want any
	b, _ := os.ReadFile(settings)
	_ = json.Unmarshal(b, &got)
	_ = json.Unmarshal([]byte(userSettings), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("제거 후 원본과 다름:\n%s", b)
	}
}

func TestHooksRefusals(t *testing.T) {
	te := newTestEnv(t, "u")
	te.env.ToolVersion = func(string) (string, error) { return "", errors.New("none") }
	settings := hooks.ClaudeSettingsPath(te.dirs.ClaudeProjects)
	_ = os.MkdirAll(filepath.Dir(settings), 0o700)
	broken := `{"hooks": {` // 깨진 JSON
	_ = os.WriteFile(settings, []byte(broken), 0o600)
	if code := te.run("", "hooks", "install", "--yes"); code != ExitError {
		t.Errorf("깨진 설정 install exit %d", code)
	}
	if b, _ := os.ReadFile(settings); string(b) != broken {
		t.Error("깨진 설정 파일이 바뀜")
	}
	if code := te.run("", "hooks", "status", "--tool", "claude"); code != ExitError || !strings.Contains(te.stdout.String(), "unreadable") {
		t.Errorf("status: %d %s", code, te.stdout)
	}
	// Codex 설정 폴더(~/.codex)가 없으면 만들지 않고 거부한다.
	if code := te.run("", "hooks", "install", "--tool", "codex", "--yes"); code != ExitError || !strings.Contains(te.stderr.String(), "Codex 설정 폴더가 없습니다") {
		t.Errorf("codex 홈 없음 install: %d %s", code, te.stderr)
	}
	if _, err := os.Stat(te.dirs.CodexHome); !os.IsNotExist(err) {
		t.Error("Codex 홈을 만들면 안 됨")
	}
	if code := te.run("", "hooks", "install", "--scope", "project"); code != ExitUsage {
		t.Errorf("scope: %d", code)
	}
	if code := te.run("", "hooks", "install", "--os", "windows"); code != ExitUsage {
		t.Errorf("install --os: %d", code)
	}
	if code := te.run("", "hooks", "frobnicate"); code != ExitUsage {
		t.Errorf("알 수 없는 하위 명령: %d", code)
	}
}

func TestHooksPrint(t *testing.T) {
	te := newTestEnv(t, "u")
	if code := te.run("", "hooks", "print", "--tool", "codex", "--os", "windows", "--exe", `C:\Program Files\AO\agent-office-collector.exe`); code != ExitOK {
		t.Fatalf("print exit %d: %s", code, te.stderr)
	}
	if !json.Valid(te.stdout.Bytes()) || !strings.Contains(te.stdout.String(), `\"C:/Program Files/AO/agent-office-collector.exe\" hook codex`) {
		t.Errorf("print:\n%s", te.stdout)
	}
	if code := te.run("", "hooks", "print", "--os", "linux", "--exe", "relative"); code != ExitError {
		t.Errorf("상대 경로 print: %d", code)
	}
}

// GUI(#40)가 읽는 `hooks status --json`은 버전·enum·개수만 담는다.
func TestHooksStatusJSON(t *testing.T) {
	te := newTestEnv(t, "u")
	te.env.ToolVersion = func(tool string) (string, error) {
		if tool == "claude" {
			return "2.1.308", nil
		}
		return "", errors.New("not found")
	}
	settings := hooks.ClaudeSettingsPath(te.dirs.ClaudeProjects)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(userSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := te.run("", "hooks", "status", "--json"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, te.stderr)
	}
	var out struct {
		OS    string       `json:"os"`
		Tools []ToolReport `json:"tools"`
	}
	if err := json.Unmarshal(te.stdout.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, te.stdout)
	}
	if len(out.Tools) != 2 {
		t.Fatalf("tools: %+v", out.Tools)
	}
	c, x := out.Tools[0], out.Tools[1]
	if c.Tool != "claude" || !c.Detected || c.Version != "2.1.308" || c.HookInstall != "auto" ||
		c.HookState != "not_installed" || c.Foreign != 1 || c.EventsTotal != 9 {
		t.Errorf("claude: %+v", c)
	}
	if x.Tool != "codex" || x.Detected || x.Support != "unverified" || x.HookInstall != "auto" || x.HookState != "not_installed" {
		t.Errorf("codex: %+v", x)
	}
	if strings.Contains(te.stdout.String(), te.home) || strings.Contains(te.stdout.String(), "my-notifier") {
		t.Errorf("JSON에 경로·다른 Hook 명령 노출:\n%s", te.stdout)
	}

	if err := os.WriteFile(settings, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := te.run("", "hooks", "status", "--json", "--tool", "claude"); code != ExitError {
		t.Fatalf("unreadable exit %d", code)
	}
	if !strings.Contains(te.stdout.String(), `"parse_error"`) {
		t.Errorf("unreadable JSON:\n%s", te.stdout)
	}
	if code := te.run("", "hooks", "install", "--json"); code != ExitUsage {
		t.Errorf("--json on install: %d", code)
	}
}

const userCodexHooks = `{
  "description": "my hooks",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "my-notifier --done", "timeout": 30}]}]
  }
}
`

// Codex 사용자 범위 $CODEX_HOME/hooks.json(#170): Claude와 같은 미리보기·백업·소유 항목만 제거.
func TestHooksCodexInstallStatusUninstall(t *testing.T) {
	te := newTestEnv(t, "u s")
	te.env.ToolVersion = func(tool string) (string, error) {
		if tool == "codex" {
			return "0.163.0", nil
		}
		return "", errors.New("not found")
	}
	te.register("https://office.example")
	path := hooks.CodexHooksPath(te.dirs.CodexHome)
	if err := os.MkdirAll(te.dirs.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(userCodexHooks), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := te.run("", "hooks", "install", "--tool", "codex"); code != ExitOK {
		t.Fatalf("preview exit %d: %s", code, te.stderr)
	}
	out := te.stdout.String()
	if !strings.Contains(out, "+ SessionStart") || !strings.Contains(out, "+ PostToolUse") || !strings.Contains(out, "--yes") ||
		!strings.Contains(out, "hook codex") || !strings.Contains(out, "SessionEnd는 timeout 3") || !strings.Contains(out, "~/.codex/hooks.json") {
		t.Errorf("미리보기 출력:\n%s", out)
	}
	if b, _ := os.ReadFile(path); string(b) != userCodexHooks {
		t.Fatal("--yes 없이 파일이 바뀜")
	}

	if code := te.run("", "hooks", "install", "--tool", "codex", "--yes"); code != ExitOK {
		t.Fatalf("install exit %d: %s", code, te.stderr)
	}
	out = te.stdout.String()
	if !strings.Contains(out, "백업") || !strings.Contains(out, "Hooks need review") || strings.Contains(out, te.home) {
		t.Errorf("설치 출력(신뢰 안내, 홈 경로는 ~):\n%s", out)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("원래 권한 유지: %v %v", fi.Mode(), err)
	}
	var doc struct {
		Description string                       `json:"description"`
		Hooks       map[string][]json.RawMessage `json:"hooks"`
	}
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &doc); err != nil || doc.Description != "my hooks" || len(doc.Hooks) != 9 || len(doc.Hooks["Stop"]) != 2 {
		t.Fatalf("설치 결과: %v\n%s", err, b)
	}
	// Codex는 최상위에 description·hooks 외 키를 거부한다. 표시 키를 넣지 않아야 한다.
	var top map[string]json.RawMessage
	_ = json.Unmarshal(b, &top)
	if len(top) != 2 {
		t.Errorf("최상위 키: %v", top)
	}
	if !strings.Contains(string(doc.Hooks["SessionEnd"][0]), `"timeout": 3`) {
		t.Errorf("SessionEnd timeout: %s", doc.Hooks["SessionEnd"][0])
	}
	if cfg, _ := config.Load(te.dirs.ConfigFile()); cfg.ClaudeVersion != "" {
		t.Errorf("Codex 설치가 claude_version을 바꿈: %q", cfg.ClaudeVersion)
	}

	if code := te.run("", "hooks", "status", "--tool", "codex"); code != ExitOK {
		t.Fatalf("status exit %d", code)
	}
	if s := te.stdout.String(); !strings.Contains(s, "Codex Hook: installed") || !strings.Contains(s, "다른 Hook 1개") || !strings.Contains(s, "/hooks") {
		t.Errorf("status:\n%s", s)
	}
	if code := te.run("", "hooks", "status", "--json", "--tool", "codex"); code != ExitOK || !strings.Contains(te.stdout.String(), `"hook_state": "installed"`) {
		t.Errorf("status --json: %d\n%s", code, te.stdout)
	}
	if code := te.run("", "hooks", "install", "--tool", "codex", "--yes"); code != ExitOK || !strings.Contains(te.stdout.String(), "변경 없음") {
		t.Fatalf("두 번째 설치: %d\n%s", code, te.stdout)
	}
	// Claude 설정은 건드리지 않는다.
	if _, err := os.Stat(hooks.ClaudeSettingsPath(te.dirs.ClaudeProjects)); !os.IsNotExist(err) {
		t.Error("codex 설치가 Claude 설정을 만듦")
	}

	if code := te.run("", "hooks", "uninstall", "--tool", "codex", "--yes"); code != ExitOK {
		t.Fatalf("uninstall exit %d: %s", code, te.stderr)
	}
	var got, want any
	b, _ = os.ReadFile(path)
	_ = json.Unmarshal(b, &got)
	_ = json.Unmarshal([]byte(userCodexHooks), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("제거 후 원본과 다름:\n%s", b)
	}
	backups, _ := filepath.Glob(path + hooks.BackupInfix + "*")
	if len(backups) == 0 {
		t.Fatal("백업 없음")
	}
	for _, bk := range backups {
		if fi, err := os.Stat(bk); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("백업 권한: %v %v", fi.Mode(), err)
		}
	}
}

func TestHooksCodexRefusesSymlinkAndBroken(t *testing.T) {
	te := newTestEnv(t, "u")
	te.env.ToolVersion = func(string) (string, error) { return "", errors.New("none") }
	if err := os.MkdirAll(te.dirs.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	path := hooks.CodexHooksPath(te.dirs.CodexHome)
	target := filepath.Join(te.home, "elsewhere.json")
	if err := os.WriteFile(target, []byte(userCodexHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip("symlink unsupported:", err)
	}
	if code := te.run("", "hooks", "install", "--tool", "codex", "--yes"); code != ExitError {
		t.Errorf("심볼릭 링크 install: %d", code)
	}
	if b, _ := os.ReadFile(target); string(b) != userCodexHooks {
		t.Error("링크 대상이 바뀜")
	}
	_ = os.Remove(path)
	broken := `{"hooks": {"Stop": [}` + "\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := te.run("", "hooks", "install", "--tool", "codex", "--yes"); code != ExitError {
		t.Errorf("깨진 hooks.json install: %d", code)
	}
	if code := te.run("", "hooks", "uninstall", "--tool", "codex", "--yes"); code != ExitError {
		t.Errorf("깨진 hooks.json uninstall: %d", code)
	}
	if b, _ := os.ReadFile(path); string(b) != broken {
		t.Error("깨진 파일이 바뀜")
	}
	if m, _ := filepath.Glob(path + hooks.BackupInfix + "*"); len(m) != 0 {
		t.Error("거부했는데 백업이 생김")
	}
}
