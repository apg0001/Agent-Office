// Package app은 Wails 창에 바인딩되는 GUI 백엔드다(#40, #41). 화면 없이 단위 테스트한다.
//
// 모든 동작은 Core 명령(corecli)으로 처리한다. 이 패키지는 장치 토큰을 읽거나 저장하거나
// 출력하지 않으며, 화면에 보내는 Core 출력에서도 토큰 형식 문자열을 가린다.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/autostart"
	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

// DevicesPath는 웹에서 PC 등록 코드를 발급하는 화면이다(apps/web `/devices`).
const DevicesPath = "/devices"

// Options는 App 의존성이다. 0 값은 실제 OS 값을 쓴다.
type Options struct {
	Getenv        func(string) string
	UserConfigDir func() (string, error)
	Executable    func() (string, error)
	Hostname      func() (string, error)
	GOOS          string
	// OpenURL은 기본 브라우저로 주소를 연다(Wails runtime.BrowserOpenURL).
	OpenURL func(ctx context.Context, url string)
	// Runner를 주면 Core 위치 탐색을 건너뛴다(테스트).
	Runner *corecli.Runner
	// UserHomeDir는 GUI 자동 시작 정의 위치 계산용이다.
	UserHomeDir func() (string, error)
	// Registry는 Windows Run 키 접근자다(nil이면 autostart.SystemRegistry).
	Registry autostart.Registry
}

// App은 Wails에 바인딩되는 구조체다. 공개 메서드가 프런트엔드에 노출된다.
type App struct {
	opts Options
	mu   sync.Mutex
	ctx  context.Context
	// hookPreviewed는 이번 실행에서 Hook 설치 미리보기를 보여 준 도구다(명시적 동의 흐름).
	hookPreviewed map[string]bool
}

// New는 App을 만든다.
func New(o Options) *App {
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.UserConfigDir == nil {
		o.UserConfigDir = os.UserConfigDir
	}
	if o.Executable == nil {
		o.Executable = os.Executable
	}
	if o.Hostname == nil {
		o.Hostname = os.Hostname
	}
	if o.GOOS == "" {
		o.GOOS = goos
	}
	if o.UserHomeDir == nil {
		o.UserHomeDir = os.UserHomeDir
	}
	if o.Registry == nil {
		o.Registry = autostart.SystemRegistry()
	}
	return &App{opts: o, ctx: context.Background(), hookPreviewed: map[string]bool{}}
}

// OnStartup은 Wails OnStartup 콜백을 돌려준다(메서드가 아니어서 프런트엔드에 바인딩되지 않음).
func OnStartup(a *App) func(context.Context) {
	return func(ctx context.Context) {
		a.mu.Lock()
		a.ctx = ctx
		a.mu.Unlock()
	}
}

func (a *App) context() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

func (a *App) runner() (corecli.Runner, error) {
	if a.opts.Runner != nil {
		return *a.opts.Runner, nil
	}
	exe, _ := a.opts.Executable()
	bin, err := corecli.Locate(a.opts.Getenv, exe, a.opts.GOOS)
	if err != nil {
		return corecli.Runner{}, err
	}
	return corecli.Runner{Bin: bin}, nil
}

// Result는 동작 결과다. Code는 고정 코드, Message는 한국어 안내, Output은 Core 출력(가림 처리)이다.
type Result struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
}

func fail(code string) Result { return Result{Code: code, Message: messageFor(code)} }

func okResult(code, output string) Result {
	return Result{OK: true, Code: code, Message: messageFor(code), Output: redact(output)}
}

func errCode(err error) string {
	switch {
	case errors.Is(err, corecli.ErrCoreMissing):
		return "core_missing"
	case errors.Is(err, corecli.ErrTimeout):
		return "core_timeout"
	case errors.Is(err, corecli.ErrStart):
		return "core_start_failed"
	case errors.Is(err, corecli.ErrMalformed):
		return "core_output_invalid"
	case errors.Is(err, corecli.ErrInvalidURL):
		return "invalid_server_url"
	case errors.Is(err, corecli.ErrInsecureURL):
		return "insecure_server_url"
	case errors.Is(err, corecli.ErrInvalidCode):
		return "invalid_registration_code"
	case errors.Is(err, corecli.ErrInvalidName):
		return "invalid_device_name"
	default:
		return "unexpected"
	}
}

// tokenLike는 서버 장치 토큰 형식(`aod_` + base64url)이다. Core는 토큰을 출력하지 않지만 한 번 더 가린다.
var tokenLike = regexp.MustCompile(`aod_[A-Za-z0-9_-]{8,}`)

