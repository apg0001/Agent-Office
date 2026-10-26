package tray

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

type fakeUpdater struct {
	mu       sync.Mutex
	mode     string
	check    app.InstallerView
	install  app.InstallerView
	outcome  *app.Outcome
	last     string
	post     app.PostInstallView
	calls    []string
	setLast  string
	checkNum int
}

func (f *fakeUpdater) log(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeUpdater) UpdatePrefs() app.UpdatePrefsView {
	return app.UpdatePrefsView{OK: true, Mode: f.mode}
}
func (f *fakeUpdater) InstallerCheck() app.InstallerView {
	f.log("check")
	f.mu.Lock()
	f.checkNum++
	f.mu.Unlock()
	return f.check
}
func (f *fakeUpdater) InstallerDownload() app.InstallerView {
	f.log("download")
	return app.InstallerView{OK: true, Status: "downloaded", Version: f.check.Version}
}
func (f *fakeUpdater) InstallerInstall(confirmed bool) app.InstallerView {
	if !confirmed {
		f.log("install-unconfirmed")
		return app.InstallerView{Status: "error"}
	}
	f.log("install")
	return f.install
}
func (f *fakeUpdater) PostInstall() app.PostInstallView { f.log("post"); return f.post }
func (f *fakeUpdater) TakeUpdateOutcome() *app.Outcome {
	o := f.outcome
	f.outcome = nil
	return o
}
func (f *fakeUpdater) LastVersion() string     { return f.last }
func (f *fakeUpdater) SetLastVersion(v string) { f.setLast = v }

type notes struct {
	mu   sync.Mutex
	list []string
}

func (n *notes) add(title, body string) error {
	n.mu.Lock()
	n.list = append(n.list, title+"|"+body)
	n.mu.Unlock()
	return nil
}

func newUpdateController(u *fakeUpdater) (*Controller, *fakeHost, *notes, *[]string) {
	acts := &fakeActions{status: report("registered", false, true, "running")}
	acts.status.Report.CollectorVersion = "0.1.0"
	host := &fakeHost{}
	c := New(acts, host)
	n := &notes{}
	var events []string
	c.Updates, c.Notify = u, n.add
	c.LaunchGUI = func(args ...string) error { events = append(events, "gui"+join(args)); return nil }
	c.QuitTray = func() { events = append(events, "quit") }
	return c, host, n, &events
}

func join(a []string) string {
	s := ""
	for _, x := range a {
		s += " " + x
	}
	return s
}

func TestNextDelayJitter(t *testing.T) {
	if d := NextDelay(6*time.Hour, 0.1, func() float64 { return 0 }); d != 6*time.Hour-36*time.Minute {
		t.Fatalf("low %v", d)
	}
	if d := NextDelay(6*time.Hour, 0.1, func() float64 { return 0.5 }); d != 6*time.Hour {
		t.Fatalf("mid %v", d)
	}
	if d := NextDelay(time.Second, 5, func() float64 { return 0 }); d != time.Second {
		t.Fatalf("floor %v", d)
	}
	for i := 0; i < 1000; i++ {
		d := NextDelay(CheckInterval, CheckJitter, nil)
		if d < 5*time.Hour+24*time.Minute || d > 6*time.Hour+36*time.Minute {
			t.Fatalf("out of range %v", d)
		}
	}
}

func TestNotifyModeOffersInstallAndWaitsForApproval(t *testing.T) {
	u := &fakeUpdater{mode: prefs.UpdateNotify, check: app.InstallerView{OK: true, Status: "available", Version: "0.2.0"},
		install: app.InstallerView{OK: true, Status: "installer_started", Version: "0.2.0"}}
	c, _, n, events := newUpdateController(u)
	if code := c.CheckForUpdates(false); code != "update_available" {
		t.Fatal(code)
	}
	if v := c.Last(); v.Install != "업데이트 설치 (v0.2.0)" {
		t.Fatalf("label %q", v.Install)
	}
	// 같은 버전은 한 번만 알린다. 승인 전에는 받거나 설치하지 않는다.
	c.CheckForUpdates(false)
	if len(n.list) != 1 || contains(u.calls, "download") || contains(u.calls, "install") {
		t.Fatalf("notes %q calls %q", n.list, u.calls)
	}
	if code := c.Handle(ItemInstall); code != "update_installing" {
		t.Fatal(code)
	}
	if !contains(u.calls, "install") || len(*events) != 2 || (*events)[0] != "gui --quit" || (*events)[1] != "quit" {
		t.Fatalf("calls %q events %q", u.calls, *events)
	}
	if c.Handle(ItemInstall) != "update_none" {
		t.Fatal("second click started another install")
	}
}

