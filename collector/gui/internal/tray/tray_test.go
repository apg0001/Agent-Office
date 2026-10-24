package tray

import (
	"bytes"
	"context"
	"image/png"
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

func (f *fakeActions) set(v app.StatusView) {
	f.mu.Lock()
	f.status = v
	f.mu.Unlock()
}

type fakeHost struct {
	mu      sync.Mutex
	views   []View
	visible bool
}

func (h *fakeHost) Apply(v View) {
	h.mu.Lock()
	h.views = append(h.views, v)
	h.mu.Unlock()
}

func (h *fakeHost) Visible() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.visible
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
	host := &fakeHost{visible: true}
	c := New(acts, host)
	shown, quit := 0, 0
	c.ShowWindow = func() { shown++ }
	c.QuitGUI = func() { quit++ }

	if v := c.Refresh(); v.State != StateRunning || host.count() != 1 {
		t.Fatalf("refresh: %+v", v)
	}
	c.Handle(ItemPause)
	if c.Last().State != StatePaused || !c.Last().CanResume {
		t.Fatalf("after pause: %+v", c.Last())
	}
	c.Handle(ItemResume)
	if c.Last().State != StateRunning {
		t.Fatalf("after resume: %+v", c.Last())
	}
	c.Handle(ItemOpen)
	if shown != 1 {
		t.Fatal("open window")
	}

	// 트레이가 보이면 창 닫기 = 숨기기(닫기 취소), Core 명령은 부르지 않는다.
	hidden := 0
	before := len(acts.calls)
	if !c.BeforeClose(func() { hidden++ }) || hidden != 1 {
		t.Fatal("close should hide to tray")
	}
	if len(acts.calls) != before {
		t.Fatal("closing the window called Core")
	}
	host.mu.Lock()
	host.visible = false
	host.mu.Unlock()
	if c.BeforeClose(func() { hidden++ }) || hidden != 1 {
		t.Fatal("without a visible tray, close must really close (window would be unreachable)")
	}
	host.mu.Lock()
	host.visible = true
	host.mu.Unlock()
	c.Handle(ItemQuit)
	if quit != 1 || c.BeforeClose(func() { hidden++ }) {
		t.Fatal("quit from tray must not be cancelled by hide-on-close")
	}
	for _, call := range acts.calls {
		if call != "status" && call != "pause" && call != "resume" {
			t.Fatalf("unexpected core action %q", call)
		}
	}
}

func TestStartHidden(t *testing.T) {
	acts := &fakeActions{status: report("registered", false, true, "running")}
	if New(acts, nil).StartHidden(true) {
		t.Error("no tray: must show window")
	}
	if New(acts, &fakeHost{visible: false}).StartHidden(true) {
		t.Error("invisible tray: must show window")
	}
	if !New(acts, &fakeHost{visible: true}).StartHidden(true) {
		t.Error("autostart with tray: start hidden")
	}
	if New(acts, &fakeHost{visible: true}).StartHidden(false) {
		t.Error("manual start: show window")
	}
	// host nil이면 BeforeClose는 항상 정상 종료.
	if New(acts, nil).BeforeClose(nil) {
		t.Error("nil host close")
	}
}

// Core가 응답하지 않다가(재시작 중) 돌아오면 다음 주기에 트레이가 회복한다.
func TestRunRecoversWhenCoreReturns(t *testing.T) {
	acts := &fakeActions{status: app.StatusView{Code: "core_timeout"}}
	host := &fakeHost{visible: true}
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
}