func redact(s string) string {
	return tokenLike.ReplaceAllString(strings.TrimSpace(s), "aod_[가림]")
}

// --- 계정·서버 ------------------------------------------------------------------

// Settings는 서버·PC 이름 초안과 Core 위치 확인 결과다.
type Settings struct {
	ServerURL  string `json:"server_url"`
	DeviceName string `json:"device_name"`
	CoreFound  bool   `json:"core_found"`
	// PrefsState는 ok / invalid(손상 파일은 무시하고 빈 값) / unavailable이다.
	PrefsState string `json:"prefs_state"`
}

func (a *App) prefsPath() (string, error) {
	return prefs.Path(a.opts.Getenv, a.opts.UserConfigDir)
}

// Settings는 저장된 GUI 설정을 읽는다. PC 이름이 없으면 호스트 이름을 제안한다.
func (a *App) Settings() Settings {
	s := Settings{PrefsState: "ok"}
	if path, err := a.prefsPath(); err != nil {
		s.PrefsState = "unavailable"
	} else if p, err := prefs.Load(path); err != nil {
		s.PrefsState = "invalid"
	} else {
		s.ServerURL, s.DeviceName = p.ServerURL, p.DeviceName
	}
	if s.DeviceName == "" {
		if h, err := a.opts.Hostname(); err == nil {
			if n, err := corecli.NormalizeName(h); err == nil {
				s.DeviceName = n
			}
		}
	}
	_, err := a.runner()
	s.CoreFound = err == nil
	return s
}

// SaveServer는 서버 주소를 검증해 GUI 설정에 저장한다.
func (a *App) SaveServer(raw string) Result {
	u, err := corecli.NormalizeServerURL(raw)
	if err != nil {
		return fail(errCode(err))
	}
	path, err := a.prefsPath()
	if err != nil {
		return fail("prefs_unavailable")
	}
	p, _ := prefs.Load(path)
	p.ServerURL = u
	if err := prefs.Save(path, p); err != nil {
		return fail("prefs_write_failed")
	}
	return okResult("server_saved", "")
}

// RegistrationPageURL은 웹 PC 등록 코드 발급 화면 주소다.
func (a *App) RegistrationPageURL() (string, error) {
	path, err := a.prefsPath()
	if err != nil {
		return "", err
	}
	p, _ := prefs.Load(path)
	if p.ServerURL == "" {
		return "", errors.New("server_not_set")
	}
	u, err := corecli.NormalizeServerURL(p.ServerURL)
	if err != nil {
		return "", err
	}
	return u + DevicesPath, nil
}

// OfficeURL은 웹 사무실(서버 루트) 주소다. 트레이 "사무실 열기"가 쓴다(#159).
func (a *App) OfficeURL() (string, error) {
	path, err := a.prefsPath()
	if err != nil {
		return "", err
	}
	p, _ := prefs.Load(path)
	if p.ServerURL == "" {
		return "", errors.New("server_not_set")
	}
	u, err := corecli.NormalizeServerURL(p.ServerURL)
	if err != nil {
		return "", err
	}
	return u + "/", nil
}

// OpenRegistrationPage는 기본 브라우저에서 웹 로그인 뒤 등록 코드 발급 화면을 연다.
// 로그인은 브라우저에서만 하며 GUI는 계정 비밀번호를 받지 않는다.
func (a *App) OpenRegistrationPage() Result {
	u, err := a.RegistrationPageURL()
	if err != nil {
		if err.Error() == "server_not_set" {
			return fail("server_not_set")
		}
		return fail(errCode(err))
	}
	if a.opts.OpenURL == nil {
		return fail("browser_unavailable")
	}
	a.opts.OpenURL(a.context(), u)
	return okResult("browser_opened", "")
}

// --- 기기 등록 --------------------------------------------------------------------

