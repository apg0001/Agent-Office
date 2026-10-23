// Package service는 OS별 사용자 수준 자동 시작 정의를 생성·설치·제거한다(#34).
//
//   - Linux: systemd 사용자 unit(`~/.config/systemd/user/agent-office-collector.service`)
//   - macOS: launchd LaunchAgent(`~/Library/LaunchAgents/io.github.apg0001.agent-office.collector.plist`)
//   - Windows: 로그온 시 실행하는 작업 스케줄러 XML(상태 디렉터리에 저장, `schtasks /Create /XML`로 등록)
//
// 생성(Generate)은 순수 함수라 어느 OS에서나 테스트할 수 있다. 설치는 소유 표시(Marker)가 없는
// 기존 파일·작업을 덮어쓰지 않고, 제거는 소유 표시가 있는 것만 지운다. 관리자 권한을 쓰지 않는다.
package service

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode/utf16"

	"github.com/apg0001/Agent-Office/collector/core/internal/fsutil"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
)

const (
	// Marker는 이 도구가 만든 정의임을 나타내는 소유 표시다.
	Marker = "agent-office-collector:managed-service:v1"
	// UnitName은 systemd 사용자 unit 이름이다.
	UnitName = "agent-office-collector.service"
	// LaunchdLabel은 launchd Label이다.
	LaunchdLabel = "io.github.apg0001.agent-office.collector"
	// TaskName은 Windows 작업 스케줄러 경로다.
	TaskName = `\AgentOffice\Collector`
	// DocURL은 정의 파일에 남기는 설명 문서 위치다.
	DocURL = "https://github.com/apg0001/Agent-Office/blob/develop/docs/installation/collector-cli.md"
)

var (
	// ErrForeign은 같은 위치에 이 도구가 만들지 않은 파일·작업이 있을 때다. 건드리지 않는다.
	ErrForeign = errors.New("service: existing definition is not owned by agent-office-collector")
	// ErrNotInstalled는 제거할 정의가 없을 때다.
	ErrNotInstalled = errors.New("service: not installed")
	// ErrUnsupportedOS는 정의를 만들 수 없는 OS다.
	ErrUnsupportedOS = errors.New("service: unsupported operating system")
	// ErrInvalidExecutable은 실행 파일 경로가 절대 경로가 아니거나 제어 문자를 포함할 때다.
	ErrInvalidExecutable = errors.New("service: executable path must be absolute without control characters")
	// ErrCommandFailed는 활성화 명령이 실패했을 때다(출력은 담지 않음).
	ErrCommandFailed = errors.New("service: activation command failed")
)

// Params는 정의 생성 입력이다.
type Params struct {
	GOOS string
	// Exe는 Collector 실행 파일 절대 경로다.
	Exe string
	// Path는 정의 파일을 쓸 위치다(Location으로 계산).
	Path string
	// UID는 macOS launchctl 도메인(gui/<uid>)용이다.
	UID int
	// WindowsUser는 `DOMAIN\user` 형식 사용자다. 비어 있으면 작업 XML에서 UserId를 생략한다.
	WindowsUser string
}

// Command는 활성화·비활성화 명령 하나다.
type Command struct {
	Args        []string
	IgnoreError bool
}

// Definition은 생성된 정의다.
type Definition struct {
	GOOS string
	Path string
	// Text는 사람이 읽는 정의 본문이다(골든 테스트 대상).
	Text string
	// Content는 디스크에 쓸 바이트다(Windows는 UTF-16LE BOM).
	Content []byte
	Perm    fs.FileMode
	// Activate는 설치 후 실행할 명령, Deactivate는 제거 전, PostRemove는 제거 후 명령이다.
	Activate   []Command
	Deactivate []Command
	PostRemove []Command
}

// Location은 정의 파일 위치다.
func Location(goos string, env paths.Env, dirs paths.Dirs) (string, error) {
	switch goos {
	case "linux":
		base := env("XDG_CONFIG_HOME")
		if !strings.HasPrefix(base, "/") {
			base = paths.Join(goos, dirs.Home, ".config")
		}
		return paths.Join(goos, base, "systemd", "user", UnitName), nil
	case "darwin":
		return paths.Join(goos, dirs.Home, "Library", "LaunchAgents", LaunchdLabel+".plist"), nil
	case "windows":
		return paths.Join(goos, dirs.State, "service", "agent-office-collector-task.xml"), nil
	default:
		return "", ErrUnsupportedOS
	}
}

// Generate는 정의를 만든다. 파일을 쓰지 않는다.
func Generate(p Params) (Definition, error) {
	if !validExe(p.GOOS, p.Exe) {
		return Definition{}, ErrInvalidExecutable
	}
	switch p.GOOS {
	case "linux":
		text := systemdUnit(p.Exe)
		return Definition{
			GOOS: p.GOOS, Path: p.Path, Text: text, Content: []byte(text), Perm: 0o644,
			Activate: []Command{
				{Args: []string{"systemctl", "--user", "daemon-reload"}},
				{Args: []string{"systemctl", "--user", "enable", "--now", UnitName}},
			},
			Deactivate: []Command{{Args: []string{"systemctl", "--user", "disable", "--now", UnitName}, IgnoreError: true}},
			PostRemove: []Command{{Args: []string{"systemctl", "--user", "daemon-reload"}, IgnoreError: true}},
		}, nil
	case "darwin":
		text := launchdPlist(p.Exe)
		domain := fmt.Sprintf("gui/%d", p.UID)
		return Definition{
			GOOS: p.GOOS, Path: p.Path, Text: text, Content: []byte(text), Perm: 0o644,
			Activate: []Command{
				{Args: []string{"launchctl", "bootout", domain + "/" + LaunchdLabel}, IgnoreError: true},
				{Args: []string{"launchctl", "bootstrap", domain, p.Path}},
			},
			Deactivate: []Command{{Args: []string{"launchctl", "bootout", domain + "/" + LaunchdLabel}, IgnoreError: true}},
		}, nil
	case "windows":
		text := taskXML(p.Exe, p.WindowsUser)
		return Definition{
			GOOS: p.GOOS, Path: p.Path, Text: text, Content: utf16LE(crlf(text)), Perm: 0o600,
			Activate: []Command{
				{Args: []string{"schtasks", "/Create", "/TN", TaskName, "/XML", p.Path, "/F"}},
				{Args: []string{"schtasks", "/Run", "/TN", TaskName}, IgnoreError: true},
			},
			Deactivate: []Command{
				{Args: []string{"schtasks", "/End", "/TN", TaskName}, IgnoreError: true},
				{Args: []string{"schtasks", "/Delete", "/TN", TaskName, "/F"}},
			},
		}, nil
	default:
		return Definition{}, ErrUnsupportedOS
	}
}

