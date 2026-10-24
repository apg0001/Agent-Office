// Package cli는 headless Collector 명령줄이다(#34). GUI(#40~#44)는 이 명령들을 감싼다.
//
//	agent-office-collector register --server URL --code XXXX-XXXX|--code-stdin --name PC [--force]
//	agent-office-collector run
//	agent-office-collector hook claude|codex [--product P] [--tool-version V] [--mode M]
//	agent-office-collector pause | resume
//	agent-office-collector status [--json]
//	agent-office-collector diagnose [--json] [--offline]
//	agent-office-collector service install|uninstall [--activate]
//	agent-office-collector service print [--os linux|darwin|windows] [--exe PATH]
//	agent-office-collector hooks install|uninstall [--tool claude] [--yes] [--scope user]
//	agent-office-collector hooks status [--tool claude|codex] [--json]
//	agent-office-collector hooks print [--tool claude|codex] [--os linux|darwin|windows] [--exe PATH]
//	agent-office-collector version
//
// 어떤 명령도 장치 토큰·등록 코드·로컬 경로 원문·이벤트 내용을 출력하지 않는다.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/service"
)

// 종료 코드.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Env는 명령 실행 환경(주입 지점)이다. 0 값 필드는 실제 OS 값을 쓴다.
type Env struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Now     func() time.Time
	GOOS    string
	// Dirs가 nil이면 paths.Default.
	Dirs func() (paths.Dirs, error)
	// HTTP는 register·diagnose용 클라이언트다.
	HTTP *http.Client
	// Executable은 서비스 정의에 넣을 실행 파일 경로다.
	Executable func() (string, error)
	// Command는 서비스 활성화 명령 실행기다.
	Command service.Runner
	// Context는 run의 수명이다(신호 처리는 main이 한다).
	Context context.Context
	// RunOptions는 run 설정 조정(테스트)이다.
	RunOptions func(*runner.Options)
	// HookTimeout은 hook stdin 읽기 상한이다.
	HookTimeout time.Duration
	// ToolVersion은 `<도구> --version`에서 버전 문자열만 돌려준다(hooks 명령).
	ToolVersion func(tool string) (string, error)
}

func (e *Env) defaults() {
	if e.Version == "" {
		e.Version = "0.0.0-dev"
	}
	if e.Stdin == nil {
		e.Stdin = os.Stdin
	}
	if e.Stdout == nil {
		e.Stdout = os.Stdout
	}
	if e.Stderr == nil {
		e.Stderr = os.Stderr
	}
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.GOOS == "" {
		e.GOOS = runtime.GOOS
	}
	if e.Dirs == nil {
		e.Dirs = paths.Default
	}
	if e.HTTP == nil {
		e.HTTP = &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if e.Executable == nil {
		e.Executable = func() (string, error) {
			p, err := os.Executable()
			if err != nil {
				return "", err
			}
			if r, err := filepath.EvalSymlinks(p); err == nil {
				p = r
			}
			return filepath.Abs(p)
		}
	}
	if e.Command == nil {
		e.Command = execCommand
	}
	if e.Context == nil {
		e.Context = context.Background()
	}
	if e.ToolVersion == nil {
		e.ToolVersion = detectToolVersion
	}
	if e.HookTimeout <= 0 {
		e.HookTimeout = 2 * time.Second
	}
}

func execCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
}

