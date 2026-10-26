package tray

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
)

type fakeActions struct {
	mu     sync.Mutex
	status app.StatusView
	office string
	calls  []string
}

func (f *fakeActions) Status() app.StatusView {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "status")
	return f.status
}

func (f *fakeActions) Pause() app.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "pause")
	f.status.Report.Paused = true
	return app.Result{OK: true}
}

func (f *fakeActions) Resume() app.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "resume")
	f.status.Report.Paused = false
	return app.Result{OK: true}
}

func (f *fakeActions) UpdateCheck() app.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "update")
	return app.Result{OK: true, Code: "update_available", Output: "새 버전 0.2.0이 있습니다(현재 0.1.0, stable 채널)"}
}

func (f *fakeActions) Logout(localOnly, confirmed bool) app.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	if localOnly || !confirmed {
		f.calls = append(f.calls, "logout-bad-args")
		return app.Result{Code: "confirmation_required"}
	}
	f.calls = append(f.calls, "logout")
	f.status.Report.Registration = "not_registered"
	return app.Result{OK: true, Code: "logged_out"}
}

func (f *fakeActions) OfficeURL() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "office")
	if f.office == "" {
		return "", errors.New("server_not_set")
	}
	return f.office, nil
}

func (f *fakeActions) set(v app.StatusView) {
	f.mu.Lock()
	f.status = v
	f.mu.Unlock()
}

type fakeHost struct {
	mu    sync.Mutex
	views []View
}

func (h *fakeHost) Apply(v View) {
	h.mu.Lock()
	h.views = append(h.views, v)
	h.mu.Unlock()
}

func (h *fakeHost) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.views)
}

func report(reg string, paused, running bool, state string) app.StatusView {
	r := &corecli.Report{Registration: reg, Paused: paused, ProcessRunning: running, Service: "installed"}
	if state != "" {
		r.Run = &corecli.RunSnapshot{State: state}
	}
	return app.StatusView{OK: true, Report: r}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		in        app.StatusView
		state     State
		canPause  bool
		canResume bool
	}{
		{report("registered", false, true, "running"), StateRunning, true, false},
		{report("registered", true, true, "paused"), StatePaused, false, true},
		{report("registered", false, false, "stopped"), StateCoreStopped, true, false},
		{report("registered", false, true, "device_revoked"), StateError, true, false},
		{report("registered", false, true, "token_unavailable"), StateError, true, false},
		{report("not_registered", false, true, "not_registered"), StateNotRegistered, true, false},
		{report("config_invalid", false, true, ""), StateError, true, false},
		{app.StatusView{Code: "core_missing"}, StateError, false, false},
		{app.StatusView{Code: "/etc/passwd"}, StateError, false, false},
	}
	for i, c := range cases {
		v := Describe(c.in)
		if v.State != c.state || v.CanPause != c.canPause || v.CanResume != c.canResume || v.Label == "" || v.Tooltip == "" {
			t.Errorf("%d: %+v", i, v)
		}
		if strings.Contains(v.Tooltip, "/etc") {
			t.Errorf("%d: unsafe code in tooltip: %q", i, v.Tooltip)
		}
	}
}

