package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/secret"
	"github.com/apg0001/Agent-Office/collector/core/internal/service"
)

const (
	sentinelToken  = "aod_SENTINEL_TOKEN_e2e_DO_NOT_PRINT"
	sentinelPrompt = "SENTINEL_PROMPT_TEXT_DO_NOT_STORE"
	testDeviceID   = "0d6b2a10-7a4c-4b8e-9f00-1234567890ab"
)

// TestMain은 하위 프로세스 모드(AO_CLI_HELPER=1)에서 실제 CLI처럼 동작한다.
func TestMain(m *testing.M) {
	if os.Getenv("AO_CLI_HELPER") == "1" {
		args := strings.Fields(os.Getenv("AO_CLI_ARGS"))
		os.Exit(Main(args, Env{}))
	}
	os.Exit(m.Run())
}

type testEnv struct {
	t      *testing.T
	home   string
	dirs   paths.Dirs
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	env    Env
	// secrets는 OS 보안 저장소 대신 쓰는 메모리 저장소다(테스트가 실제 키체인을 건드리지 않게).
	secrets *secret.Memory
}

func newTestEnv(t *testing.T, homeName string) *testEnv {
	t.Helper()
	home := filepath.Join(t.TempDir(), homeName)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{
		paths.EnvHome:       filepath.Join(home, "ao"),
		"CLAUDE_CONFIG_DIR": filepath.Join(home, "claude"),
	}
	getenv := func(k string) string { return vars[k] }
	dirs, err := paths.Resolve(runtime.GOOS, getenv, home)
	if err != nil {
		t.Fatal(err)
	}
	te := &testEnv{t: t, home: home, dirs: dirs, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, secrets: secret.NewMemory()}
	te.env = Env{
		Version: "0.0.0-test", Stdin: strings.NewReader(""), Stdout: te.stdout, Stderr: te.stderr,
		Getenv: getenv, Dirs: func() (paths.Dirs, error) { return dirs, nil },
		Executable: func() (string, error) { return testExecutable(), nil },
		Command:    func(...string) ([]byte, error) { return nil, nil },
		Secrets:    te.secrets,
	}
	return te
}

// testExecutable은 이 OS에서 절대 경로인 가짜 설치 위치다(Hook 명령 생성은 절대 경로만 허용).
func testExecutable() string {
	if runtime.GOOS == "windows" {
		return `C:\Program Files\AgentOffice\agent-office-collector.exe`
	}
	return "/usr/local/bin/agent-office-collector"
}

func (te *testEnv) run(stdin string, args ...string) int {
	te.stdout.Reset()
	te.stderr.Reset()
	e := te.env
	e.Stdin = strings.NewReader(stdin)
	return Main(args, e)
}

func (te *testEnv) register(server string) {
	te.t.Helper()
	if err := config.Save(te.dirs.ConfigFile(), config.Config{
		SchemaVersion: config.SchemaVersion, ServerURL: server, DeviceID: testDeviceID,
		DeviceToken: sentinelToken, DeviceName: "PC", RegisteredAt: time.Now().UTC(),
	}); err != nil {
		te.t.Fatal(err)
	}
}