// Register는 웹에서 받은 일회용 코드로 Core `register`를 실행한다. 코드는 표준 입력으로만 넘기고
// 저장하지 않는다. 성공하면 PC 이름을 GUI 설정에 기억한다.
func (a *App) Register(code, name string, force bool) Result {
	path, err := a.prefsPath()
	if err != nil {
		return fail("prefs_unavailable")
	}
	p, _ := prefs.Load(path)
	if p.ServerURL == "" {
		return fail("server_not_set")
	}
	r, err := a.runner()
	if err != nil {
		return fail(errCode(err))
	}
	res, err := r.Register(a.context(), p.ServerURL, code, name, force)
	if err != nil {
		return fail(errCode(err))
	}
	if res.Exit != 0 {
		// Core 오류 문구는 토큰·코드·URL을 담지 않는다. 화면 표시 전 한 번 더 가린다.
		out := fail("register_failed")
		out.Output = redact(res.Stderr)
		if strings.Contains(res.Stderr, "--force") {
			out.Code, out.Message = "already_registered", messageFor("already_registered")
		}
		return out
	}
	n, _ := corecli.NormalizeName(name)
	p.DeviceName = n
	_ = prefs.Save(path, p)
	// Core 출력(장치 ID 포함)은 보여 주지 않는다.
	return okResult("registered", "")
}

// --- 수집 상태·일시중지·진단 ---------------------------------------------------------

// StatusView는 상태 화면 데이터다.
type StatusView struct {
	OK     bool            `json:"ok"`
	Code   string          `json:"code,omitempty"`
	Report *corecli.Report `json:"report,omitempty"`
}

// Status는 Core `status --json`이다.
func (a *App) Status() StatusView {
	r, err := a.runner()
	if err != nil {
		return StatusView{Code: errCode(err)}
	}
	rep, err := r.Status(a.context())
	if err != nil {
		return StatusView{Code: errCode(err)}
	}
	return StatusView{OK: true, Report: &rep}
}

// Diagnose는 Core `diagnose --json`이다. offline이면 서버 도달 확인을 건너뛴다.
func (a *App) Diagnose(offline bool) StatusView {
	r, err := a.runner()
	if err != nil {
		return StatusView{Code: errCode(err)}
	}
	rep, err := r.Diagnose(a.context(), offline)
	if err != nil {
		return StatusView{Code: errCode(err)}
	}
	return StatusView{OK: true, Report: &rep}
}

// Pause는 수집을 일시중지한다(실행 중인 Core가 몇 초 안에 반영).
func (a *App) Pause() Result { return a.simple("paused", "pause_failed", "pause") }

// Resume은 수집을 재개한다. 일시중지 동안 쓰인 transcript 줄은 수집하지 않는다(Core 규칙).
func (a *App) Resume() Result { return a.simple("resumed", "resume_failed", "resume") }

func (a *App) simple(okCode, failCode string, args ...string) Result {
	r, err := a.runner()
	if err != nil {
		return fail(errCode(err))
	}
	res, err := r.Run(a.context(), "", args...)
	if err != nil {
		return fail(errCode(err))
	}
	if res.Exit != 0 {
		out := fail(failCode)
		out.Output = redact(res.Stderr)
		return out
	}
	return okResult(okCode, "")
}

// updateTimeout은 `update check`(manifest 내려받기·서명 확인) 상한이다. Core 내부 상한(2분)보다 조금 길다.
const updateTimeout = 150 * time.Second

// UpdateCheck는 Core `update check`로 새 버전이 있는지만 확인한다(적용하지 않음, #159 트레이).
// 채널은 현재 Core 버전이 SemVer 사전 배포(`-beta.N` 등)면 beta, 아니면 stable이다.
// Output은 Core가 출력한 첫 줄(버전·채널만 담음)이다.
func (a *App) UpdateCheck() Result {
	channel := a.updateChannel()
	r, err := a.runner()
	if err != nil {
		return fail(errCode(err))
	}
	r.Timeout = updateTimeout
	res, err := r.Run(a.context(), "", "update", "check", "--channel", channel)
	if err != nil {
		return fail(errCode(err))
	}
	if res.Exit != 0 {
		out := fail("update_check_failed")
		out.Output = firstLine(redact(res.Stderr))
		return out
	}
	line := firstLine(res.Stdout)
	if strings.Contains(line, "새 버전") {
		return okResult("update_available", line)
	}
	return okResult("update_latest", line)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120])
	}
	return strings.TrimSpace(s)
}

// --- 도구 탐지·Hook ---------------------------------------------------------------

// ToolsView는 도구 탐지 화면 데이터다.
type ToolsView struct {
	OK    bool                 `json:"ok"`
	Code  string               `json:"code,omitempty"`
	OS    string               `json:"os,omitempty"`
	Tools []corecli.ToolReport `json:"tools,omitempty"`
}

