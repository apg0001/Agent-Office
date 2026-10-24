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
	if code := te.run("", "hooks", "install", "--tool", "codex", "--yes"); code != ExitError {
		t.Errorf("codex install은 거부해야 함: %d", code)
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
	if x.Tool != "codex" || x.Detected || x.Support != "unverified" || x.HookInstall != "print_only" {
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
