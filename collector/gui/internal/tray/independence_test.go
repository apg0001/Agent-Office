//go:build !windows

package tray

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

// EnvRealCore는 실제 Core 실행 파일 경로다(app 통합 테스트와 같은 변수, CI `Go GUI` 작업이 넣음).
const EnvRealCore = "AGENT_OFFICE_TEST_CORE_BIN"

const (
	envHelper     = "AO_TRAY_GUI_HELPER"
	envHelperBin  = "AO_TRAY_GUI_HELPER_CORE"
	sentinelToken = "aod_SENTINEL_TOKEN_tray_DO_NOT_SHOW"
	deviceID      = "0d6b2a10-7a4c-4b8e-9f00-1234567890ab"
)

// TestMain의 도우미 모드는 "GUI 프로세스" 역할이다. 실제 Core를 corecli로 부르며 트레이 상태를
// 갱신하다가 부모가 SIGKILL로 죽이거나(crash) 트레이 "GUI 종료"로 스스로 끝난다(quit).
func TestMain(m *testing.M) {
	if mode := os.Getenv(envHelper); mode != "" {
		guiHelper(mode)
		return
	}
	os.Exit(m.Run())
}

func guiHelper(mode string) {
	a := app.New(app.Options{Runner: &corecli.Runner{Bin: os.Getenv(envHelperBin), Env: os.Environ()}})
	c := New(a, nil)
	c.QuitGUI = func() { os.Exit(0) }
	v := c.Refresh()
	fmt.Printf("ready %s\n", v.State)
	if mode == "quit" {
		c.Handle(ItemQuit)
	}
	for {
		time.Sleep(50 * time.Millisecond)
		c.Refresh()
	}
}

type office struct {
	mu         sync.Mutex
	events     int
	heartbeats int
	revoked    int
	bodies     strings.Builder
}