// Tools는 Core `hooks status --json`으로 Claude·Codex 설치 여부·버전·검증 수준·Hook 상태를 읽는다.
func (a *App) Tools() ToolsView {
	r, err := a.runner()
	if err != nil {
		return ToolsView{Code: errCode(err)}
	}
	t, err := r.Tools(a.context())
	if err != nil {
		return ToolsView{Code: errCode(err)}
	}
	return ToolsView{OK: true, OS: t.OS, Tools: t.Tools}
}

// hookTools는 Core `hooks install`이 자동 설치를 지원하는 도구다(Codex는 #170부터).
var hookTools = map[string]bool{"claude": true, "codex": true}

// HookPreview는 도구(claude·codex)의 Hook 설치 미리보기(`hooks install`, `--yes` 없음)다. 파일을 바꾸지 않는다.
func (a *App) HookPreview(tool string) Result {
	if !hookTools[tool] {
		return fail("invalid_tool")
	}
	res := a.output("hook_preview", "hook_preview_failed", "hooks", "install", "--tool", tool)
	if res.OK {
		a.mu.Lock()
		a.hookPreviewed[tool] = true
		a.mu.Unlock()
	}
	return res
}

// HookInstall은 같은 도구의 미리보기를 본 뒤 사용자가 승인했을 때만 `hooks install --yes`를 실행한다.
// Codex는 설치 뒤 Codex 안에서 사용자가 Hook을 신뢰해야 실행된다(Collector는 신뢰 상태를 쓰지 않음).
func (a *App) HookInstall(tool string, confirmed bool) Result {
	if !hookTools[tool] {
		return fail("invalid_tool")
	}
	a.mu.Lock()
	previewed := a.hookPreviewed[tool]
	a.mu.Unlock()
	if !confirmed {
		return fail("confirmation_required")
	}
	if !previewed {
		return fail("preview_required")
	}
	done := "hook_installed"
	if tool == "codex" {
		done = "codex_hook_installed"
	}
	res := a.output(done, "hook_install_failed", "hooks", "install", "--tool", tool, "--yes")
	a.mu.Lock()
	delete(a.hookPreviewed, tool)
	a.mu.Unlock()
	return res
}

// HookUninstall은 승인 시 그 도구의 Agent Office 소유 Hook만 제거한다.
func (a *App) HookUninstall(tool string, confirmed bool) Result {
	if !hookTools[tool] {
		return fail("invalid_tool")
	}
	if !confirmed {
		return fail("confirmation_required")
	}
	return a.output("hook_uninstalled", "hook_uninstall_failed", "hooks", "uninstall", "--tool", tool, "--yes")
}

// --- 자동 시작 ---------------------------------------------------------------------

// ServicePreview는 자동 시작 정의 미리보기(`service print`)다.
func (a *App) ServicePreview() Result {
	return a.output("service_preview", "service_preview_failed", "service", "print")
}

// ServiceInstall은 승인 시 자동 시작 정의를 설치한다. activate면 OS 서비스 관리자 명령까지 실행한다.
func (a *App) ServiceInstall(activate, confirmed bool) Result {
	if !confirmed {
		return fail("confirmation_required")
	}
	args := []string{"service", "install"}
	if activate {
		args = append(args, "--activate")
	}
	return a.outputT(serviceTimeout, "service_installed", "service_install_failed", args...)
}

// output은 Core 출력을 보여 주는 동작이다. 실패면 stderr를, 성공이면 stdout을 가림 처리해 담는다.
func (a *App) output(okCode, failCode string, args ...string) Result {
	return a.outputT(0, okCode, failCode, args...)
}

// serviceTimeout은 `service install --activate`(systemctl·launchctl·schtasks 실행) 상한이다.
const serviceTimeout = 90 * time.Second

func (a *App) outputT(timeout time.Duration, okCode, failCode string, args ...string) Result {
	r, err := a.runner()
	if err != nil {
		return fail(errCode(err))
	}
	if timeout > 0 {
		r.Timeout = timeout
	}
	res, err := r.Run(a.context(), "", args...)
	if err != nil {
		return fail(errCode(err))
	}
	if res.Exit != 0 {
		out := fail(failCode)
		out.Output = redact(strings.TrimSpace(res.Stdout + "\n" + res.Stderr))
		return out
	}
	return okResult(okCode, res.Stdout)
}

// --- 로그아웃(#41) ------------------------------------------------------------------

