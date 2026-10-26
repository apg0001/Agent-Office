package main

import (
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/apg0001/Agent-Office/collector/gui/internal/tray"
)

// updateTest는 업데이트 검증용 셀프 테스트다(#180). 실제 트레이를 띄운 뒤 곧바로 업데이트를 확인하고, 새 버전이
// 보이면 "업데이트 설치" 메뉴 항목의 클릭 채널에 클릭을 보내 사람이 누른 것과 같은 경로(Handle → InstallUpdate →
// Core `update install --installer --yes` → 도우미 → 트레이 종료)를 탄다. OS 알림은 띄우지 않고 횟수만 센다.
// 결과 JSON에는 OS·버전·결과 코드·개수만 쓴다.
type updateTest struct {
	path string
	done chan struct{}

	mu  sync.Mutex
	rep updateReport
}

type updateReport struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	TrayVersion   string `json:"tray_version"`
	AfterStart    string `json:"after_start"`
	Check         string `json:"check"`
	Offered       string `json:"offered_label"`
	InstallClick  string `json:"install_click"`
	Notifications int    `json:"notifications"`
	OK            bool   `json:"ok"`
}

func newUpdateTest(path string) *updateTest {
	return &updateTest{path: path, done: make(chan struct{}),
		rep: updateReport{OS: runtime.GOOS, Arch: runtime.GOARCH, TrayVersion: version}}
}

func (u *updateTest) noteNotify(string, string) error {
	u.mu.Lock()
	u.rep.Notifications++
	u.mu.Unlock()
	return nil
}

func (u *updateTest) set(f func(*updateReport)) {
	u.mu.Lock()
	f(&u.rep)
	u.mu.Unlock()
}

func (u *updateTest) run(ctrl *tray.Controller, items map[tray.Item]*systray.MenuItem) {
	defer close(u.done)
	after := ctrl.AfterStart()
	u.set(func(r *updateReport) { r.AfterStart = after })
	code := ""
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
		if code = ctrl.CheckForUpdates(true); code == "update_available" {
			break
		}
	}
	label := ctrl.Last().Install
	u.set(func(r *updateReport) { r.Check, r.Offered = code, label })
	if code != "update_available" {
		systray.Quit()
		return
	}
	// 실제 메뉴 항목 클릭 채널로 승인한다(dispatch가 Handle(ItemInstall)을 부른다).
	select {
	case items[tray.ItemInstall].ClickedCh <- struct{}{}:
	case <-time.After(10 * time.Second):
		u.set(func(r *updateReport) { r.InstallClick = "click_not_received" })
		systray.Quit()
		return
	}
	// 설치가 시작되면 트레이가 스스로 끝난다(QuitTray). 끝나지 않으면 실패로 기록하고 끝낸다.
	time.Sleep(20 * time.Second)
	u.set(func(r *updateReport) {
		if r.InstallClick == "" {
			r.InstallClick = "tray_still_running"
		}
	})
	systray.Quit()
}

// noteClick은 dispatch가 ItemInstall 처리 결과를 알릴 때 쓴다.
func (u *updateTest) noteClick(code string) {
	u.set(func(r *updateReport) {
		if r.InstallClick == "" {
			r.InstallClick = code
		}
	})
}

func (u *updateTest) finish() int {
	select {
	case <-u.done:
	case <-time.After(5 * time.Second):
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.rep.OK = u.rep.Check == "update_available" && u.rep.InstallClick == "update_installing" && u.rep.Offered != ""
	b, _ := json.MarshalIndent(u.rep, "", "  ")
	if err := os.WriteFile(u.path, append(b, '\n'), 0o600); err != nil || !u.rep.OK {
		return 1
	}
	return 0
}
