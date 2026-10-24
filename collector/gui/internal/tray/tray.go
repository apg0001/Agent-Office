// Package tray는 시스템 트레이(메뉴 막대) 메뉴의 동작 논리다(#43). 화면·D-Bus 없이 테스트한다.
//
// 실제 아이콘 표시는 main 패키지의 OS별 Host 구현(Linux: fyne.io/systray, StatusNotifierItem)이 맡고,
// 이 패키지는 Core 상태 → 트레이 표시(상태·툴팁·아이콘·메뉴 활성화) 변환, 메뉴 동작, 창 닫기 판단만 한다.
//
// 트레이도 GUI와 같이 Core 명령(app.App → corecli)만 쓴다. "GUI 종료"는 GUI 프로세스만 끝내고
// 별도 프로세스인 Core(`agent-office-collector run`, OS 자동 시작)는 계속 수집한다.
package tray

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
)

// State는 트레이에 보이는 요약 상태다.
type State string

const (
	StateRunning       State = "running"
	StatePaused        State = "paused"
	StateError         State = "error"
	StateNotRegistered State = "not_registered"
	// StateCoreStopped는 Core `run` 프로세스가 없다(자동 시작 미설치·중지·재시작 중).
	StateCoreStopped State = "core_stopped"
)

// Item은 메뉴 항목이다.
type Item string

const (
	ItemOpen   Item = "open"
	ItemPause  Item = "pause"
	ItemResume Item = "resume"
	ItemQuit   Item = "quit"
)

// View는 트레이 표시 내용이다. 개수·enum에서 만든 한국어 문구만 담는다(경로·주소·토큰 없음).
type View struct {
	State     State
	Label     string
	Tooltip   string
	CanPause  bool
	CanResume bool
}

// Describe는 Core 상태를 트레이 표시로 바꾼다.
func Describe(v app.StatusView) View {
	if !v.OK || v.Report == nil {
		return View{State: StateError, Label: "상태: Core 응답 없음", Tooltip: "Agent Office Collector: Core 상태를 읽을 수 없음(" + safeCode(v.Code) + ")"}
	}
	r := v.Report
	view := View{CanPause: !r.Paused, CanResume: r.Paused}
	switch {
	case r.Registration == "not_registered":
		view.State, view.Label = StateNotRegistered, "상태: 기기 등록 필요"
	case r.Registration != "registered":
		view.State, view.Label = StateError, "상태: 설정 손상(다시 등록 필요)"
	case r.Paused:
		view.State, view.Label = StatePaused, "상태: 일시중지"
	case !r.ProcessRunning:
		view.State, view.Label = StateCoreStopped, "상태: Core 실행 중 아님"
	case r.Run != nil && (r.Run.State == "device_revoked" || r.Run.State == "token_unavailable" || r.Run.State == "config_invalid"):
		view.State = StateError
		view.Label = map[string]string{
			"device_revoked":    "상태: 장치 폐기됨(다시 등록 필요)",
			"token_unavailable": "상태: 보안 저장소 잠김(토큰 읽기 실패)",
			"config_invalid":    "상태: 설정 손상",
		}[r.Run.State]
	default:
		view.State, view.Label = StateRunning, "상태: 수집 중"
	}
	view.Tooltip = "Agent Office Collector — " + view.Label[len("상태: "):]
	return view
}

func safeCode(c string) string {
	if c == "" {
		return "unexpected"
	}
	for _, r := range c {
		if !(r >= 'a' && r <= 'z' || r == '_') {
			return "unexpected"
		}
	}
	return c
}

// Host는 OS 트레이 구현이다.
type Host interface {
	// Apply는 표시를 갱신한다.
	Apply(View)
	// Visible은 지금 트레이 아이콘이 실제로 보일 수 있는지다(Linux: StatusNotifierWatcher 존재).
	// false면 창 닫기를 트레이 숨기기로 바꾸지 않는다(창을 다시 열 방법이 없어지므로).
	Visible() bool
}