// Main은 명령을 실행하고 종료 코드를 돌려준다.
func Main(args []string, env Env) int {
	env.defaults()
	if len(args) == 0 {
		usage(env.Stderr)
		return ExitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "hook":
		// Hook은 어떤 경우에도 0으로 빨리 끝난다(fail-open). 출력도 하지 않는다.
		return runHook(rest, env)
	case "version", "--version":
		fmt.Fprintf(env.Stdout, "agent-office-collector %s (%s/%s)\n", env.Version, env.GOOS, runtime.GOARCH)
		return ExitOK
	case "register":
		return cmdRegister(rest, env)
	case "run":
		return cmdRun(rest, env)
	case "pause", "resume":
		return cmdPause(cmd == "pause", rest, env)
	case "status":
		return cmdStatus(rest, env, false)
	case "diagnose":
		return cmdStatus(rest, env, true)
	case "service":
		return cmdService(rest, env)
	case "hooks":
		return cmdHooks(rest, env)
	case "update":
		return cmdUpdate(rest, env)
	case "help", "-h", "--help":
		usage(env.Stdout)
		return ExitOK
	default:
		usage(env.Stderr)
		return ExitUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `사용법: agent-office-collector <명령>

  register --server URL --code XXXX-XXXX|--code-stdin --name PC [--force]  장치 등록(토큰은 0600 설정 파일에 저장, 출력하지 않음)
  run                                                          수집·전송 실행(포그라운드, SIGINT/SIGTERM으로 종료)
  hook claude|codex                                            Hook stdin을 실행 중인 Collector에 전달(항상 종료 코드 0)
  pause | resume                                               수집·전송 일시중지/재개
  status [--json]                                              로컬 상태 요약
  diagnose [--json] [--offline]                                안전한 진단 정보(경로·토큰·내용 없음)
  service install|uninstall [--activate]                       OS 사용자 자동 시작 정의 설치/제거
  service print [--os linux|darwin|windows] [--exe PATH]       자동 시작 정의 미리보기
  hooks install|uninstall [--tool claude] [--yes] [--scope user]
                                                               Claude Code 사용자 설정에 Hook 추가/제거(미리보기 후 --yes로 적용, 백업)
  hooks status [--tool claude|codex] [--json]                           Hook 설치 상태와 도구 버전 검증 수준
  hooks print [--tool claude|codex] [--os OS] [--exe PATH]     Hook 항목 미리보기(Codex는 print 전용)
  update check|apply [--channel stable|beta] [--manifest-url URL] [--yes]
                                                               서명된 manifest로 새 버전 확인·적용(실패 시 되돌림)
  version
`)
}

