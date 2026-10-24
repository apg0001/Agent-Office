package corecli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/fakecore"
)

func TestMain(m *testing.M) {
	fakecore.Main()
	os.Exit(m.Run())
}

func fake(t *testing.T, scenario string) (Runner, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls.jsonl")
	return Runner{Bin: os.Args[0], Env: fakecore.Env(log, scenario)}, log
}

func TestLocate(t *testing.T) {
	dir := t.TempDir()
	name := BinName(runtime.GOOS)
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	none := func(string) string { return "" }

	if got, err := Locate(func(k string) string {
		if k == EnvBin {
			return bin
		}
		return ""
	}, "", runtime.GOOS); err != nil || got != bin {
		t.Errorf("env: %q %v", got, err)
	}
	if _, err := Locate(func(string) string { return "relative/core" }, "", runtime.GOOS); !errors.Is(err, ErrCoreMissing) {
		t.Errorf("relative env path accepted: %v", err)
	}
	if _, err := Locate(func(string) string { return filepath.Join(dir, "missing") }, "", runtime.GOOS); !errors.Is(err, ErrCoreMissing) {
		t.Errorf("missing env path accepted: %v", err)
	}
	if got, err := Locate(none, filepath.Join(dir, "agent-office-collector-gui"), runtime.GOOS); err != nil || got != bin {
		t.Errorf("sibling: %q %v", got, err)
	}
	t.Setenv("PATH", dir)
	if got, err := Locate(none, "", runtime.GOOS); err != nil || got != bin {
		t.Errorf("PATH: %q %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Locate(none, filepath.Join(t.TempDir(), "gui"), runtime.GOOS); !errors.Is(err, ErrCoreMissing) {
		t.Errorf("nothing found: %v", err)
	}
	if BinName("windows") != "agent-office-collector.exe" || BinName("darwin") != "agent-office-collector" {
		t.Error("BinName")
	}
}

func TestRunExitAndErrors(t *testing.T) {
	r, _ := fake(t, "")
	res, err := r.Run(context.Background(), "", "unknown-command")
	if err != nil || res.Exit != 2 || !strings.Contains(res.Stderr, "usage") {
		t.Fatalf("exit 2: %+v %v", res, err)
	}
	if _, err := (Runner{}).Run(context.Background(), ""); !errors.Is(err, ErrCoreMissing) {
		t.Errorf("empty bin: %v", err)
	}
	if _, err := (Runner{Bin: filepath.Join(t.TempDir(), "nope")}).Run(context.Background(), ""); !errors.Is(err, ErrStart) {
		t.Errorf("missing bin: %v", err)
	}
	slow, _ := fake(t, "slow")
	slow.Timeout = 300 * time.Millisecond
	start := time.Now()
	if _, err := slow.Run(context.Background(), "", "pause"); !errors.Is(err, ErrTimeout) {
		t.Errorf("timeout: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("timeout did not stop the process promptly")
	}
}

func TestCappedOutput(t *testing.T) {
	c := &capped{max: 4}
	n, err := c.Write([]byte("abcdef"))
	if n != 6 || err != nil || c.buf.String() != "abcd" {
		t.Fatalf("capped: %d %v %q", n, err, c.buf.String())
	}
	_, _ = c.Write([]byte("zz"))
	if c.buf.String() != "abcd" {
		t.Fatal("capped grew past max")
	}
}

func TestStatusDiagnoseTools(t *testing.T) {
	r, _ := fake(t, "")
	ctx := context.Background()
	rep, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TokenStorage != "os_keyring" {
		t.Errorf("token storage: %q", rep.TokenStorage)
	}
	if rep.Registration != "registered" || rep.Run == nil || rep.Run.State != "running" || rep.Run.QueueDepth != 2 ||
		rep.Queue == nil || rep.Queue.DeadLetter != 1 || rep.Run.LastHeartbeatOKAt == nil || len(rep.Run.Errors) != 1 {
		t.Errorf("status: %+v", rep)
	}
	d, err := r.Diagnose(ctx, true)
	if err != nil || d.Server != "skipped" || d.GoVersion == "" {
		t.Errorf("diagnose offline: %+v %v", d, err)
	}
	if d, _ := r.Diagnose(ctx, false); d.Server != "reachable" {
		t.Errorf("diagnose: %+v", d.Server)
	}
	tools, err := r.Tools(ctx)
	if err != nil || len(tools.Tools) != 2 || !tools.Tools[0].Detected || tools.Tools[1].HookInstall != "print_only" {
		t.Errorf("tools: %+v %v", tools, err)
	}

	u, _ := fake(t, "unreadable")
	tools, err = u.Tools(ctx)
	if err != nil || tools.Tools[0].HookState != "unreadable" || tools.Tools[0].Error != "parse_error" {
		t.Errorf("unreadable tools (exit 1 but JSON): %+v %v", tools, err)
	}

	g, _ := fake(t, "garbage")
	if _, err := g.Status(ctx); !errors.Is(err, ErrMalformed) {
		t.Errorf("garbage status: %v", err)
	}
	if _, err := g.Tools(ctx); !errors.Is(err, ErrMalformed) {
		t.Errorf("garbage tools: %v", err)
	}
}

func TestParseReportSanitizes(t *testing.T) {
	in := `{"registration":"weird","service":"installed","server":"https://host.example/x",
	"run":{"state":"hacked","watch_root":"/home/me/.claude/projects","counters":{"ok_count":1,"/etc/passwd":2},
	"errors":[{"code":"network","count":1},{"code":"dial tcp 10.0.0.1:443: refused","count":1}]},
	"queue":{"state":"<script>"},"token_storage":"/home/me/keyring"}`
	r, err := ParseReport(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.TokenStorage != "unknown" {
		t.Errorf("token storage not sanitized: %q", r.TokenStorage)
	}
	if r.Registration != "unknown" || r.Server != "unknown" || r.Run.State != "unknown" || r.Run.WatchRoot != "unknown" || r.Queue.State != "unknown" {
		t.Errorf("enums not sanitized: %+v %+v", r, r.Run)
	}
	if len(r.Run.Errors) != 1 || r.Run.Errors[0].Code != "network" {
		t.Errorf("error codes: %+v", r.Run.Errors)
	}
	if _, ok := r.Run.Counters["/etc/passwd"]; ok || r.Run.Counters["ok_count"] != 1 {
		t.Errorf("counters: %+v", r.Run.Counters)
	}
	for _, bad := range []string{"", "[]", "{}", "null", "not json"} {
		if _, err := ParseReport(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("ParseReport(%q) = %v", bad, err)
		}
	}
}

func TestParseToolsSanitizes(t *testing.T) {
	in := `{"os":"linux","tools":[{"tool":"claude","detected":true,"version":"2.1.308 /home/me","support":"super","hook_install":"x","hook_state":"weird","error":"open /home/me: denied"}]}`
	tr, err := ParseTools(in)
	if err != nil {
		t.Fatal(err)
	}
	c := tr.Tools[0]
	if c.Detected || c.Version != "" || c.Support != "unknown" || c.HookInstall != "print_only" || c.HookState != "unknown" || c.Error != "unknown" {
		t.Errorf("not sanitized: %+v", c)
	}
	if _, err := ParseTools(`{"tools":[{"tool":"other"}]}`); !errors.Is(err, ErrMalformed) {
		t.Error("unknown tool accepted")
	}
	if _, err := ParseTools(`{"tools":[]}`); !errors.Is(err, ErrMalformed) {
		t.Error("empty tools accepted")
	}
}

func TestNormalizeServerURL(t *testing.T) {
	ok := map[string]string{
		"https://office.example":        "https://office.example",
		" https://office.example:8443/": "https://office.example:8443",
		"http://localhost:8080":         "http://localhost:8080",
		"http://127.0.0.1:5173":         "http://127.0.0.1:5173",
		"http://[::1]:8000":             "http://[::1]:8000",
	}
	for in, want := range ok {
		if got, err := NormalizeServerURL(in); err != nil || got != want {
			t.Errorf("%q → %q %v, want %q", in, got, err, want)
		}
	}
	bad := map[string]error{
		"http://office.example":          ErrInsecureURL,
		"http://192.168.0.2":             ErrInsecureURL,
		"ftp://office.example":           ErrInvalidURL,
		"office.example":                 ErrInvalidURL,
		"https://office.example/devices": ErrInvalidURL,
		"https://u:p@office.example":     ErrInvalidURL,
		"https://office.example/?a=1":    ErrInvalidURL,
		"https://office.example#x":       ErrInvalidURL,
		"":                               ErrInvalidURL,
	}
	for in, want := range bad {
		if _, err := NormalizeServerURL(in); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", in, err, want)
		}
	}
}

func TestNormalizeCodeAndName(t *testing.T) {
	for in, want := range map[string]string{"abcd-efgh": "ABCD-EFGH", " ABCD EFGH ": "ABCD-EFGH", "abcdefgh": "ABCD-EFGH", "2345-6789": "2345-6789"} {
		if got, err := NormalizeCode(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	// I·L·O·U·0·1은 서버 알파벳에 없다.
	for _, in := range []string{"", "ABCD", "ABCD-EFGHJ", "ABCD-EFG1", "ABCD-EFGO", "ABCD_EFGH", "ABCD-EFGH\n"} {
		if _, err := NormalizeCode(in); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("%q accepted", in)
		}
	}
	if n, err := NormalizeName("  내 PC  "); err != nil || n != "내 PC" {
		t.Errorf("name: %q %v", n, err)
	}
	for _, in := range []string{"", "   ", strings.Repeat("가", 81), "a\nb", "a\x00b"} {
		if _, err := NormalizeName(in); !errors.Is(err, ErrInvalidName) {
			t.Errorf("name %q accepted", in)
		}
	}
	if _, err := NormalizeName(strings.Repeat("가", 80)); err != nil {
		t.Error("80 runes rejected")
	}
}

// 등록 코드는 명령 인자가 아니라 표준 입력으로만 넘긴다(프로세스 목록 노출 방지).
func TestRegisterPassesCodeOnStdin(t *testing.T) {
	r, log := fake(t, "")
	res, err := r.Register(context.Background(), "https://office.example/", "abcd efgh", " 내 PC ", true)
	if err != nil || res.Exit != 0 {
		t.Fatalf("register: %+v %v", res, err)
	}
	calls := fakecore.ReadCalls(log)
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	want := []string{"register", "--server", "https://office.example", "--code-stdin", "--name", "내 PC", "--force"}
	if !slices.Equal(calls[0].Args, want) {
		t.Errorf("args %q, want %q", calls[0].Args, want)
	}
	if calls[0].Stdin != "ABCD-EFGH\n" || strings.Contains(strings.Join(calls[0].Args, " "), "ABCD") {
		t.Errorf("code not on stdin only: %+v", calls[0])
	}
	for _, c := range []struct{ server, code, name string }{
		{"http://office.example", "ABCD-EFGH", "PC"},
		{"https://office.example", "bad", "PC"},
		{"https://office.example", "ABCD-EFGH", ""},
	} {
		if _, err := r.Register(context.Background(), c.server, c.code, c.name, false); err == nil {
			t.Errorf("invalid input accepted: %+v", c)
		}
	}
	if n := len(fakecore.ReadCalls(log)); n != 1 {
		t.Errorf("invalid input reached core: %d calls", n)
	}
}

func TestLogoutArgs(t *testing.T) {
	r, log := fake(t, "")
	ctx := context.Background()
	for _, local := range []bool{false, true} {
		res, err := r.Logout(ctx, local)
		if err != nil || res.Exit != 0 {
			t.Fatalf("logout(%v): %+v %v", local, res, err)
		}
	}
	calls := fakecore.ReadCalls(log)
	if len(calls) != 2 || !slices.Equal(calls[0].Args, []string{"logout"}) || !slices.Equal(calls[1].Args, []string{"logout", "--local-only"}) {
		t.Fatalf("calls: %+v", calls)
	}
	if rs, _ := ParseReport(`{"registration":"registered","service":"absent","run":{"state":"token_unavailable"}}`); rs.Run.State != "token_unavailable" {
		t.Fatalf("token_unavailable state: %+v", rs.Run)
	}
}
