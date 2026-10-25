// Package tray는 시스템 트레이(메뉴 막대) 메뉴의 동작 논리다(#43, #159). 화면·D-Bus 없이 테스트한다.
//
// 트레이는 Wails 창과 다른 프로세스(`agent-office-collector-tray`, cmd/agent-office-collector-tray)에서
// fyne.io/systray로 뜬다(ADR-0005). 이 패키지는 Core 상태 → 트레이 표시(상태·대기 건수·툴팁·아이콘·메뉴
// 활성화) 변환과 메뉴 동작만 한다.
//
// 트레이도 GUI와 같이 Core 명령(app.App → corecli)만 쓴다. "종료"는 트레이와 GUI 창 프로세스만 끝내고
// 별도 프로세스인 Core(`agent-office-collector run`, OS 자동 시작)는 계속 수집한다.
package tray

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"sync"
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

// Item은 메뉴 항목이다(명세 §10 트레이 메뉴).
type Item string

const (
	// ItemOffice는 기본 브라우저로 웹 사무실(서버 주소)을 연다. 서버 주소가 없으면 설정 창을 연다.
	ItemOffice Item = "office"
	// ItemSettings는 GUI 설정 창을 띄운다(이미 떠 있으면 앞으로).
	ItemSettings Item = "settings"
	ItemPause    Item = "pause"
	ItemResume   Item = "resume"
	// ItemUpdate는 Core `update check`(확인만, 적용 안 함)다.
	ItemUpdate Item = "update"
	// ItemLogout은 "로그아웃" 하위 메뉴의 확인 항목이다(한 번 더 골라야 실행).
	ItemLogout Item = "logout"
	// ItemQuit은 트레이와 GUI 창을 끝낸다. Core 수집은 계속된다.
	ItemQuit Item = "quit"
)

// Items는 메뉴 순서다(셀프 테스트·문서와 같은 순서).
var Items = []Item{ItemOffice, ItemSettings, ItemPause, ItemResume, ItemUpdate, ItemLogout, ItemQuit}

// View는 트레이 표시 내용이다. 개수·enum·Core가 낸 버전 문구만 담는다(경로·주소·토큰 없음).
type View struct {
	State     State
	Label     string
	Detail    string
	Tooltip   string
	CanPause  bool
	CanResume bool
	// CanLogout은 등록된 상태일 때만 참이다.
	CanLogout bool
	// Update는 마지막 업데이트 확인 결과 문구다(확인 전 "업데이트 확인").
	Update string
}

