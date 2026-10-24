package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/secret"
	"github.com/apg0001/Agent-Office/collector/core/internal/transport"
)

func configHasToken(t *testing.T, te *testEnv) bool {
	t.Helper()
	b, err := os.ReadFile(te.dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(b), sentinelToken)
}

func (te *testEnv) statusJSON() Report {
	te.t.Helper()
	if code := te.run("", "status", "--json"); code != ExitOK {
		te.t.Fatalf("status: %d", code)
	}
	var r Report
	if err := json.Unmarshal(te.stdout.Bytes(), &r); err != nil {
		te.t.Fatal(err)
	}
	return r
}

func TestRegisterStoresTokenInKeyring(t *testing.T) {
	srv := httptest.NewServer(&fakeOffice{})
	defer srv.Close()
	te := newTestEnv(t, "k")
	if code := te.run("", "register", "--server", srv.URL, "--code", "ABCD-EFGH", "--name", "E2E PC"); code != ExitOK {
		t.Fatalf("register: %d %s", code, te.stderr)
	}
	out := te.stdout.String()
	if strings.Contains(out, sentinelToken) || !strings.Contains(out, "OS 보안 저장소에만") {
		t.Fatalf("output: %s", out)
	}
	if configHasToken(t, te) {
		t.Fatal("token written to config.json despite keyring")
	}
	if v, err := te.secrets.Get(testDeviceID); err != nil || v != sentinelToken {
		t.Fatalf("keyring item: %v", err)
	}
	if r := te.statusJSON(); r.TokenStorage != "os_keyring" || r.Registration != "registered" {
		t.Fatalf("status: %+v", r)
	}
	if strings.Contains(te.stdout.String(), sentinelToken) {
		t.Fatal("status printed token")
	}
	// 사람이 읽는 status도 보관 위치를 알려 준다.
	te.run("", "status")
	if !strings.Contains(te.stdout.String(), "장치 토큰 보관: OS 보안 저장소") {
		t.Fatalf("status text: %s", te.stdout)
	}
}

func TestRegisterFallsBackToFileWithoutKeyring(t *testing.T) {
	srv := httptest.NewServer(&fakeOffice{})
	defer srv.Close()
	te := newTestEnv(t, "f")
	te.env.Secrets = &secret.Memory{Unavailable: true}
	if code := te.run("", "register", "--server", srv.URL, "--code", "ABCD-EFGH", "--name", "E2E PC"); code != ExitOK {
		t.Fatalf("register: %d %s", code, te.stderr)
	}
	if !strings.Contains(te.stdout.String(), "대체 경로") || strings.Contains(te.stdout.String(), sentinelToken) {
		t.Fatalf("output: %s", te.stdout)
	}
	if !configHasToken(t, te) {
		t.Fatal("fallback must keep the token in the 0600 file")
	}
	if r := te.statusJSON(); r.TokenStorage != "file_fallback" {
		t.Fatalf("status: %+v", r)
	}
}

func TestForcedFileModeFromEnvironment(t *testing.T) {
	env := Env{Getenv: func(k string) string {
		if k == secret.EnvStore {
			return "file"
		}
		return ""
	}}
	env.defaults()
	if env.Secrets != nil {
		t.Fatal("forced file mode must not use the OS secret store")
	}
}

func TestRegisterForceDeletesPreviousKeyringItem(t *testing.T) {
	srv := httptest.NewServer(&fakeOffice{})
	defer srv.Close()
	te := newTestEnv(t, "r")
	const oldID = "99999999-2222-4333-8444-555555555555"
	if _, err := config.SaveWithToken(te.dirs.ConfigFile(), config.Config{
		ServerURL: srv.URL, DeviceID: oldID, DeviceName: "PC", RegisteredAt: time.Now().UTC(),
	}, "aod_old_token_value", te.secrets); err != nil {
		t.Fatal(err)
	}
	if code := te.run("", "register", "--server", srv.URL, "--code", "ABCD-EFGH", "--name", "E2E PC", "--force"); code != ExitOK {
		t.Fatalf("register --force: %d %s", code, te.stderr)
	}
	if te.secrets.Has(oldID) || !te.secrets.Has(testDeviceID) {
		t.Fatal("old keyring item must be removed and new one stored")
	}
}

type revokeServer struct {
	mu     sync.Mutex
	status int
	auth   []string
}

func (s *revokeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path != "/api/v1/devices/revoke" || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	w.WriteHeader(s.status)
}

func (s *revokeServer) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

