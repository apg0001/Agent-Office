package main

import (
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/apg0001/Agent-Office/collector/gui/internal/tray"
)

// EnvSelfTestLogout를 1로 두면 등록된 PC에서도 셀프 테스트가 로그아웃(서버 폐기)을 누른다.
// 기본은 등록된 PC면 로그아웃을 건너뛴다(실사용 PC에서 실수로 장치를 폐기하지 않게).
const EnvSelfTestLogout = "AGENT_OFFICE_TRAY_SELFTEST_LOGOUT"

// selfTest는 설치 검증용 셀프 테스트다(#159). 실제 systray 메뉴를 만든 뒤 각 항목의 클릭 채널에
// 클릭을 보내 실제 클릭과 같은 처리 경로(dispatch → Controller.Handle → Core 명령·GUI 실행)를 탄다.
// OS가 그리는 아이콘·메뉴를 사람이 누르는 것은 확인하지 않는다(러너에서 자동화 불가).
//
// 결과 JSON에는 OS·항목·결과 코드·상태 enum만 쓴다(경로·주소·토큰·Core 출력 원문 없음).
type selfTest struct {
	path string
	done chan struct{}

	mu      sync.Mutex
	steps   []step
	browser int
	results chan step
	started time.Time
	readyAt time.Duration
}

type step struct {
	Item     tray.Item  `json:"item"`
	Code     string     `json:"code"`
	State    tray.State `json:"state"`
	Expected []string   `json:"expected"`
	OK       bool       `json:"ok"`
}

type report struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	TrayVersion    string `json:"tray_version"`
	Ready          bool   `json:"ready"`
	ReadyMillis    int64  `json:"ready_ms"`
	BrowserOpens   int    `json:"browser_open_requests"`
	Steps          []step `json:"steps"`
	OK             bool   `json:"ok"`
	NotVerifiedNow string `json:"not_verified"`
}

func newSelfTest(path string) *selfTest {
	return &selfTest{path: path, done: make(chan struct{}), results: make(chan step, 16), started: time.Now()}
}

func (s *selfTest) noteBrowser() {
	s.mu.Lock()
	s.browser++
	s.mu.Unlock()
}

func (s *selfTest) record(it tray.Item, code string, v tray.View) {
	s.results <- step{Item: it, Code: code, State: v.State}
}

// click은 m의 클릭 채널에 클릭을 보내고 처리 결과를 기다린다.
func (s *selfTest) click(it tray.Item, m *systray.MenuItem, expected ...string) bool {
	select {
	case m.ClickedCh <- struct{}{}:
	case <-time.After(10 * time.Second):
		s.add(step{Item: it, Code: "click_not_received", Expected: expected})
		return false
	}
	select {
	case r := <-s.results:
		r.Expected = expected
		r.OK = slices.Contains(expected, r.Code)
		s.add(r)
		return r.OK
	case <-time.After(4 * time.Minute):
		s.add(step{Item: it, Code: "handler_timeout", Expected: expected})
		return false
	}
}

func (s *selfTest) add(r step) {
	s.mu.Lock()
	s.steps = append(s.steps, r)
	s.mu.Unlock()
}

func (s *selfTest) run(ctrl *tray.Controller, items map[tray.Item]*systray.MenuItem) {
	defer close(s.done)
	s.mu.Lock()
	s.readyAt = time.Since(s.started)
	s.mu.Unlock()
	// 첫 상태 갱신(Core `status --json`)을 기다린다.
	deadline := time.Now().Add(60 * time.Second)
	for ctrl.Last().State == "" && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	first := ctrl.Last()
	s.add(step{Item: "status", Code: string(first.State), State: first.State, OK: first.State != "" && first.Label != ""})

	// 원래 일시중지였으면 재개 → 일시중지 순서로 눌러 원래 상태로 돌려놓는다.
	if first.State == tray.StatePaused {
		s.click(tray.ItemResume, items[tray.ItemResume], "resumed")
		s.click(tray.ItemPause, items[tray.ItemPause], "paused")
	} else {
		s.click(tray.ItemPause, items[tray.ItemPause], "paused")
		s.click(tray.ItemResume, items[tray.ItemResume], "resumed")
	}
	// 업데이트 확인은 네트워크·서명 키에 따라 결과가 다르다. 처리 경로가 끝까지 도는지만 본다.
	s.click(tray.ItemUpdate, items[tray.ItemUpdate], "update_latest", "update_available", "update_check_failed")
	s.click(tray.ItemOffice, items[tray.ItemOffice], "office_opened", "settings_opened_no_server")
	s.click(tray.ItemSettings, items[tray.ItemSettings], "settings_opened")
	// GUI 창이 뜰 시간을 준다. 다음 "종료"가 떠 있는 창 프로세스에 --quit을 보내는지 보려는 것이다.
	time.Sleep(10 * time.Second)
	registered := first.CanLogout
	if registered && os.Getenv(EnvSelfTestLogout) != "1" {
		s.add(step{Item: tray.ItemLogout, Code: "skipped_registered", OK: true})
	} else {
		s.click(tray.ItemLogout, items[tray.ItemLogout], "logged_out")
	}
	s.click(tray.ItemQuit, items[tray.ItemQuit], "quit")
}

// finish는 결과 JSON을 쓴다. 모든 단계가 기대한 결과면 0이다.
func (s *selfTest) finish() int {
	// "종료" 클릭 결과가 기록될 때까지 기다린다(systray 루프는 그보다 먼저 끝날 수 있음).
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rep := report{
		OS: runtime.GOOS, Arch: runtime.GOARCH, TrayVersion: version,
		Ready: s.readyAt > 0, ReadyMillis: s.readyAt.Milliseconds(), BrowserOpens: s.browser, Steps: s.steps,
		NotVerifiedNow: "OS 패널에 아이콘이 그려지는지와 사람의 실제 클릭은 이 셀프 테스트로 확인하지 않음",
	}
	rep.OK = rep.Ready && len(s.steps) >= 8
	for _, st := range s.steps {
		rep.OK = rep.OK && st.OK
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.WriteFile(s.path, append(b, '\n'), 0o600); err != nil || !rep.OK {
		return 1
	}
	return 0
}
