// Package autostart는 로그인 시 트레이 도우미를 띄우는 사용자 수준 정의를 만들고 설치·제거한다(#43, #159).
//
// Core의 자동 시작(`agent-office-collector service install`, systemd·launchd·작업 스케줄러)과는
// 별개다. Core는 GUI 없이도 수집하고, 이 정의는 트레이 아이콘을 보여 주기 위한 트레이 도우미
// (`agent-office-collector-tray`, 없으면 개발 빌드의 GUI)만 띄운다. 파일 이름·Label·값 이름은 #43의
// GUI 자동 시작과 같아서 이전 항목을 그대로 갱신한다.
//
//   - Linux: XDG autostart `${XDG_CONFIG_HOME:-~/.config}/autostart/agent-office-collector-gui.desktop`
//   - macOS: LaunchAgent `~/Library/LaunchAgents/io.github.apg0001.agent-office.collector-gui.plist`
//     (다음 로그인부터 launchd가 읽음. 지금 바로 bootstrap하지 않음)
//   - Windows: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`의 `AgentOfficeCollectorGUI` 값
//
// 설치는 소유 표시(Marker)가 없는 기존 파일·값을 덮어쓰지 않고, 제거는 소유 표시가 있는 것만 지운다.
// 파일은 같은 디렉터리 임시 파일 → fsync → rename으로 원자적으로 쓴다. 관리자 권한을 쓰지 않는다.
package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Marker는 이 도구가 만든 정의임을 나타내는 소유 표시다.
	Marker = "agent-office-collector-gui:managed-autostart:v1"
	// Arg는 자동 시작으로 실행됐음을 알리는 GUI 인자다(트레이가 있으면 창 없이 시작).
	// Windows Run 값에는 주석을 넣을 수 없어 이 인자와 실행 파일 이름이 소유 표시 역할을 한다.
	Arg = "--autostart"
	// DesktopFile은 Linux XDG autostart 파일 이름이다.
	DesktopFile = "agent-office-collector-gui.desktop"
	// LaunchdLabel은 macOS LaunchAgent Label이다(Core의 ...collector와 다름).
	LaunchdLabel = "io.github.apg0001.agent-office.collector-gui"
	// RunValue는 Windows Run 키의 값 이름이다.
	RunValue = "AgentOfficeCollectorGUI"
	// RunKey는 표시용 레지스트리 위치다.
	RunKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	// ExeBase는 소유 판정에 쓰는 GUI 실행 파일 이름(확장자 제외)이다.
	ExeBase = "agent-office-collector-gui"
	// TrayExeBase는 트레이 도우미 실행 파일 이름(확장자 제외)이다(#159).
	TrayExeBase = "agent-office-collector-tray"
)

var (
	// ErrForeign은 같은 위치에 이 도구가 만들지 않은 정의가 있을 때다. 건드리지 않는다.
	ErrForeign = errors.New("autostart: existing entry is not owned by agent-office-collector-gui")
	// ErrNotInstalled는 제거할 정의가 없을 때다.
	ErrNotInstalled = errors.New("autostart: not installed")
	// ErrUnsupportedOS는 지원하지 않는 OS다.
	ErrUnsupportedOS = errors.New("autostart: unsupported operating system")
	// ErrInvalidExecutable은 GUI 실행 파일 경로가 절대 경로가 아니거나 제어 문자·임시 빌드 경로일 때다.
	ErrInvalidExecutable = errors.New("autostart: executable path must be an absolute, installed path")
	// ErrWriteFailed는 파일·레지스트리 쓰기 실패다(경로는 담지 않음).
	ErrWriteFailed = errors.New("autostart: write failed")
)

// Registry는 Windows Run 키 접근 주입 지점이다(테스트는 메모리 구현).
type Registry interface {
	Get(name string) (value string, ok bool, err error)
	Set(name, value string) error
	Delete(name string) error
}

// Params는 정의 생성 입력이다.
type Params struct {
	GOOS string
	// Exe는 GUI 실행 파일 절대 경로다.
	Exe string
	// Home은 사용자 홈이다(Linux·macOS 위치 계산).
	Home   string
	Getenv func(string) string
}

// Definition은 생성된 정의다.
type Definition struct {
	GOOS string
	// Path는 정의 파일 위치(Linux·macOS) 또는 표시용 레지스트리 위치(Windows)다.
	Path string
	// Content는 파일 본문 또는 Windows Run 값 데이터다.
	Content string
}