func newFlags(name string, env Env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

func cmdRun(args []string, env Env) int {
	fs := newFlags("run", env)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	logger := log.New(env.Stderr, "agent-office-collector: ", log.LstdFlags)
	o := runner.Options{Dirs: dirs, Version: env.Version, GOOS: env.GOOS, Now: env.Now, Logf: logger.Printf}
	if env.RunOptions != nil {
		env.RunOptions(&o)
	}
	if err := runner.Run(env.Context, o); err != nil {
		if errors.Is(err, runner.ErrAlreadyRunning) {
			fmt.Fprintln(env.Stderr, "이미 다른 Collector가 실행 중입니다")
		} else {
			fmt.Fprintln(env.Stderr, "Collector 실행 실패")
		}
		return ExitError
	}
	return ExitOK
}

func cmdPause(pause bool, args []string, env Env) int {
	fs := newFlags("pause", env)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	if err := config.SavePause(dirs.PauseFile(), pause, env.Now()); err != nil {
		fmt.Fprintln(env.Stderr, "일시중지 상태를 저장하지 못했습니다")
		return ExitError
	}
	if pause {
		fmt.Fprintln(env.Stdout, "수집을 일시중지했습니다. transcript tail, Hook 전달, 전송, heartbeat가 멈춥니다.")
	} else {
		fmt.Fprintln(env.Stdout, "수집을 재개했습니다. 일시중지 동안 쌓인 transcript 줄은 수집하지 않습니다.")
	}
	return ExitOK
}

func cmdService(args []string, env Env) int {
	if len(args) == 0 {
		usage(env.Stderr)
		return ExitUsage
	}
	sub := args[0]
	fs := newFlags("service "+sub, env)
	activate := fs.Bool("activate", false, "정의 파일을 쓴 뒤 OS 서비스 관리자 명령까지 실행")
	goos := fs.String("os", env.GOOS, "print 대상 OS")
	exeFlag := fs.String("exe", "", "print에 넣을 실행 파일 경로")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	if sub != "print" && (*goos != env.GOOS || *exeFlag != "") {
		fmt.Fprintln(env.Stderr, "--os, --exe는 service print에서만 쓸 수 있습니다")
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	exe := *exeFlag
	if exe == "" {
		if exe, err = env.Executable(); err != nil {
			fmt.Fprintln(env.Stderr, "실행 파일 경로를 확인할 수 없습니다")
			return ExitError
		}
	}
	loc, err := service.Location(*goos, env.Getenv, dirs)
	if err != nil {
		fmt.Fprintln(env.Stderr, "이 OS용 자동 시작 정의는 지원하지 않습니다")
		return ExitError
	}
	p := service.Params{GOOS: *goos, Exe: exe, Path: loc, UID: os.Getuid()}
	if u, d := env.Getenv("USERNAME"), env.Getenv("USERDOMAIN"); u != "" && d != "" {
		p.WindowsUser = d + `\` + u
	}
	def, err := service.Generate(p)
	if err != nil {
		fmt.Fprintln(env.Stderr, "자동 시작 정의를 만들 수 없습니다:", err)
		return ExitError
	}
	shown := displayPath(loc, dirs)
	switch sub {
	case "print":
		fmt.Fprint(env.Stdout, def.Text)
		return ExitOK
	case "install":
		if strings.Contains(filepath.ToSlash(exe), "/go-build") {
			fmt.Fprintln(env.Stderr, "go run 임시 실행 파일로는 설치할 수 없습니다. 빌드한 바이너리를 고정 위치에 두고 실행하세요")
			return ExitError
		}
		res, err := service.Install(def, *activate, env.Command)
		if err != nil {
			return serviceError(env, err, shown)
		}
		verb := "새로 썼습니다"
		if res.Replaced {
			verb = "갱신했습니다"
		}
		fmt.Fprintf(env.Stdout, "자동 시작 정의를 %s: %s\n", verb, shown)
		if res.Activated {
			fmt.Fprintln(env.Stdout, "OS 서비스 관리자에 등록하고 시작했습니다.")
		} else {
			fmt.Fprintln(env.Stdout, "다음 명령으로 활성화하세요(또는 --activate):")
			printCommands(env.Stdout, def.Activate, def.Path, shown)
		}
		return ExitOK
	case "uninstall":
		res, err := service.Uninstall(def, *activate, env.Command)
		if errors.Is(err, service.ErrNotInstalled) {
			fmt.Fprintln(env.Stdout, "설치된 자동 시작 정의가 없습니다.")
			return ExitOK
		}
		if err != nil {
			return serviceError(env, err, shown)
		}
		fmt.Fprintf(env.Stdout, "자동 시작 정의를 제거했습니다: %s\n", shown)
		if !res.Activated {
			fmt.Fprintln(env.Stdout, "이미 OS 서비스 관리자에 등록했다면 다음 명령으로 해제하세요(또는 --activate):")
			printCommands(env.Stdout, append(def.Deactivate, def.PostRemove...), def.Path, shown)
		}
		return ExitOK
	default:
		usage(env.Stderr)
		return ExitUsage
	}
}

func serviceError(env Env, err error, shown string) int {
	switch {
	case errors.Is(err, service.ErrForeign):
		fmt.Fprintf(env.Stderr, "같은 위치(%s) 또는 같은 이름의 작업이 Agent Office 소유가 아니어서 건드리지 않았습니다\n", shown)
	case errors.Is(err, service.ErrCommandFailed):
		fmt.Fprintln(env.Stderr, "정의 파일은 처리했지만 OS 서비스 관리자 명령이 실패했습니다:", err)
	default:
		fmt.Fprintln(env.Stderr, "자동 시작 정의 처리 실패:", err)
	}
	return ExitError
}

func printCommands(w io.Writer, cmds []service.Command, path, shown string) {
	for _, c := range cmds {
		parts := make([]string, len(c.Args))
		for i, a := range c.Args {
			if a == path {
				a = shown
			}
			if strings.ContainsAny(a, " \\") && a != shown {
				a = `"` + a + `"`
			}
			parts[i] = a
		}
		fmt.Fprintln(w, "  "+strings.Join(parts, " "))
	}
}

// displayPath는 홈 디렉터리를 `~`로 바꾼 경로다(사용자 본인 터미널 안내용, 진단에는 쓰지 않음).
func displayPath(p string, dirs paths.Dirs) string {
	if dirs.Home != "" && strings.HasPrefix(p, dirs.Home) {
		return "~" + p[len(dirs.Home):]
	}
	return p
}