// Actions는 트레이가 쓰는 GUI 백엔드 동작이다(app.App이 구현).
type Actions interface {
	Status() app.StatusView
	Pause() app.Result
	Resume() app.Result
}

// Controller는 메뉴 동작과 주기적 상태 갱신을 맡는다.
type Controller struct {
	actions Actions
	host    Host
	// ShowWindow는 창을 보이고 앞으로 가져온다. QuitGUI는 GUI 프로세스만 끝낸다.
	ShowWindow func()
	QuitGUI    func()
	// Interval은 상태 갱신 주기다(0이면 5초).
	Interval time.Duration

	mu       sync.Mutex
	last     View
	quitting atomic.Bool
}

// New는 Controller를 만든다. host가 nil이면 트레이 없이 동작한다(창 닫기 = GUI 종료).
func New(actions Actions, host Host) *Controller {
	return &Controller{actions: actions, host: host}
}

// Refresh는 Core 상태를 읽어 트레이를 갱신한다.
func (c *Controller) Refresh() View {
	v := Describe(c.actions.Status())
	c.mu.Lock()
	c.last = v
	c.mu.Unlock()
	if c.host != nil {
		c.host.Apply(v)
	}
	return v
}

// Last는 마지막 표시다.
func (c *Controller) Last() View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// Handle은 메뉴 항목 클릭이다. 일시중지·재개 뒤에는 바로 상태를 다시 읽는다.
func (c *Controller) Handle(it Item) {
	switch it {
	case ItemOpen:
		if c.ShowWindow != nil {
			c.ShowWindow()
		}
	case ItemPause:
		c.actions.Pause()
		c.Refresh()
	case ItemResume:
		c.actions.Resume()
		c.Refresh()
	case ItemQuit:
		c.quitting.Store(true)
		if c.QuitGUI != nil {
			c.QuitGUI()
		}
	}
}

// BeforeClose는 창 닫기(X) 처리다. 트레이가 보이면 창만 숨기고 true(닫기 취소)를 돌려준다.
// 트레이 "GUI 종료" 중이거나 트레이가 없으면 false(정상 종료)다. 어느 경우에도 Core는 영향이 없다.
func (c *Controller) BeforeClose(hide func()) bool {
	if c.quitting.Load() || c.host == nil || !c.host.Visible() {
		return false
	}
	if hide != nil {
		hide()
	}
	return true
}

// StartHidden은 자동 시작(--autostart)으로 실행됐고 트레이가 보일 때만 창 없이 시작한다.
func (c *Controller) StartHidden(autostart bool) bool {
	return autostart && c.host != nil && c.host.Visible()
}

// Run은 ctx가 끝날 때까지 주기적으로 Refresh한다. Core가 멈췄다 다시 시작해도 다음 주기에 회복한다.
func (c *Controller) Run(ctx context.Context) {
	d := c.Interval
	if d <= 0 {
		d = 5 * time.Second
	}
	c.Refresh()
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Refresh()
		}
	}
}

// Icon은 상태별 22×22 PNG 아이콘이다(이미지 파일 없이 생성, 색으로 상태 구분).
func Icon(s State) []byte {
	fill := map[State]color.NRGBA{
		StateRunning:       {R: 0x2e, G: 0x9e, B: 0x5b, A: 0xff},
		StatePaused:        {R: 0xd9, G: 0x9a, B: 0x1e, A: 0xff},
		StateError:         {R: 0xd1, G: 0x3b, B: 0x3b, A: 0xff},
		StateNotRegistered: {R: 0x6b, G: 0x72, B: 0x80, A: 0xff},
		StateCoreStopped:   {R: 0x6b, G: 0x72, B: 0x80, A: 0xff},
	}[s]
	if fill.A == 0 {
		fill = color.NRGBA{R: 0x6b, G: 0x72, B: 0x80, A: 0xff}
	}
	const n = 22
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-10.5, float64(y)-10.5
			d := dx*dx + dy*dy
			switch {
			case d <= 9*9:
				img.SetNRGBA(x, y, fill)
			case d <= 10.5*10.5:
				img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
