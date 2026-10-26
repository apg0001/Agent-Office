// Package notify는 OS 알림을 띄운다(#180, 업데이트 알림). fyne.io/systray에는 알림 API가 없어 OS 기본 수단을 쓴다.
//
//   - macOS: osascript `display notification`(제목·본문은 argv로 넘김)
//   - Linux: notify-send(libnotify, org.freedesktop.Notifications). 없으면 알리지 않는다(트레이 메뉴 표시는 그대로).
//   - Windows: PowerShell로 Windows 토스트(PowerShell의 AppUserModelID). 제목·본문은 환경 변수로 넘긴다.
//
// 알림 실패는 무시한다(트레이 메뉴 "업데이트 설치 (vX.Y.Z)"가 기본 표시 수단). 알림에는 버전·코드만 넣는다.
package notify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrUnavailable은 이 환경에 알림 수단이 없다는 뜻이다.
var ErrUnavailable = errors.New("notify: unavailable")

// powerShellToast는 Windows 10+ 토스트를 띄운다. 입력은 $env:AO_NOTIFY_TITLE·$env:AO_NOTIFY_BODY뿐이다.
const powerShellToast = `$ErrorActionPreference='Stop'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$x = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$t = $x.GetElementsByTagName('text')
$t.Item(0).AppendChild($x.CreateTextNode($env:AO_NOTIFY_TITLE)) > $null
$t.Item(1).AppendChild($x.CreateTextNode($env:AO_NOTIFY_BODY)) > $null
$id = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($id).Show([Windows.UI.Notifications.ToastNotification]::new($x))`

// Command는 OS별 알림 명령과 추가 환경 변수다. lookPath가 nil이면 exec.LookPath.
func Command(goos, title, body string, lookPath func(string) (string, error)) ([]string, []string, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	title, body = clean(title, 80), clean(body, 240)
	switch goos {
	case "darwin":
		return []string{"/usr/bin/osascript", "-e", "on run argv", "-e",
			"display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body}, nil, nil
	case "windows":
		return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", powerShellToast},
			[]string{"AO_NOTIFY_TITLE=" + title, "AO_NOTIFY_BODY=" + body}, nil
	default:
		p, err := lookPath("notify-send")
		if err != nil {
			return nil, nil, ErrUnavailable
		}
		return []string{p, "--app-name=Agent Office Collector", "--icon=agent-office-collector", "--", title, body}, nil, nil
	}
}

// clean은 제어 문자를 공백으로 바꾸고 길이를 자른다.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

// Send는 알림을 띄우고 최대 20초 기다린다.
func Send(title, body string) error {
	argv, env, err := Command(runtime.GOOS, title, body, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	hide(cmd)
	return cmd.Run()
}