func TestDownloadAutoAndManualModes(t *testing.T) {
	avail := app.InstallerView{OK: true, Status: "available", Version: "0.3.0"}
	started := app.InstallerView{Status: "installer_started"}
	u := &fakeUpdater{mode: prefs.UpdateDownload, check: avail, install: started}
	c, _, _, events := newUpdateController(u)
	c.CheckForUpdates(false)
	if !contains(u.calls, "download") || contains(u.calls, "install") || c.Last().Install != "업데이트 설치 (v0.3.0, 받아 둠)" {
		t.Fatalf("download mode %q %q", u.calls, c.Last().Install)
	}
	u = &fakeUpdater{mode: prefs.UpdateAuto, check: avail, install: started}
	c, _, _, events = newUpdateController(u)
	if code := c.CheckForUpdates(false); code != "update_installing" || len(*events) != 2 {
		t.Fatalf("auto %s %q", code, *events)
	}
	u = &fakeUpdater{mode: prefs.UpdateManual, check: avail}
	c, _, _, _ = newUpdateController(u)
	if code := c.CheckForUpdates(false); code != "update_skipped_manual" || len(u.calls) != 0 {
		t.Fatalf("manual %s %q", code, u.calls)
	}
	// manual 모드에서도 사용자가 "업데이트 확인"을 누르면 확인한다(설치는 하지 않음).
	if code := c.Handle(ItemUpdate); code != "update_available" || contains(u.calls, "install") {
		t.Fatalf("manual click %s", code)
	}
}

func TestInstallFailureKeepsTrayAndReports(t *testing.T) {
	u := &fakeUpdater{mode: prefs.UpdateNotify, check: app.InstallerView{Status: "available", Version: "0.2.0"},
		install: app.InstallerView{Status: "error", Code: "checksum_mismatch"}}
	c, _, n, events := newUpdateController(u)
	c.CheckForUpdates(false)
	if code := c.InstallUpdate(); code != "update_install_failed" || len(*events) != 0 {
		t.Fatalf("%s %q", code, *events)
	}
	if last := n.list[len(n.list)-1]; !strings.Contains(last, "checksum_mismatch") {
		t.Fatalf("note %q", last)
	}
	if c.Last().Install == "" {
		t.Fatal("install item hidden after failure (retry should be possible)")
	}
}

func TestAfterStartPostInstall(t *testing.T) {
	// 버전이 같고 결과가 없으면 아무것도 하지 않는다.
	u := &fakeUpdater{last: "0.1.0"}
	c, _, _, _ := newUpdateController(u)
	if c.AfterStart() != "unchanged" || contains(u.calls, "post") {
		t.Fatal("ran post-install without change")
	}
	// 업데이트 성공 결과: 후처리 → 버전 기록 → 완료 알림(+ Codex 재신뢰 안내).
	u = &fakeUpdater{last: "0.0.9", outcome: &app.Outcome{From: "0.0.9", To: "0.1.0", Code: "installed"},
		post: app.PostInstallView{OK: true, Status: "attention", CodexRetrust: true, LegacyFiles: 1}}
	c, _, n, _ := newUpdateController(u)
	if code := c.AfterStart(); code != "update_installed" || u.setLast != "0.1.0" {
		t.Fatalf("%s last=%q", code, u.setLast)
	}
	if len(n.list) != 2 || !strings.Contains(n.list[0], "Hooks need review") || !strings.Contains(n.list[1], "이전 명령행") {
		t.Fatalf("notes %q", n.list)
	}
	// 실패 결과: 후처리(서비스 재시작)는 하고 이전 버전 유지 안내.
	u = &fakeUpdater{last: "0.1.0", outcome: &app.Outcome{From: "0.1.0", To: "0.2.0", Code: "installer_failed"}, post: app.PostInstallView{OK: true}}
	c, _, n, _ = newUpdateController(u)
	if code := c.AfterStart(); code != "update_failed" || !contains(u.calls, "post") || !strings.Contains(n.list[0], "0.1.0") {
		t.Fatalf("%s %q", code, n.list)
	}
	// 명령행 설치에서 설치 프로그램으로 처음 옮겨 온 경우(LastVersion 없음): 후처리만, 알림은 Codex 안내만.
	u = &fakeUpdater{post: app.PostInstallView{OK: true, CodexRetrust: true}}
	c, _, n, _ = newUpdateController(u)
	if code := c.AfterStart(); code != "post_install_ok" || len(n.list) != 1 {
		t.Fatalf("%s %q", code, n.list)
	}
}

func TestUpdateLoopChecksAtStart(t *testing.T) {
	u := &fakeUpdater{mode: prefs.UpdateNotify, check: app.InstallerView{Status: "latest"}}
	c, _, _, _ := newUpdateController(u)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	// rnd=0 → 첫 확인은 FirstCheck의 절반(약 22초)이라 테스트에서는 짧게 바꾼 상수 대신 직접 호출로 본다.
	go func() { c.UpdateLoop(ctx, func() float64 { return 0 }); close(done) }()
	cancel()
	<-done
	if c.CheckForUpdates(false) != "update_latest" || c.Last().Install != "" {
		t.Fatal("latest")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
