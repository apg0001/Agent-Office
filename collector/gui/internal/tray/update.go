package tray

// 트레이 자동 업데이트(#180, ADR-0006): 시작 때와 주기적으로(기본 6시간 ± 10%) 서명된 채널 manifest를 확인하고,
// 새 버전이 있으면 OS 알림과 "업데이트 설치 (vX.Y.Z)" 메뉴를 보인다. 모드(GUI 설정):
//   - notify(기본): 알림 후 사용자가 메뉴를 눌러 승인
//   - download: 받아서 서명·해시를 확인해 두고 알림, 승인하면 바로 설치
//   - auto: 받아서 바로 설치(macOS·Linux는 OS 관리자 승인 창이 그대로 뜬다)
//   - manual: 주기 확인 안 함(메뉴 "업데이트 확인"만)
//
// 설치를 시작하면 Core가 업데이트 도우미를 띄우고 트레이·GUI 창은 끝난다(설치 프로그램이 파일을 바꿀 수 있게).
// 도우미가 설치 뒤 트레이를 다시 띄우면 AfterStart가 결과를 알리고 후처리(`update post-install`)를 한다.

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

// Updater는 트레이 자동 업데이트가 쓰는 동작이다(app.App이 구현).
type Updater interface {
	UpdatePrefs() app.UpdatePrefsView
	InstallerCheck() app.InstallerView
	InstallerDownload() app.InstallerView
	InstallerInstall(confirmed bool) app.InstallerView
	PostInstall() app.PostInstallView
	TakeUpdateOutcome() *app.Outcome
	LastVersion() string
	SetLastVersion(v string)
}

// 확인 주기.
const (
	CheckInterval = 6 * time.Hour
	CheckJitter   = 0.1
	// FirstCheck는 트레이 시작 뒤 첫 확인까지다(로그인 직후 네트워크가 올라올 시간, ± FirstJitter).
	FirstCheck  = 45 * time.Second
	FirstJitter = 0.5
)