func validExe(goos, exe string) bool {
	if exe == "" || strings.ContainsAny(exe, "\x00\r\n\t") {
		return false
	}
	if goos == "windows" {
		return len(exe) >= 3 && exe[1] == ':' && (exe[2] == '\\' || exe[2] == '/')
	}
	return strings.HasPrefix(exe, "/")
}

func systemdQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(s) + `"`
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func systemdUnit(exe string) string {
	return `# Agent Office Collector user service.
# ` + Marker + `
# Only files carrying the marker above are replaced or removed by
# "agent-office-collector service install|uninstall".
[Unit]
Description=Agent Office Collector
Documentation=` + DocURL + `

[Service]
Type=simple
ExecStart=` + systemdQuote(exe) + ` run
Restart=on-failure
RestartSec=30
TimeoutStopSec=20
NoNewPrivileges=yes

[Install]
WantedBy=default.target
`
}

func launchdPlist(exe string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- ` + Marker + ` -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + LaunchdLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(exe) + `</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`
}

func taskXML(exe, user string) string {
	userID := ""
	if user != "" {
		userID = "\n      <UserId>" + xmlEscape(user) + "</UserId>"
	}
	text := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>agent-office-collector</Author>
    <Description>Agent Office Collector (` + Marker + `)</Description>
    <URI>` + TaskName + `</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>` + userID + `
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">` + userID + `
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(exe) + `</Command>
      <Arguments>run</Arguments>
    </Exec>
  </Actions>
</Task>
`
	return text
}

// crlf는 Windows 파일용 줄바꿈으로 바꾼다(골든 텍스트는 LF로 유지).
func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[2+2*i:], u)
	}
	return out
}

// Owned는 바이트(UTF-8 또는 UTF-16LE)에 소유 표시가 있는지 확인한다.
func Owned(data []byte) bool {
	if bytes.Contains(data, []byte(Marker)) {
		return true
	}
	return bytes.Contains(data, utf16LE(Marker)[2:])
}

// Runner는 외부 명령 실행 주입 지점이다. 출력은 소유 확인에만 쓰고 표시하지 않는다.
type Runner func(args ...string) ([]byte, error)

// Result는 설치·제거 결과다.
type Result struct {
	// Replaced는 이전에 이 도구가 만든 정의를 갱신했는지다.
	Replaced bool
	// Activated는 활성화 명령까지 실행했는지다.
	Activated bool
}

// Install은 정의를 쓴다. activate면 활성화 명령도 실행한다.
func Install(def Definition, activate bool, run Runner) (Result, error) {
	var res Result
	switch data, err := readExisting(def.Path); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return res, ErrForeign
	case !Owned(data):
		return res, ErrForeign
	default:
		res.Replaced = true
	}
	if activate && def.GOOS == "windows" {
		// 같은 이름의 작업이 이미 있으면 소유 표시가 있을 때만 /F로 교체한다.
		if out, err := run("schtasks", "/Query", "/TN", TaskName, "/XML"); err == nil && !Owned(out) {
			return res, ErrForeign
		}
	}
	if err := fsutil.WriteFileAtomic(def.Path, def.Content, def.Perm); err != nil {
		return res, err
	}
	if activate {
		if err := runAll(def.Activate, run); err != nil {
			return res, err
		}
		res.Activated = true
	}
	return res, nil
}

// Uninstall은 소유 표시가 있는 정의만 비활성화·삭제한다.
func Uninstall(def Definition, activate bool, run Runner) (Result, error) {
	var res Result
	data, err := readExisting(def.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return res, ErrNotInstalled
	}
	if err != nil || !Owned(data) {
		return res, ErrForeign
	}
	if activate {
		if def.GOOS == "windows" {
			out, err := run("schtasks", "/Query", "/TN", TaskName, "/XML")
			switch {
			case err != nil:
				// 작업이 이미 없다. 파일만 지운다.
			case !Owned(out):
				return res, ErrForeign
			default:
				if err := runAll(def.Deactivate, run); err != nil {
					return res, err
				}
			}
		} else if err := runAll(def.Deactivate, run); err != nil {
			return res, err
		}
		res.Activated = true
	}
	if err := os.Remove(def.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, errors.New("service: remove failed")
	}
	if activate {
		if err := runAll(def.PostRemove, run); err != nil {
			return res, err
		}
	}
	return res, nil
}

func readExisting(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return nil, ErrForeign
	}
	return os.ReadFile(path)
}

func runAll(cmds []Command, run Runner) error {
	for _, c := range cmds {
		if _, err := run(c.Args...); err != nil && !c.IgnoreError {
			return fmt.Errorf("%w: %s", ErrCommandFailed, c.Args[0])
		}
	}
	return nil
}