func spoolFiles(t *testing.T, dirs paths.Dirs) [][]byte {
	t.Helper()
	entries, _ := os.ReadDir(dirs.SpoolDir())
	var out [][]byte
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dirs.SpoolDir(), e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

const validHook = `{"hook_event_name":"PreToolUse","session_id":"00000000-0000-4000-8000-000000000001","prompt_id":"00000000-0000-4000-8000-000000000101","tool_name":"Read","tool_use_id":"toolu_x1","tool_input":{"file_path":"/home/someone/` + sentinelPrompt + `"},"cwd":"/home/someone/` + sentinelPrompt + `","prompt":"` + sentinelPrompt + `"}`

// blockingReader는 끝나지 않는 stdin이다.
type blockingReader struct{ ch chan struct{} }

func (b blockingReader) Read([]byte) (int, error) { <-b.ch; return 0, io.EOF }

func TestHookFailOpen(t *testing.T) {
	te := newTestEnv(t, "u")
	cases := []struct {
		name  string
		setup func()
		stdin string
		args  []string
	}{
		{"no provider", nil, validHook, []string{"hook"}},
		{"unknown provider", nil, validHook, []string{"hook", "gemini"}},
		{"bad flag", nil, validHook, []string{"hook", "claude", "--bogus"}},
		{"not registered", nil, validHook, []string{"hook", "claude"}},
		{"garbage", func() { te.register("https://office.example") }, "\x00not json", []string{"hook", "claude"}},
		{"empty", nil, "", []string{"hook", "claude"}},
		{"missing session", nil, `{"hook_event_name":"Stop"}`, []string{"hook", "codex"}},
		{"oversized", nil, `{"x":"` + strings.Repeat("a", 8<<20) + `"}`, []string{"hook", "claude"}},
		{"paused", func() { _ = config.SavePause(te.dirs.PauseFile(), true, time.Now()) }, validHook, []string{"hook", "claude"}},
	}
	for _, c := range cases {
		if c.setup != nil {
			c.setup()
		}
		start := time.Now()
		if code := te.run(c.stdin, c.args...); code != ExitOK {
			t.Fatalf("%s: exit %d", c.name, code)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("%s: too slow", c.name)
		}
		if te.stdout.Len() != 0 || te.stderr.Len() != 0 {
			t.Fatalf("%s: hook printed output", c.name)
		}
	}
	if n := len(spoolFiles(t, te.dirs)); n != 0 {
		t.Fatalf("spooled %d files for rejected input", n)
	}

	// stdin이 끝나지 않아도 HookTimeout 안에 끝난다.
	e := te.env
	e.HookTimeout = 100 * time.Millisecond
	block := blockingReader{ch: make(chan struct{})}
	defer close(block.ch)
	e.Stdin = block
	start := time.Now()
	if code := Main([]string{"hook", "claude"}, e); code != ExitOK || time.Since(start) > time.Second {
		t.Fatalf("blocking stdin: exit %d after %v", code, time.Since(start))
	}

	// spool 디렉터리를 쓸 수 없어도 0이다(상태 디렉터리 자리에 파일).
	_ = config.SavePause(te.dirs.PauseFile(), false, time.Now())
	if err := os.MkdirAll(te.dirs.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(te.dirs.SpoolDir(), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := te.run(validHook, "hook", "claude"); code != ExitOK || te.stdout.Len() != 0 {
		t.Fatalf("unwritable spool: exit %d", code)
	}
}

func TestHookSpoolsWhitelistedCandidateOnly(t *testing.T) {
	te := newTestEnv(t, "u")
	te.register("https://office.example")
	if code := te.run(validHook, "hook", "claude", "--tool-version", "2.1.308", "--mode", "print"); code != ExitOK {
		t.Fatal(code)
	}
	files := spoolFiles(t, te.dirs)
	if len(files) != 1 {
		t.Fatalf("spool files %d", len(files))
	}
	for _, bad := range []string{sentinelPrompt, "/home/someone", "tool_input", "Read\""} {
		if bytes.Contains(files[0], []byte(bad)) {
			t.Fatalf("spool contains %q", bad)
		}
	}
	if !bytes.Contains(files[0], []byte(`"event_type":"tool.started"`)) {
		t.Fatalf("unexpected spool record %s", files[0])
	}
}

// TodoWrite Hook은 단계 상태만 spool에 남기고 단계 문장은 버린다(#38).
func TestHookSpoolsPlanStatusesOnly(t *testing.T) {
	te := newTestEnv(t, "u")
	te.register("https://office.example")
	hook := `{"hook_event_name":"PreToolUse","session_id":"00000000-0000-4000-8000-000000000001","prompt_id":"00000000-0000-4000-8000-000000000101","tool_name":"TodoWrite","tool_use_id":"toolu_p1","tool_input":{"todos":[{"content":"` + sentinelPrompt + `","activeForm":"` + sentinelPrompt + `","status":"in_progress"},{"content":"` + sentinelPrompt + `","activeForm":"x","status":"pending"}]}}`
	if code := te.run(hook, "hook", "claude", "--tool-version", "2.1.308", "--mode", "print"); code != ExitOK {
		t.Fatal(code)
	}
	files := spoolFiles(t, te.dirs)
	if len(files) != 1 {
		t.Fatalf("spool files %d", len(files))
	}
	for _, bad := range []string{sentinelPrompt, "content", "activeForm", "todos"} {
		if bytes.Contains(files[0], []byte(bad)) {
			t.Fatalf("spool contains %q", bad)
		}
	}
	if !bytes.Contains(files[0], []byte(`"plan":{"steps":["in_progress","pending"]`)) {
		t.Fatalf("plan statuses missing: %s", files[0])
	}
}

// 실제 프로세스로 실행해 종료 코드와 시간을 확인한다(Claude Code가 보는 것과 같은 조건).
func TestHookProcessExitsZeroQuickly(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name  string
		stdin io.Reader
		args  string
		limit time.Duration
	}{
		{"garbage stdin, not registered", strings.NewReader("garbage{"), "hook claude", 5 * time.Second},
		{"no args", strings.NewReader(""), "hook", 5 * time.Second},
	}
	for _, c := range cases {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "AO_CLI_HELPER=1", "AO_CLI_ARGS="+c.args, paths.EnvHome+"="+home)
		cmd.Stdin = c.stdin
		var out bytes.Buffer
		cmd.Stdout = &out
		start := time.Now()
		err := cmd.Run()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if d := time.Since(start); d > c.limit {
			t.Fatalf("%s: took %v", c.name, d)
		}
		if out.Len() != 0 {
			t.Fatalf("%s: stdout not empty", c.name)
		}
	}
	// stdin을 닫지 않는 호출자: HookTimeout(2초) 뒤 0으로 끝나야 한다.
	// *os.File을 넘겨 자식이 fd를 직접 받게 한다(io.Pipe면 exec가 복사 고루틴을 기다려 테스트가 멈춘다).
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "AO_CLI_HELPER=1", "AO_CLI_ARGS=hook claude", paths.EnvHome+"="+home)
	cmd.Stdin = pr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("open stdin: %v", err)
	}
	if d := time.Since(start); d > 6*time.Second {
		t.Fatalf("open stdin took %v", d)
	}
}

