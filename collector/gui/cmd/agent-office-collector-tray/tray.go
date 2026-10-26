package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/launch"
	"github.com/apg0001/Agent-Office/collector/gui/internal/notify"
	"github.com/apg0001/Agent-Office/collector/gui/internal/tray"
)

var errGUIMissing = errors.New("tray: gui executable not found")

// quitWait는 "종료" 때 GUI `--quit` 프로세스를 기다리는 상한이다(떠 있지 않으면 숨긴 채 떴다 끝남).
const quitWait = 30 * time.Second

func selfPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

func runTray(selfTestPath, updateTestPath string, interactive bool) int {
	exe := selfPath()
	a := app.New(app.Options{})
	h := &host{}
	ctrl := tray.New(a, h)
	gui, guiOK := corecli.FindSibling(exe, runtime.GOOS, corecli.GUIName)
	ctrl.OpenURL = launch.OpenURL
	ctrl.LaunchGUI = func(args ...string) error {
		if !guiOK {
			return errGUIMissing
		}
		if len(args) > 0 && args[0] == "--quit" {
			return launch.Wait(quitWait, gui, args...)
		}
		return launch.Detached(gui, args...)
	}
	ctrl.QuitTray = systray.Quit
	ctrl.Updates = a
	ctrl.Notify = notify.Send

	var st *selfTest
	if selfTestPath != "" {
		st = newSelfTest(selfTestPath)
		// 셀프 테스트는 브라우저를 실제로 열지 않고 열려던 사실만 기록한다(주소는 남기지 않음).
		ctrl.OpenURL = func(string) error { st.noteBrowser(); return nil }
		// 메뉴 셀프 테스트는 업데이트 자동 확인·후처리를 하지 않는다(메뉴 경로만 본다).
		ctrl.Updates, ctrl.Notify = nil, nil
	}
	var ut *updateTest
	if updateTestPath != "" {
		ut = newUpdateTest(updateTestPath)
		ctrl.OpenURL = func(string) error { return nil }
		ctrl.Notify = ut.noteNotify
	}
	ctx, cancel := context.WithCancel(context.Background())
	systray.Run(func() {
		items := h.build()
		h.dispatch(ctrl, items, st, ut)
		go ctrl.Run(ctx)
		switch {
		case st != nil:
			go st.run(ctrl, items)
		case ut != nil:
			go ut.run(ctrl, items)
		default:
			go func() {
				// 설치·업데이트 뒤 후처리와 결과 알림, 그 다음 첫 실행 마법사(인자 없이 떴을 때만).
				ctrl.AfterStart()
				if interactive && a.SetupStatus().Show {
					_ = ctrl.LaunchGUI()
				}
				ctrl.UpdateLoop(ctx, nil)
			}()
		}
	}, cancel)
	cancel()
	if st != nil {
		return st.finish()
	}
	if ut != nil {
		return ut.finish()
	}
	return 0
}

// host는 fyne.io/systray 메뉴다. Apply는 어느 고루틴에서 불러도 된다(systray가 내부에서 직렬화).
type host struct {
	mu      sync.Mutex
	ready   bool
	pending *tray.View
	status  *systray.MenuItem
	detail  *systray.MenuItem
	pause   *systray.MenuItem
	resume  *systray.MenuItem
	update  *systray.MenuItem
	install *systray.MenuItem
	logout  *systray.MenuItem
}

func icon(s tray.State) []byte {
	if runtime.GOOS == "windows" {
		return tray.ICO(s)
	}
	return tray.Icon(s)
}

