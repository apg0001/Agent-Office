//go:build desktop && linux

package main

import (
	"os"
	"strconv"
	"sync"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"

	"github.com/apg0001/Agent-Office/collector/gui/internal/tray"
)

// linuxTray는 fyne.io/systray(StatusNotifierItem·dbusmenu over D-Bus, cgo·GTK 없음)로 트레이를 띄운다.
// Wails의 GTK 메인 루프와 별개로 세션 D-Bus에서만 동작하므로 RunWithExternalLoop로 붙인다.
// GNOME 기본 셸처럼 StatusNotifierWatcher가 없는 환경에서는 아이콘이 보이지 않으며, 그때는
// Visible()이 false라 창 닫기를 트레이 숨기기로 바꾸지 않는다.
type linuxTray struct {
	mu      sync.Mutex
	ready   bool
	pending *tray.View
	status  *systray.MenuItem
	pause   *systray.MenuItem
	resume  *systray.MenuItem
	end     func()
}

func newTrayHost() trayHost {
	if !sessionBusPresent() {
		return nil
	}
	return &linuxTray{}
}

// sessionBusPresent는 세션 D-Bus 주소가 있는지다. 없으면 D-Bus 자동 실행(dbus-launch)을 피하려고 트레이를 끈다.
func sessionBusPresent() bool {
	if a := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); a != "" && a != "autolaunch:" {
		return true
	}
	_, err := os.Stat("/run/user/" + strconv.Itoa(os.Getuid()) + "/bus")
	return err == nil
}

func (t *linuxTray) Visible() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has); err != nil {
		return false
	}
	return has
}

func (t *linuxTray) Start(ctrl *tray.Controller) {
	start, end := systray.RunWithExternalLoop(func() { t.onReady(ctrl) }, nil)
	t.mu.Lock()
	t.end = end
	t.mu.Unlock()
	start()
}

func (t *linuxTray) onReady(ctrl *tray.Controller) {
	systray.SetIcon(tray.Icon(tray.StateCoreStopped))
	systray.SetTitle("Agent Office Collector")
	systray.SetTooltip("Agent Office Collector")
	status := systray.AddMenuItem("상태: 확인 중", "Collector Core 상태")
	status.Disable()
	systray.AddSeparator()
	open := systray.AddMenuItem("창 열기", "설정·상태 창을 엽니다")
	pause := systray.AddMenuItem("수집 일시중지", "transcript·Hook 수집과 전송을 멈춥니다")
	resume := systray.AddMenuItem("수집 재개", "일시중지 동안의 기록은 수집하지 않습니다")
	systray.AddSeparator()
	quit := systray.AddMenuItem("GUI 종료(수집은 계속)", "트레이·창만 닫습니다. Core 수집은 계속됩니다")

	t.mu.Lock()
	t.status, t.pause, t.resume, t.ready = status, pause, resume, true
	pending := t.pending
	t.mu.Unlock()
	if pending != nil {
		t.Apply(*pending)
	}
	go func() {
		for {
			select {
			case <-open.ClickedCh:
				ctrl.Handle(tray.ItemOpen)
			case <-pause.ClickedCh:
				ctrl.Handle(tray.ItemPause)
			case <-resume.ClickedCh:
				ctrl.Handle(tray.ItemResume)
			case <-quit.ClickedCh:
				ctrl.Handle(tray.ItemQuit)
				return
			}
		}
	}()
}

func (t *linuxTray) Apply(v tray.View) {
	t.mu.Lock()
	if !t.ready {
		t.pending = &v
		t.mu.Unlock()
		return
	}
	status, pause, resume := t.status, t.pause, t.resume
	t.mu.Unlock()
	systray.SetIcon(tray.Icon(v.State))
	systray.SetTooltip(v.Tooltip)
	status.SetTitle(v.Label)
	setEnabled(pause, v.CanPause)
	setEnabled(resume, v.CanResume)
}

func setEnabled(m *systray.MenuItem, on bool) {
	if on {
		m.Enable()
	} else {
		m.Disable()
	}
}

func (t *linuxTray) Stop() {
	t.mu.Lock()
	end := t.end
	t.end = nil
	t.mu.Unlock()
	if end != nil {
		end()
	}
}