func TestLogout(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		closed    bool
		args      []string
		inFile    bool
		failGet   bool
		wantExit  int
		wantCalls int
		removed   bool
	}{
		{name: "revoked", status: http.StatusNoContent, wantExit: ExitOK, wantCalls: 1, removed: true},
		{name: "file fallback token", status: http.StatusNoContent, inFile: true, wantExit: ExitOK, wantCalls: 1, removed: true},
		{name: "already revoked", status: http.StatusUnauthorized, wantExit: ExitOK, wantCalls: 1, removed: true},
		{name: "server error keeps local", status: http.StatusInternalServerError, wantExit: ExitError, wantCalls: 1},
		{name: "network error keeps local", closed: true, wantExit: ExitError},
		{name: "local only", status: http.StatusNoContent, args: []string{"--local-only"}, wantExit: ExitOK, removed: true},
		{name: "keyring locked", status: http.StatusNoContent, failGet: true, wantExit: ExitError},
		{name: "keyring locked local only", status: http.StatusNoContent, failGet: true, args: []string{"--local-only"}, wantExit: ExitOK, removed: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rs := &revokeServer{status: c.status}
			srv := httptest.NewServer(rs)
			defer srv.Close()
			te := newTestEnv(t, "l")
			var store secret.Store = te.secrets
			if c.inFile {
				store = nil
			}
			if _, err := config.SaveWithToken(te.dirs.ConfigFile(), config.Config{
				ServerURL: srv.URL, DeviceID: testDeviceID, DeviceName: "PC", RegisteredAt: time.Now().UTC(),
			}, sentinelToken, store); err != nil {
				t.Fatal(err)
			}
			te.secrets.FailGet = c.failGet
			if c.closed {
				srv.Close()
			}
			code := te.run("", append([]string{"logout"}, c.args...)...)
			if code != c.wantExit {
				t.Fatalf("exit %d, stderr %s", code, te.stderr)
			}
			if out := te.stdout.String() + te.stderr.String(); strings.Contains(out, sentinelToken) || strings.Contains(out, srv.URL) {
				t.Fatalf("logout output leaks token or url: %s", out)
			}
			calls := rs.calls()
			if len(calls) != c.wantCalls {
				t.Fatalf("revoke calls %d", len(calls))
			}
			for _, a := range calls {
				if a != "Bearer "+sentinelToken {
					t.Fatal("revoke without device token")
				}
			}
			_, err := os.Stat(te.dirs.ConfigFile())
			if gone := os.IsNotExist(err); gone != c.removed {
				t.Fatalf("config removed=%v want %v", gone, c.removed)
			}
			if !c.inFile && te.secrets.Has(testDeviceID) == c.removed {
				t.Fatalf("keyring item present=%v after logout (removed=%v)", te.secrets.Has(testDeviceID), c.removed)
			}
			if c.removed {
				if r := te.statusJSON(); r.Registration != "not_registered" || r.TokenStorage != "" {
					t.Fatalf("status after logout: %+v", r)
				}
			}
		})
	}
}