func TestControllerMenu(t *testing.T) {
	acts := &fakeActions{status: report("registered", false, true, "running")}
	host := &fakeHost{}
	c := New(acts, host)
	var launched [][]string
	var opened []string
	quit := 0
	c.LaunchGUI = func(args ...string) error { launched = append(launched, args); return nil }
	c.OpenURL = func(u string) error { opened = append(opened, u); return nil }
	c.QuitTray = func() { quit++ }

	if v := c.Refresh(); v.State != StateRunning || host.count() != 1 || v.Update != "업데이트 확인" || !v.CanLogout {
		t.Fatalf("refresh: %+v", v)
	}
	if c.Handle(ItemPause); c.Last().State != StatePaused || !c.Last().CanResume {
		t.Fatalf("after pause: %+v", c.Last())
	}
	c.Handle(ItemResume)
	if c.Last().State != StateRunning {
		t.Fatalf("after resume: %+v", c.Last())
	}
	// 서버 주소가 없으면 사무실 열기 = 설정 창.
	if code := c.Handle(ItemOffice); code != "settings_opened_no_server" || len(launched) != 1 || len(opened) != 0 {
		t.Fatalf("office without server: %q %v %v", code, launched, opened)
	}
	acts.mu.Lock()
	acts.office = "https://office.example/"
	acts.mu.Unlock()
	if code := c.Handle(ItemOffice); code != "office_opened" || !slices.Equal(opened, []string{"https://office.example/"}) {
		t.Fatalf("office: %q %v", code, opened)
	}
	if code := c.Handle(ItemSettings); code != "settings_opened" || len(launched) != 2 || len(launched[1]) != 0 {
		t.Fatalf("settings: %q %v", code, launched)
	}
	if code := c.Handle(ItemUpdate); code != "update_available" || !strings.Contains(c.Last().Update, "0.2.0") {
		t.Fatalf("update: %q %+v", code, c.Last())
	}
	if code := c.Handle(ItemLogout); code != "logged_out" || c.Last().State != StateNotRegistered || c.Last().CanLogout {
		t.Fatalf("logout: %q %+v", code, c.Last())
	}
	if code := c.Handle(ItemQuit); code != "quit" || quit != 1 || !slices.Equal(launched[2], []string{"--quit"}) {
		t.Fatalf("quit: %q %d %v", code, quit, launched)
	}
	for _, call := range acts.calls {
		if !slices.Contains([]string{"status", "pause", "resume", "office", "update", "logout"}, call) {
			t.Fatalf("unexpected core action %q", call)
		}
	}
	// GUI·브라우저를 열 수 없을 때.
	c.LaunchGUI = func(...string) error { return errors.New("missing") }
	c.OpenURL = func(string) error { return errors.New("no browser") }
	if c.Handle(ItemSettings) != "gui_unavailable" || c.Handle(ItemOffice) != "browser_unavailable" || c.Handle("x") != "unexpected" {
		t.Fatal("failure codes")
	}
	if len(Items) != 8 { // #180: 업데이트 설치
		t.Fatal("menu items")
	}
}

func TestDescribeDetail(t *testing.T) {
	v := report("registered", false, true, "running")
	v.Report.Queue = &corecli.QueueReport{Depth: 3}
	if d := Describe(v).Detail; d != "전송 대기 3건" {
		t.Fatalf("detail: %q", d)
	}
	if d := Describe(report("registered", false, true, "")).Detail; d != "전송 대기 정보 없음" {
		t.Fatalf("detail none: %q", d)
	}
}

// Core가 응답하지 않다가(재시작 중) 돌아오면 다음 주기에 트레이가 회복한다.
func TestRunRecoversWhenCoreReturns(t *testing.T) {
	acts := &fakeActions{status: app.StatusView{Code: "core_timeout"}}
	host := &fakeHost{}
	c := New(acts, host)
	c.Interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for c.Last().State != StateError {
		if time.Now().After(deadline) {
			t.Fatal("no error state")
		}
		time.Sleep(5 * time.Millisecond)
	}
	acts.set(report("registered", false, true, "running"))
	for c.Last().State != StateRunning {
		if time.Now().After(deadline) {
			t.Fatal("did not recover")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestIcons(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range []State{StateRunning, StatePaused, StateError, StateNotRegistered, StateCoreStopped, "other"} {
		b := Icon(s)
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil || img.Bounds().Dx() != 22 {
			t.Fatalf("%s: %v", s, err)
		}
		seen[string(b)] = true
	}
	if len(seen) < 4 {
		t.Fatalf("states should have distinct icons, got %d", len(seen))
	}
	if img, err := png.Decode(bytes.NewReader(IconSize(StateRunning, 256))); err != nil || img.Bounds().Dx() != 256 {
		t.Fatalf("app icon: %v", err)
	}
	ico := ICO(StateRunning)
	// ICONDIR(6) + 항목(16) + BITMAPINFOHEADER(40) + 32×32×4 + AND 마스크(32×4)
	if len(ico) != 6+16+40+32*32*4+32*4 || ico[2] != 1 || ico[4] != 1 || ico[22] != 40 {
		t.Fatalf("ico layout: %d bytes", len(ico))
	}
}