// NextDelay는 base에 ±jitter 비율의 무작위 지연을 더한다(여러 PC가 같은 시각에 몰리지 않게). rnd는 [0,1).
func NextDelay(base time.Duration, jitter float64, rnd func() float64) time.Duration {
	if rnd == nil {
		rnd = rand.Float64
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 1 {
		jitter = 1
	}
	d := base + time.Duration((rnd()*2-1)*jitter*float64(base))
	if d < time.Second {
		d = time.Second
	}
	return d
}

type updateState struct {
	version    string // 설치할 수 있는 새 버전(없으면 "")
	downloaded bool
	notified   string // 이미 알린 버전
	installing bool
}

func (u updateState) label() string {
	switch {
	case u.version == "":
		return ""
	case u.installing:
		return "업데이트 설치 중 (v" + u.version + ")"
	case u.downloaded:
		return "업데이트 설치 (v" + u.version + ", 받아 둠)"
	}
	return "업데이트 설치 (v" + u.version + ")"
}

func (c *Controller) notify(title, body string) {
	if c.Notify != nil {
		_ = c.Notify(title, body)
	}
}

func (c *Controller) setUpdateLabel(label string) {
	c.mu.Lock()
	c.update = label
	c.mu.Unlock()
}

// CheckForUpdates는 새 버전을 확인한다. manual은 사용자가 "업데이트 확인"을 누른 경우다(manual 모드에서도 확인).
func (c *Controller) CheckForUpdates(manual bool) string {
	if c.Updates == nil {
		return "update_unavailable"
	}
	mode := c.Updates.UpdatePrefs().Mode
	if !manual && mode == prefs.UpdateManual {
		return "update_skipped_manual"
	}
	v := c.Updates.InstallerCheck()
	switch v.Status {
	case "latest":
		c.mu.Lock()
		c.upd = updateState{notified: c.upd.notified}
		c.update = "업데이트: 최신 버전"
		c.mu.Unlock()
		c.Refresh()
		return "update_latest"
	case "available":
	default:
		if v.Code == "not_installer_install" {
			// 개발 빌드 등 설치 프로그램 배치가 아니면 예전처럼 확인만 한다.
			return c.legacyCheck()
		}
		c.setUpdateLabel("업데이트: 확인 실패")
		c.Refresh()
		return "update_check_failed"
	}
	c.mu.Lock()
	if c.upd.version != v.Version {
		c.upd.version, c.upd.downloaded = v.Version, false
	}
	first := c.upd.notified != v.Version
	c.upd.notified = v.Version
	c.update = "업데이트: 새 버전 v" + v.Version
	c.mu.Unlock()
	if mode == prefs.UpdateDownload || mode == prefs.UpdateAuto {
		if d := c.Updates.InstallerDownload(); d.Status == "downloaded" {
			c.mu.Lock()
			c.upd.downloaded = true
			c.mu.Unlock()
		}
	}
	c.Refresh()
	if mode == prefs.UpdateAuto && !manual {
		c.notify("Agent Office Collector 업데이트", "새 버전 v"+v.Version+"을 자동으로 설치합니다.")
		return c.InstallUpdate()
	}
	if first {
		c.notify("Agent Office Collector 업데이트", "새 버전 v"+v.Version+"이 있습니다. 트레이 메뉴의 '업데이트 설치'를 누르면 설치합니다.")
	}
	return "update_available"
}

// InstallUpdate는 사용자가 승인한(메뉴를 누른) 업데이트를 시작한다. 시작되면 트레이·GUI 창을 끝낸다.
func (c *Controller) InstallUpdate() string {
	if c.Updates == nil {
		return "update_unavailable"
	}
	c.mu.Lock()
	if c.upd.version == "" || c.upd.installing {
		c.mu.Unlock()
		return "update_none"
	}
	c.upd.installing = true
	c.mu.Unlock()
	c.Refresh()
	v := c.Updates.InstallerInstall(true)
	if v.Status != "installer_started" {
		c.mu.Lock()
		c.upd.installing = false
		c.update = "업데이트: 설치 시작 실패"
		c.mu.Unlock()
		c.Refresh()
		c.notify("Agent Office Collector 업데이트 실패", "설치를 시작하지 못했습니다("+safeCode(v.Code)+"). 지금 버전을 계속 씁니다.")
		return "update_install_failed"
	}
	// 설치 프로그램이 실행 파일을 바꿀 수 있게 GUI 창과 트레이를 끝낸다. 도우미가 설치 뒤 트레이를 다시 띄운다.
	if c.LaunchGUI != nil {
		_ = c.LaunchGUI("--quit")
	}
	if c.QuitTray != nil {
		c.QuitTray()
	}
	return "update_installing"
}

// AfterStart는 트레이 시작 때 한 번 부른다. 업데이트 도우미 결과가 있거나 Core 버전이 지난번과 다르면
// (설치 프로그램으로 새로 설치·업데이트·명령행 설치에서 이전) 후처리를 하고 결과를 알린다.
func (c *Controller) AfterStart() string {
	if c.Updates == nil {
		return "skipped"
	}
	out := c.Updates.TakeUpdateOutcome()
	cur := ""
	if st := c.actions.Status(); st.OK && st.Report != nil {
		cur = st.Report.CollectorVersion
	}
	last := c.Updates.LastVersion()
	if out == nil && (cur == "" || cur == last) {
		return "unchanged"
	}
	pi := c.Updates.PostInstall()
	if pi.OK && cur != "" {
		c.Updates.SetLastVersion(cur)
	}
	code := "post_install_ok"
	if !pi.OK {
		code = "post_install_failed"
	}
	switch {
	case out != nil && out.Code == "installed":
		body := "v" + out.To + " 설치와 후처리를 마쳤습니다."
		if pi.CodexRetrust {
			body += " Codex Hook 경로가 바뀌어 Codex를 열고 \"Hooks need review\"에서 다시 신뢰해야 합니다."
		}
		c.notify("Agent Office Collector 업데이트 완료", body)
		code = "update_installed"
	case out != nil && out.Code == "installer_cancelled":
		c.notify("Agent Office Collector 업데이트 취소", "설치를 취소했습니다. 지금 버전(v"+cur+")을 계속 씁니다.")
		code = "update_cancelled"
	case out != nil:
		c.notify("Agent Office Collector 업데이트 실패", "설치하지 못했습니다("+safeCode(out.Code)+"). 지금 버전(v"+cur+")을 계속 씁니다.")
		code = "update_failed"
	case pi.CodexRetrust:
		c.notify("Codex Hook 다시 신뢰 필요", "Collector 위치가 바뀌어 Codex Hook을 갱신했습니다. Codex를 열고 \"Hooks need review\"에서 신뢰하세요.")
	}
	if pi.LegacyFiles > 0 {
		c.notify("이전 명령행 설치 정리", "예전 명령행 설치 파일이 남아 있습니다. 설정 창의 '업데이트' 화면에서 승인하면 지웁니다.")
	}
	c.Refresh()
	return code
}

// UpdateLoop는 ctx가 끝날 때까지 시작 직후와 주기적으로 업데이트를 확인한다.
func (c *Controller) UpdateLoop(ctx context.Context, rnd func() float64) {
	if c.Updates == nil {
		return
	}
	t := time.NewTimer(NextDelay(FirstCheck, FirstJitter, rnd))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.CheckForUpdates(false)
			t.Reset(NextDelay(CheckInterval, CheckJitter, rnd))
		}
	}
}