func TestPauseResumeCommands(t *testing.T) {
	te := newTestEnv(t, "u")
	if code := te.run("", "pause"); code != ExitOK {
		t.Fatal(code)
	}
	p, _ := config.LoadPause(te.dirs.PauseFile())
	if !p.Paused {
		t.Fatal("not paused")
	}
	if code := te.run("", "resume"); code != ExitOK {
		t.Fatal(code)
	}
	p, _ = config.LoadPause(te.dirs.PauseFile())
	if p.Paused || !p.SkipBacklog {
		t.Fatalf("resume state %+v", p)
	}
	if code := te.run("", "pause", "extra"); code != ExitUsage {
		t.Fatalf("usage %d", code)
	}
}

func TestDiagnoseHasNoSecretsOrPaths(t *testing.T) {
	te := newTestEnv(t, "SENTINEL_HOME_DIR")
	if err := config.Save(te.dirs.ConfigFile(), config.Config{
		SchemaVersion: config.SchemaVersion, ServerURL: "https://sentinel-host.example", DeviceID: testDeviceID,
		DeviceToken: sentinelToken, DeviceName: "PC", RegisteredAt: time.Now().UTC(),
		ClaudeProjectsDir: filepath.Join(te.home, "SENTINEL_PROJECTS"),
	}); err != nil {
		t.Fatal(err)
	}
	snap := runner.Snapshot{SchemaVersion: runner.SnapshotVersion, State: runner.StateRunning, Counters: map[string]int64{"acked": 3},
		Errors: []runner.ErrorStat{{Code: runner.ErrCodeHTTP5xx, Count: 2}}}
	data, _ := json.Marshal(snap)
	if err := os.MkdirAll(te.dirs.State, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(te.dirs.StatusFile(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"diagnose", "--offline"}, {"diagnose", "--json", "--offline"}, {"status"}, {"status", "--json"}} {
		if code := te.run("", args...); code != ExitOK {
			t.Fatalf("%v: exit %d %s", args, code, te.stderr)
		}
		out := te.stdout.String() + te.stderr.String()
		for _, bad := range []string{"SENTINEL", sentinelToken, te.home, "sentinel-host", testDeviceID} {
			if strings.Contains(out, bad) {
				t.Fatalf("%v output contains %q:\n%s", args, bad, out)
			}
		}
		if !strings.Contains(out, "http_5xx") {
			t.Fatalf("%v: error code missing:\n%s", args, out)
		}
	}
	te.run("", "diagnose", "--json", "--offline")
	var rep Report
	if err := json.Unmarshal(te.stdout.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Registration != "registered" || rep.ServerScheme != "https" || rep.Server != "skipped" || rep.GoVersion == "" || rep.Run == nil {
		t.Fatalf("report %+v", rep)
	}
}

func TestServiceCommands(t *testing.T) {
	te := newTestEnv(t, "u")
	te.env.GOOS = "linux"
	var calls [][]string
	te.env.Command = func(args ...string) ([]byte, error) { calls = append(calls, args); return nil, nil }
	if code := te.run("", "service", "print", "--os", "windows", "--exe", `C:\ao\agent-office-collector.exe`); code != ExitOK {
		t.Fatalf("print: %d %s", code, te.stderr)
	}
	if !strings.Contains(te.stdout.String(), service.Marker) || !strings.Contains(te.stdout.String(), "LogonTrigger") {
		t.Fatal("windows print output")
	}
	if code := te.run("", "service", "install", "--os", "darwin"); code != ExitUsage {
		t.Fatalf("--os on install must be rejected: %d", code)
	}
	if code := te.run("", "service", "install"); code != ExitOK {
		t.Fatalf("install: %d %s", code, te.stderr)
	}
	unit := filepath.Join(te.home, ".config", "systemd", "user", service.UnitName)
	if b, err := os.ReadFile(unit); err != nil || !service.Owned(b) {
		t.Fatalf("unit not written: %v", err)
	}
	if strings.Contains(te.stdout.String(), te.home) || len(calls) != 0 {
		t.Fatalf("install without --activate must not run commands or print home: %s %v", te.stdout, calls)
	}
	if code := te.run("", "service", "install", "--activate"); code != ExitOK || len(calls) != 2 {
		t.Fatalf("activate: %d %v", code, calls)
	}
	if code := te.run("", "service", "uninstall"); code != ExitOK {
		t.Fatal(code)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatal("unit not removed")
	}
	if err := os.WriteFile(unit, []byte("[Service]\nExecStart=/bin/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := te.run("", "service", "install"); code != ExitError {
		t.Fatalf("foreign install: %d", code)
	}
	if code := te.run("", "service", "uninstall"); code != ExitError {
		t.Fatalf("foreign uninstall: %d", code)
	}
	if b, _ := os.ReadFile(unit); !strings.Contains(string(b), "/bin/other") {
		t.Fatal("foreign unit modified")
	}
}

func TestRunRefusesSecondInstance(t *testing.T) {
	te := newTestEnv(t, "u")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	te.env.Context = ctx
	te.env.RunOptions = func(o *runner.Options) { o.PollInterval = 20 * time.Millisecond }
	done := make(chan int, 1)
	e := te.env
	e.Stdout, e.Stderr = io.Discard, io.Discard
	go func() { done <- Main([]string{"run"}, e) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if snap, err := runner.ReadSnapshot(te.dirs.StatusFile()); err == nil && snap.State == runner.StateNotRegistered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not report not_registered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code := te.run("", "run"); code != ExitError || !strings.Contains(te.stderr.String(), "이미") {
		t.Fatalf("second run: %d %s", code, te.stderr)
	}
	cancel()
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("run exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
}