func (o *office) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.bodies.Write(body)
	if r.URL.Path != "/api/v1/devices/register" && r.URL.Path != "/api/v1/system/health" && r.Header.Get("Authorization") != "Bearer "+sentinelToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/api/v1/devices/register":
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"device_id":"` + deviceID + `","device_token":"` + sentinelToken + `"}`))
	case "/api/v1/system/health":
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	case "/api/v1/devices/heartbeat":
		o.heartbeats++
		w.WriteHeader(http.StatusNoContent)
	case "/api/v1/devices/revoke":
		o.revoked++
		w.WriteHeader(http.StatusNoContent)
	case "/api/v1/events/batch":
		var req struct {
			Events []struct {
				EventID string `json:"event_id"`
			} `json:"events"`
		}
		_ = json.Unmarshal(body, &req)
		ids := []string{}
		for _, e := range req.Events {
			ids = append(ids, e.EventID)
		}
		o.events += len(ids)
		resp, _ := json.Marshal(map[string]any{"schema_version": "1.0", "accepted_event_ids": ids, "duplicate_event_ids": []string{}, "rejected_events": []any{}})
		_, _ = w.Write(resp)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (o *office) counts() (events, heartbeats, revoked int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.events, o.heartbeats, o.revoked
}

func waitUntil(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// coreProc는 별도 프로세스로 띄운 Core `run`이다(OS 자동 시작 서비스 역할).
type coreProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (c coreProc) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// GUI/Core 독립성(#43):
//  1. 별도 프로세스로 실행 중인 Core는 GUI 프로세스가 비정상 종료(SIGKILL)하거나 트레이 "GUI 종료"로
//     끝나도 계속 실행·수집·전송한다.
//  2. GUI(트레이 Controller)는 Core가 죽으면 core_stopped를 보이고, Core가 다시 시작하면 running으로 회복한다.
//  3. 트레이 일시중지·재개와 GUI 로그아웃은 Core 명령으로만 동작하고, 로그아웃 뒤 Core는 전송을 멈춘다.
func TestRealCoreIndependence(t *testing.T) {
	bin := os.Getenv(EnvRealCore)
	if bin == "" {
		t.Skip(EnvRealCore + " 미설정: 실제 Core 독립성 테스트는 Core를 빌드한 CI·수동 검증에서 실행한다")
	}
	off := &office{}
	srv := httptest.NewServer(off)
	defer srv.Close()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	aoHome := filepath.Join(root, "ao")
	coreEnv := []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin",
		"CLAUDE_CONFIG_DIR=" + filepath.Join(root, "claude"),
		prefs.EnvHome + "=" + aoHome,
		// 실제 사용자 키체인을 건드리지 않는다(보안 저장소 경로는 Core 단위 테스트·컨테이너 절차에서 확인).
		"AGENT_OFFICE_COLLECTOR_TOKEN_STORE=file",
	}
	a := app.New(app.Options{
		Getenv: func(k string) string {
			if k == prefs.EnvHome {
				return aoHome
			}
			return ""
		},
		Runner: &corecli.Runner{Bin: bin, Env: coreEnv},
	})
	ctl := New(a, nil)

	if r := a.SaveServer(srv.URL); !r.OK {
		t.Fatalf("save server: %+v", r)
	}
	if r := a.Register("ABCD-EFGH", "독립성 PC", false); !r.OK {
		t.Fatalf("register: %+v", r)
	}

	startCore := func() coreProc {
		cmd := exec.Command(bin, "run")
		cmd.Env = coreEnv
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		p := coreProc{cmd: cmd, done: make(chan struct{})}
		go func() { _ = cmd.Wait(); close(p.done) }()
		return p
	}
	core := startCore()
	t.Cleanup(func() {
		_ = core.cmd.Process.Kill()
		<-core.done
	})
	waitUntil(t, "core running + first heartbeat", 20*time.Second, func() bool {
		_, hb, _ := off.counts()
		return hb > 0 && ctl.Refresh().State == StateRunning
	})

	sendHook := func(session string) {
		t.Helper()
		cmd := exec.Command(bin, "hook", "claude")
		cmd.Env = coreEnv
		cmd.Stdin = strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"` + session + `","source":"startup"}`)
		if out, err := cmd.CombinedOutput(); err != nil || len(out) != 0 {
			t.Fatalf("hook: %v %q", err, out)
		}
	}

	for i, mode := range []string{"crash", "quit"} {
		gui := exec.Command(os.Args[0], "-test.run=^$")
		gui.Env = append(append([]string{}, coreEnv...), envHelper+"="+mode, envHelperBin+"="+bin)
		stdout, _ := gui.StdoutPipe()
		gui.Stderr = io.Discard
		if err := gui.Start(); err != nil {
			t.Fatal(err)
		}
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		if strings.TrimSpace(line) != "ready running" {
			_ = gui.Process.Kill()
			t.Fatalf("%s: gui helper saw %q", mode, line)
		}
		if mode == "crash" {
			_ = gui.Process.Signal(syscall.SIGKILL)
		}
		err := gui.Wait()
		if mode == "quit" && err != nil {
			t.Fatalf("quit from tray: %v", err)
		}
		if mode == "crash" && err == nil {
			t.Fatal("gui helper was not killed")
		}

		if !core.alive() {
			t.Fatalf("%s: core died with the GUI", mode)
		}
		before, _, _ := off.counts()
		sendHook(fmt.Sprintf("00000000-0000-4000-8000-00000000010%d", i))
		waitUntil(t, mode+": event sent after GUI exit", 20*time.Second, func() bool {
			ev, _, _ := off.counts()
			return ev > before
		})
	}

	// Core 재시작 → GUI 회복.
	_ = core.cmd.Process.Signal(syscall.SIGKILL)
	<-core.done
	waitUntil(t, "tray shows core stopped", 10*time.Second, func() bool { return ctl.Refresh().State == StateCoreStopped })
	core = startCore()
	waitUntil(t, "tray recovers after core restart", 20*time.Second, func() bool { return ctl.Refresh().State == StateRunning })

	ctl.Handle(ItemPause)
	if v := ctl.Last(); v.State != StatePaused || !v.CanResume {
		t.Fatalf("tray pause: %+v", v)
	}
	ctl.Handle(ItemResume)
	if v := ctl.Last(); v.State != StateRunning {
		t.Fatalf("tray resume: %+v", v)
	}

	// GUI 로그아웃: 서버 폐기 → 로컬 토큰 삭제 → Core는 이후 이벤트를 보내지 않는다.
	if r := a.Logout(false, true); !r.OK {
		t.Fatalf("logout: %+v", r)
	}
	if _, _, rv := off.counts(); rv != 1 {
		t.Fatalf("server revoke calls %d", rv)
	}
	if v := ctl.Refresh(); v.State != StateNotRegistered {
		t.Fatalf("after logout: %+v", v)
	}
	before, _, _ := off.counts()
	sendHook("00000000-0000-4000-8000-000000000199")
	time.Sleep(3 * time.Second) // Core 수집 주기(2초)보다 길게
	if ev, _, _ := off.counts(); ev != before {
		t.Fatalf("events sent after logout: %d -> %d", before, ev)
	}
	if !core.alive() {
		t.Fatal("core exited on logout (should keep waiting for a new registration)")
	}

	off.mu.Lock()
	bodies := off.bodies.String()
	off.mu.Unlock()
	if strings.Contains(bodies, sentinelToken) {
		t.Fatal("token in request body")
	}
	for _, v := range []any{ctl.Last(), a.Status(), a.Settings()} {
		if b, _ := json.Marshal(v); strings.Contains(string(b), sentinelToken) || strings.Contains(string(b), home) {
			t.Fatalf("UI value leaks token or home: %s", b)
		}
	}
}