func TestLogoutNotRegisteredAndCorrupt(t *testing.T) {
	te := newTestEnv(t, "n")
	if code := te.run("", "logout"); code != ExitOK || !strings.Contains(te.stdout.String(), "등록되어 있지 않습니다") {
		t.Fatalf("not registered: %d %s", code, te.stdout)
	}
	if err := os.MkdirAll(te.dirs.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(te.dirs.ConfigFile(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := te.run("", "logout"); code != ExitError {
		t.Fatalf("corrupt without --local-only: %d", code)
	}
	if code := te.run("", "logout", "--local-only"); code != ExitOK {
		t.Fatalf("corrupt --local-only: %d", code)
	}
	if _, err := os.Stat(te.dirs.ConfigFile()); !os.IsNotExist(err) {
		t.Fatal("corrupt config not removed")
	}
	if code := te.run("", "logout", "extra"); code != ExitUsage {
		t.Fatalf("usage: %d", code)
	}
}

func fastRun(te *testEnv, ctx context.Context) Env {
	e := te.env
	e.Context = ctx
	e.Stdout, e.Stderr = io.Discard, io.Discard
	e.RunOptions = func(o *runner.Options) {
		o.PollInterval = 30 * time.Millisecond
		o.DiscoverInterval = 30 * time.Millisecond
		o.HeartbeatInterval = 60 * time.Millisecond
		o.StatusInterval = 30 * time.Millisecond
		o.TuneSender = func(c *transport.Config) {
			c.IdleInterval = 30 * time.Millisecond
			c.BackoffBase = 10 * time.Millisecond
			c.BackoffMax = 50 * time.Millisecond
		}
	}
	return e
}

// #41 이전 형식(config.json에 토큰)으로 등록된 Collector가 run 시작 때 토큰을 보안 저장소로 옮기고
// 계속 전송하며, logout 뒤에는 몇 주기 안에 전송·heartbeat를 멈추는지 확인한다.
func TestRunMigratesLegacyTokenAndStopsAfterLogout(t *testing.T) {
	office := &fakeOffice{}
	srv := httptest.NewServer(office)
	defer srv.Close()
	te := newTestEnv(t, "m")
	te.register(srv.URL) // 파일에 토큰(legacy)
	if !configHasToken(t, te) {
		t.Fatal("precondition: legacy file token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Main([]string{"run"}, fastRun(te, ctx)) }()

	waitFor(t, "heartbeat after migration", func() bool { _, hb, _ := office.snapshot(); return hb > 0 })
	if configHasToken(t, te) {
		t.Fatal("token still in config.json after run started")
	}
	if v, err := te.secrets.Get(testDeviceID); err != nil || v != sentinelToken {
		t.Fatal("token not migrated to keyring")
	}
	cfg, err := config.Load(te.dirs.ConfigFile())
	if err != nil || !cfg.InKeyring() {
		t.Fatalf("config after migration: %v", err)
	}

	if code := te.run("", "logout"); code != ExitOK {
		t.Fatalf("logout: %d %s", code, te.stderr)
	}
	office.mu.Lock()
	revoked := office.revoked
	office.mu.Unlock()
	if revoked != 1 {
		t.Fatalf("server revoke calls %d", revoked)
	}
	waitFor(t, "run notices logout", func() bool {
		s, _ := runner.ReadSnapshot(te.dirs.StatusFile())
		return s.State == runner.StateNotRegistered
	})
	_, before, _ := office.snapshot()
	time.Sleep(300 * time.Millisecond) // heartbeat 주기(60ms)의 5배
	if _, after, _ := office.snapshot(); after != before {
		t.Fatalf("heartbeats continued after logout: %d -> %d", before, after)
	}
	cancel()
	if code := <-done; code != ExitOK {
		t.Fatalf("run exit %d", code)
	}
	_, _, bodies := office.snapshot()
	if strings.Contains(bodies, sentinelToken) {
		t.Fatal("token in request body")
	}
}

// 보안 저장소가 잠겨 토큰을 읽을 수 없으면 token_unavailable로 기다리고, 풀리면 전송을 시작한다.
func TestRunWaitsForLockedKeyring(t *testing.T) {
	office := &fakeOffice{}
	srv := httptest.NewServer(office)
	defer srv.Close()
	te := newTestEnv(t, "w")
	if _, err := config.SaveWithToken(te.dirs.ConfigFile(), config.Config{
		ServerURL: srv.URL, DeviceID: testDeviceID, DeviceName: "PC", RegisteredAt: time.Now().UTC(),
	}, sentinelToken, te.secrets); err != nil {
		t.Fatal(err)
	}
	te.secrets.SetFailGet(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Main([]string{"run"}, fastRun(te, ctx)) }()
	waitFor(t, "token_unavailable state", func() bool {
		s, _ := runner.ReadSnapshot(te.dirs.StatusFile())
		return s.State == runner.StateTokenUnavailable
	})
	if _, hb, _ := office.snapshot(); hb != 0 {
		t.Fatal("sent without a token")
	}
	te.secrets.SetFailGet(false)
	waitFor(t, "heartbeat after unlock", func() bool { _, hb, _ := office.snapshot(); return hb > 0 })
	cancel()
	<-done
}

// 실제 OS 보안 저장소로 register → status → logout → legacy 이전을 확인한다
// (AGENT_OFFICE_TEST_REAL_KEYRING=1, 수동·컨테이너 절차. CI에서는 건너뜀).
func TestRealKeyringRegisterLogout(t *testing.T) {
	if os.Getenv("AGENT_OFFICE_TEST_REAL_KEYRING") != "1" {
		t.Skip("AGENT_OFFICE_TEST_REAL_KEYRING 미설정: 실제 보안 저장소 검증은 수동·컨테이너 절차에서만 실행한다")
	}
	st := secret.System(os.Getenv, runtime.GOOS)
	if st == nil {
		t.Fatal("system secret store unavailable")
	}
	office := &fakeOffice{}
	srv := httptest.NewServer(office)
	defer srv.Close()
	te := newTestEnv(t, "real")
	te.env.Secrets = st
	_ = st.Delete(testDeviceID)
	t.Cleanup(func() { _ = st.Delete(testDeviceID) })
	if code := te.run("", "register", "--server", srv.URL, "--code", "ABCD-EFGH", "--name", "E2E PC"); code != ExitOK {
		t.Fatalf("register: %d %s", code, te.stderr)
	}
	if configHasToken(t, te) {
		t.Fatal("token in config.json")
	}
	if v, err := st.Get(testDeviceID); err != nil || v != sentinelToken {
		t.Fatalf("real keyring get: %v", err)
	}
	if r := te.statusJSON(); r.TokenStorage != "os_keyring" {
		t.Fatalf("status: %+v", r)
	}
	if code := te.run("", "logout"); code != ExitOK {
		t.Fatalf("logout: %d %s", code, te.stderr)
	}
	if _, err := st.Get(testDeviceID); err != secret.ErrNotFound {
		t.Fatalf("item after logout: %v", err)
	}
	// legacy 파일 토큰을 run이 실제 저장소로 옮기는지.
	te.register(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Main([]string{"run"}, fastRun(te, ctx)) }()
	waitFor(t, "heartbeat", func() bool { _, hb, _ := office.snapshot(); return hb > 0 })
	if configHasToken(t, te) {
		t.Fatal("legacy token not migrated")
	}
	if v, err := st.Get(testDeviceID); err != nil || v != sentinelToken {
		t.Fatalf("migrated item: %v", err)
	}
	cancel()
	<-done
}