// Describe는 Core 상태를 트레이 표시로 바꾼다.
func Describe(v app.StatusView) View {
	if !v.OK || v.Report == nil {
		return View{State: StateError, Label: "상태: Core 응답 없음", Tooltip: "Agent Office Collector: Core 상태를 읽을 수 없음(" + safeCode(v.Code) + ")"}
	}
	r := v.Report
	view := View{CanPause: !r.Paused, CanResume: r.Paused, CanLogout: r.Registration == "registered"}
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
	depth := -1
	if r.Queue != nil {
		depth = r.Queue.Depth
	} else if r.Run != nil {
		depth = r.Run.QueueDepth
	}
	if depth >= 0 {
		view.Detail = "전송 대기 " + strconv.Itoa(depth) + "건"
	} else {
		view.Detail = "전송 대기 정보 없음"
	}
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

// Host는 OS 트레이 구현이다(cmd/agent-office-collector-tray의 fyne.io/systray 래퍼).
type Host interface {
	// Apply는 표시를 갱신한다.
	Apply(View)
}

// Actions는 트레이가 쓰는 GUI 백엔드 동작이다(app.App이 구현).
type Actions interface {
	Status() app.StatusView
	Pause() app.Result
	Resume() app.Result
	UpdateCheck() app.Result
	Logout(localOnly, confirmed bool) app.Result
	OfficeURL() (string, error)
}

// Controller는 메뉴 동작과 주기적 상태 갱신을 맡는다.
type Controller struct {
	actions Actions
	host    Host
	// OpenURL은 기본 브라우저로 주소를 연다.
	OpenURL func(url string) error
	// LaunchGUI는 GUI 창 프로세스를 인자와 함께 띄운다. 인자 없음 = 창 열기(이미 떠 있으면 Wails 단일
	// 실행이 기존 창을 앞으로), "--quit" = 떠 있는 GUI 창 프로세스 종료.
	LaunchGUI func(args ...string) error
	// QuitTray는 트레이 프로세스를 끝낸다(systray.Quit).
	QuitTray func()
	// Interval은 상태 갱신 주기다(0이면 5초).
	Interval time.Duration

	mu     sync.Mutex
	last   View
	update string
}

// New는 Controller를 만든다. host가 nil이면 표시 없이 동작한다(테스트·도우미).
func New(actions Actions, host Host) *Controller {
	return &Controller{actions: actions, host: host, update: "업데이트 확인"}
}

// Refresh는 Core 상태를 읽어 트레이를 갱신한다.
func (c *Controller) Refresh() View {
	v := Describe(c.actions.Status())
	c.mu.Lock()
	v.Update = c.update
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

// Handle은 메뉴 항목 클릭이다. 결과 코드(app 결과 코드 또는 아래 고정 코드)를 돌려준다.
// Core를 바꾸는 동작(일시중지·재개·로그아웃) 뒤에는 바로 상태를 다시 읽는다.
func (c *Controller) Handle(it Item) string {
	switch it {
	case ItemOffice:
		u, err := c.actions.OfficeURL()
		if err != nil {
			// 서버 주소가 아직 없으면 설정 창에서 먼저 정한다.
			if c.launch() {
				return "settings_opened_no_server"
			}
			return "gui_unavailable"
		}
		if c.OpenURL == nil || c.OpenURL(u) != nil {
			return "browser_unavailable"
		}
		return "office_opened"
	case ItemSettings:
		if c.launch() {
			return "settings_opened"
		}
		return "gui_unavailable"
	case ItemPause:
		r := c.actions.Pause()
		c.Refresh()
		return r.Code
	case ItemResume:
		r := c.actions.Resume()
		c.Refresh()
		return r.Code
	case ItemUpdate:
		r := c.actions.UpdateCheck()
		label := "업데이트: 확인 실패"
		switch r.Code {
		case "update_latest":
			label = "업데이트: 최신 버전"
		case "update_available":
			label = "업데이트: 새 버전 있음"
			if r.Output != "" {
				label = "업데이트: " + r.Output
			}
		}
		c.mu.Lock()
		c.update = label
		c.mu.Unlock()
		c.Refresh()
		return r.Code
	case ItemLogout:
		r := c.actions.Logout(false, true)
		c.Refresh()
		return r.Code
	case ItemQuit:
		// 떠 있는 GUI 창에 종료를 알린다(없으면 숨긴 채 떴다가 곧바로 끝난다). 실패해도 트레이는 끝낸다.
		if c.LaunchGUI != nil {
			_ = c.LaunchGUI("--quit")
		}
		if c.QuitTray != nil {
			c.QuitTray()
		}
		return "quit"
	}
	return "unexpected"
}

func (c *Controller) launch() bool {
	return c.LaunchGUI != nil && c.LaunchGUI() == nil
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
func Icon(s State) []byte { return IconSize(s, 22) }

// IconSize는 n×n PNG 아이콘이다(패키지용 앱 아이콘은 256).
func IconSize(s State, n int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, iconImage(s, n))
	return b.Bytes()
}

// ICO는 Windows 알림 영역용 .ico다. fyne.io/systray는 Windows에서 LoadImage(IMAGE_ICON)로 읽으므로
// PNG가 아닌 ICO가 필요하다. 호환성을 위해 PNG 압축 대신 32비트 BGRA DIB + AND 마스크로 만든다.
func ICO(s State) []byte {
	const n = 32
	img := iconImage(s, n)
	mask := ((n + 31) / 32) * 4 * n // 1비트 AND 마스크(행마다 4바이트 정렬), 0 = 불투명 처리는 알파가 맡음
	dib := 40 + n*n*4 + mask
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	w([]uint16{0, 1, 1})                             // ICONDIR: reserved, type=icon, count
	w([]uint8{n, n, 0, 0})                           // 너비, 높이, 색 수, reserved
	w([]uint16{1, 32})                               // planes, bpp
	w([]uint32{uint32(dib), 6 + 16})                 // 크기, 오프셋
	w([]uint32{40, n, 2 * n})                        // BITMAPINFOHEADER: 크기, 너비, 높이(XOR+AND)
	w([]uint16{1, 32})                               // planes, bpp
	w([]uint32{0, uint32(n*n*4 + mask), 0, 0, 0, 0}) // 압축 없음, 이미지 크기, 해상도, 색
	for y := n - 1; y >= 0; y-- {                    // 아래 행부터
		for x := 0; x < n; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, mask))
	return b.Bytes()
}

func iconImage(s State, n int) *image.NRGBA {
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
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	c := float64(n)/2 - 0.5
	outer, inner := float64(n)/2-0.5, float64(n)/2-0.5-1.5*float64(n)/22
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			d := dx*dx + dy*dy
			switch {
			case d <= inner*inner:
				img.SetNRGBA(x, y, fill)
			case d <= outer*outer:
				img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
			}
		}
	}
	return img
}