// build는 명세 §10 순서로 메뉴를 만든다. 항목 → 메뉴 객체 표를 돌려준다.
func (h *host) build() map[tray.Item]*systray.MenuItem {
	systray.SetIcon(icon(tray.StateCoreStopped))
	if runtime.GOOS == "linux" {
		// macOS는 제목이 메뉴 막대에 글자로 보이므로 Linux(StatusNotifierItem Title)에만 둔다.
		systray.SetTitle("Agent Office Collector")
	}
	systray.SetTooltip("Agent Office Collector")
	status := systray.AddMenuItem("상태: 확인 중", "Collector Core 상태")
	status.Disable()
	detail := systray.AddMenuItem("전송 대기 확인 중", "로컬 큐에서 서버 전송을 기다리는 이벤트 수")
	detail.Disable()
	systray.AddSeparator()
	office := systray.AddMenuItem("사무실 열기", "기본 브라우저로 Agent Office 웹을 엽니다")
	settings := systray.AddMenuItem("설정 열기", "등록·Hook·자동 시작 설정 창을 엽니다")
	systray.AddSeparator()
	pause := systray.AddMenuItem("수집 일시중지", "transcript·Hook 수집과 전송을 멈춥니다")
	resume := systray.AddMenuItem("수집 재개", "일시중지 동안의 기록은 수집하지 않습니다")
	update := systray.AddMenuItem("업데이트 확인", "서명된 업데이트 정보에서 새 Collector 버전을 확인합니다")
	install := systray.AddMenuItem("업데이트 설치", "새 버전 설치 파일을 받아 서명·해시를 확인하고 설치합니다(누르면 승인)")
	install.Hide()
	logout := systray.AddMenuItem("로그아웃", "이 PC의 장치 등록을 해제합니다")
	logoutConfirm := logout.AddSubMenuItem("이 PC 로그아웃(서버에서 장치 폐기)", "서버에서 이 장치를 폐기하고 로컬 장치 토큰을 지웁니다")
	systray.AddSeparator()
	quit := systray.AddMenuItem("종료(수집은 계속)", "트레이와 설정 창만 닫습니다. Core 수집은 계속됩니다")

	h.mu.Lock()
	h.status, h.detail, h.pause, h.resume, h.update, h.install, h.logout, h.ready = status, detail, pause, resume, update, install, logout, true
	pending := h.pending
	h.mu.Unlock()
	if pending != nil {
		h.Apply(*pending)
	}
	return map[tray.Item]*systray.MenuItem{
		tray.ItemOffice: office, tray.ItemSettings: settings, tray.ItemPause: pause, tray.ItemResume: resume,
		tray.ItemUpdate: update, tray.ItemInstall: install, tray.ItemLogout: logoutConfirm, tray.ItemQuit: quit,
	}
}

// dispatch는 항목마다 클릭 채널을 읽어 Controller로 넘긴다. 한 항목 처리 중 같은 항목 클릭은 기다린다.
func (h *host) dispatch(ctrl *tray.Controller, items map[tray.Item]*systray.MenuItem, st *selfTest, ut *updateTest) {
	for it, m := range items {
		go func(it tray.Item, m *systray.MenuItem) {
			for range m.ClickedCh {
				code := ctrl.Handle(it)
				if st != nil {
					st.record(it, code, ctrl.Last())
				}
				if ut != nil && it == tray.ItemInstall {
					ut.noteClick(code)
				}
				if it == tray.ItemQuit {
					return
				}
			}
		}(it, m)
	}
}

func (h *host) Apply(v tray.View) {
	h.mu.Lock()
	if !h.ready {
		h.pending = &v
		h.mu.Unlock()
		return
	}
	status, detail, pause, resume, update, install, logout := h.status, h.detail, h.pause, h.resume, h.update, h.install, h.logout
	h.mu.Unlock()
	systray.SetIcon(icon(v.State))
	systray.SetTooltip(v.Tooltip)
	status.SetTitle(v.Label)
	detail.SetTitle(v.Detail)
	update.SetTitle(v.Update)
	if v.Install == "" {
		install.Hide()
	} else {
		install.SetTitle(v.Install)
		install.Show()
	}
	setEnabled(pause, v.CanPause)
	setEnabled(resume, v.CanResume)
	setEnabled(logout, v.CanLogout)
}

func setEnabled(m *systray.MenuItem, on bool) {
	if on {
		m.Enable()
	} else {
		m.Disable()
	}
}

// actionsCheck는 app.App이 트레이 Actions를 구현하는지 컴파일 때 확인한다.
var _ tray.Actions = (*app.App)(nil)