// Generate는 정의를 만든다. 파일을 쓰지 않는 순수 함수다.
func Generate(p Params) (Definition, error) {
	if !validExe(p.GOOS, p.Exe) {
		return Definition{}, ErrInvalidExecutable
	}
	getenv := p.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	switch p.GOOS {
	case "linux":
		if !strings.HasPrefix(p.Home, "/") {
			return Definition{}, ErrUnsupportedOS
		}
		base := getenv("XDG_CONFIG_HOME")
		if !strings.HasPrefix(base, "/") {
			base = p.Home + "/.config"
		}
		return Definition{GOOS: p.GOOS, Path: base + "/autostart/" + DesktopFile, Content: desktopEntry(p.Exe)}, nil
	case "darwin":
		if !strings.HasPrefix(p.Home, "/") {
			return Definition{}, ErrUnsupportedOS
		}
		return Definition{GOOS: p.GOOS, Path: p.Home + "/Library/LaunchAgents/" + LaunchdLabel + ".plist", Content: launchdPlist(p.Exe)}, nil
	case "windows":
		return Definition{GOOS: p.GOOS, Path: RunKey + `\` + RunValue, Content: `"` + p.Exe + `" ` + Arg}, nil
	default:
		return Definition{}, ErrUnsupportedOS
	}
}

func validExe(goos, exe string) bool {
	if exe == "" || strings.ContainsAny(exe, "\x00\r\n\t") {
		return false
	}
	// go run·go test 임시 실행 파일은 다음 로그인 때 없다.
	if s := filepath.ToSlash(exe); strings.Contains(s, "/go-build") {
		return false
	}
	if goos == "windows" {
		return len(exe) >= 3 && exe[1] == ':' && (exe[2] == '\\' || exe[2] == '/') && !strings.Contains(exe, `"`)
	}
	return strings.HasPrefix(exe, "/")
}

// desktopExecQuote는 Desktop Entry 명세의 Exec 인자 인용이다. 큰따옴표 안에서 `"` “ ` “ `$` `\`를
// 역슬래시로 이스케이프하고, 값 문자열 수준에서 다시 `\`를 두 배로, `%`를 `%%`로 바꾼다.
func desktopExecQuote(s string) string {
	inner := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(s)
	quoted := `"` + inner + `"`
	return strings.NewReplacer(`\`, `\\`, `%`, `%%`).Replace(quoted)
}

func desktopEntry(exe string) string {
	return `[Desktop Entry]
# ` + Marker + `
# Only files carrying the marker above are replaced or removed by Agent Office Collector GUI.
Type=Application
Name=Agent Office Collector
Comment=Agent Office Collector tray icon (collection runs in the separate Core service)
Exec=` + desktopExecQuote(exe) + ` ` + Arg + `
Terminal=false
NoDisplay=true
X-GNOME-Autostart-enabled=true
X-AgentOffice-Managed=` + Marker + `
`
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
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
		<string>` + Arg + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`
}

// Owned는 파일 본문 또는 Windows Run 값이 이 도구 소유인지다.
func Owned(goos, content string) bool {
	if goos != "windows" {
		return strings.Contains(content, Marker)
	}
	// "<드라이브:\...\agent-office-collector-(gui|tray).exe>" --autostart
	if !strings.HasPrefix(content, `"`) {
		return false
	}
	end := strings.Index(content[1:], `"`)
	if end < 0 {
		return false
	}
	exe, rest := content[1:1+end], content[2+end:]
	base := exe
	if i := strings.LastIndexAny(exe, `\/`); i >= 0 {
		base = exe[i+1:]
	}
	return rest == " "+Arg && (strings.EqualFold(base, ExeBase+".exe") || strings.EqualFold(base, TrayExeBase+".exe"))
}

// State는 installed / outdated(소유지만 실행 파일 위치가 다름) / absent / foreign이다.
func State(def Definition, reg Registry) string {
	cur, ok, err := read(def, reg)
	switch {
	case err != nil:
		return "foreign"
	case !ok:
		return "absent"
	case !Owned(def.GOOS, cur):
		return "foreign"
	case cur != def.Content:
		return "outdated"
	default:
		return "installed"
	}
}

func read(def Definition, reg Registry) (string, bool, error) {
	if def.GOOS == "windows" {
		if reg == nil {
			return "", false, ErrUnsupportedOS
		}
		return reg.Get(RunValue)
	}
	fi, err := os.Lstat(def.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 64<<10 {
		return "", true, ErrForeign
	}
	b, err := os.ReadFile(def.Path)
	if err != nil {
		return "", true, ErrForeign
	}
	return string(b), true, nil
}

// Install은 정의를 쓴다. 소유 표시가 없는 기존 항목이 있으면 ErrForeign이다. 갱신이면 true다.
func Install(def Definition, reg Registry) (bool, error) {
	cur, ok, err := read(def, reg)
	if err != nil {
		if errors.Is(err, ErrUnsupportedOS) {
			return false, err
		}
		return false, ErrForeign
	}
	if ok && !Owned(def.GOOS, cur) {
		return false, ErrForeign
	}
	if def.GOOS == "windows" {
		if err := reg.Set(RunValue, def.Content); err != nil {
			return false, ErrWriteFailed
		}
		return ok, nil
	}
	if err := writeAtomic(def.Path, []byte(def.Content)); err != nil {
		return false, ErrWriteFailed
	}
	return ok, nil
}

// Uninstall은 소유 표시가 있는 정의만 지운다.
func Uninstall(def Definition, reg Registry) error {
	cur, ok, err := read(def, reg)
	if err != nil {
		if errors.Is(err, ErrUnsupportedOS) {
			return err
		}
		return ErrForeign
	}
	if !ok {
		return ErrNotInstalled
	}
	if !Owned(def.GOOS, cur) {
		return ErrForeign
	}
	if def.GOOS == "windows" {
		if err := reg.Delete(RunValue); err != nil {
			return ErrWriteFailed
		}
		return nil
	}
	if err := os.Remove(def.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrWriteFailed
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Memory는 테스트용 Registry다.
type Memory struct {
	Values map[string]string
	Fail   bool
}

func (m *Memory) Get(name string) (string, bool, error) {
	if m.Fail {
		return "", false, errors.New("registry failure")
	}
	v, ok := m.Values[name]
	return v, ok, nil
}

func (m *Memory) Set(name, value string) error {
	if m.Fail {
		return errors.New("registry failure")
	}
	if m.Values == nil {
		m.Values = map[string]string{}
	}
	m.Values[name] = value
	return nil
}

func (m *Memory) Delete(name string) error {
	if m.Fail {
		return errors.New("registry failure")
	}
	delete(m.Values, name)
	return nil
}