// Logout은 승인 시 Core `logout`을 실행한다. Core가 서버 `/devices/revoke`로 이 장치를 폐기하고
// 보안 저장소·설정 파일의 장치 토큰을 지운다. GUI는 토큰을 다루지 않는다. localOnly면 서버 요청 없이
// 로컬만 지운다(웹 /devices에서 따로 폐기해야 함).
func (a *App) Logout(localOnly, confirmed bool) Result {
	if !confirmed {
		return fail("confirmation_required")
	}
	r, err := a.runner()
	if err != nil {
		return fail(errCode(err))
	}
	res, err := r.Logout(a.context(), localOnly)
	if err != nil {
		return fail(errCode(err))
	}
	if res.Exit != 0 {
		out := fail("logout_failed")
		out.Output = redact(res.Stderr)
		return out
	}
	code := "logged_out"
	if localOnly {
		code = "logged_out_local"
	}
	return okResult(code, res.Stdout)
}

// --- GUI 자동 시작(#43) -------------------------------------------------------------

func (a *App) guiExecutable() (string, error) {
	exe, err := a.opts.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Abs(exe)
}

// autostartDef는 로그인 시 띄울 실행 파일 정의다. 설치 배치에 트레이 도우미가 있으면 트레이를
// 띄우고(#159, 창은 트레이 "설정"으로 연다), 없으면(개발 빌드) GUI를 띄운다.
func (a *App) autostartDef() (autostart.Definition, error) {
	exe, err := a.guiExecutable()
	if err != nil {
		return autostart.Definition{}, autostart.ErrInvalidExecutable
	}
	if tray, ok := corecli.FindSibling(exe, a.opts.GOOS, corecli.TrayName); ok {
		exe = tray
	}
	home, _ := a.opts.UserHomeDir()
	return autostart.Generate(autostart.Params{GOOS: a.opts.GOOS, Exe: exe, Home: home, Getenv: a.opts.Getenv})
}

func autostartErrCode(err error) string {
	switch {
	case errors.Is(err, autostart.ErrForeign):
		return "gui_autostart_foreign"
	case errors.Is(err, autostart.ErrInvalidExecutable):
		return "gui_autostart_invalid_exe"
	case errors.Is(err, autostart.ErrUnsupportedOS):
		return "gui_autostart_unsupported"
	case errors.Is(err, autostart.ErrNotInstalled):
		return "gui_autostart_absent"
	default:
		return "gui_autostart_failed"
	}
}

// GUIAutostartView는 GUI 자동 시작 상태다. State는 installed / outdated / absent / foreign / unsupported다.
type GUIAutostartView struct {
	OK    bool   `json:"ok"`
	State string `json:"state"`
	// Location은 정의 위치의 기호 표기다(홈은 ~, 실제 경로 원문 아님).
	Location string `json:"location,omitempty"`
}

func (a *App) displayLocation(p string) string {
	if home, err := a.opts.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// GUIAutostartStatus는 로그인 시 GUI(트레이) 자동 시작 정의 상태다. Core 자동 시작과 별개다.
func (a *App) GUIAutostartStatus() GUIAutostartView {
	def, err := a.autostartDef()
	if err != nil {
		return GUIAutostartView{State: "unsupported"}
	}
	st := autostart.State(def, a.opts.Registry)
	if def.GOOS == "windows" && a.opts.Registry == nil {
		st = "unsupported"
	}
	return GUIAutostartView{OK: true, State: st, Location: a.displayLocation(def.Path)}
}

// GUIAutostartInstall은 승인 시 GUI 자동 시작 정의를 설치·갱신한다(소유하지 않은 항목은 덮어쓰지 않음).
func (a *App) GUIAutostartInstall(confirmed bool) Result {
	if !confirmed {
		return fail("confirmation_required")
	}
	def, err := a.autostartDef()
	if err != nil {
		return fail(autostartErrCode(err))
	}
	if _, err := autostart.Install(def, a.opts.Registry); err != nil {
		return fail(autostartErrCode(err))
	}
	return okResult("gui_autostart_installed", a.displayLocation(def.Path))
}

// GUIAutostartUninstall은 승인 시 이 도구가 만든 GUI 자동 시작 정의만 지운다.
func (a *App) GUIAutostartUninstall(confirmed bool) Result {
	if !confirmed {
		return fail("confirmation_required")
	}
	def, err := a.autostartDef()
	if err != nil {
		return fail(autostartErrCode(err))
	}
	if err := autostart.Uninstall(def, a.opts.Registry); err != nil {
		if errors.Is(err, autostart.ErrNotInstalled) {
			return okResult("gui_autostart_absent", "")
		}
		return fail(autostartErrCode(err))
	}
	return okResult("gui_autostart_removed", "")
}
