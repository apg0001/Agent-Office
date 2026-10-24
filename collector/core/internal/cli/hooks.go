package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/hooks"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
)

// versionTimeout은 `<도구> --version` 실행 상한이다.
const versionTimeout = 3 * time.Second

// detectToolVersion은 PATH의 도구를 `--version`으로 실행해 버전 문자열만 돌려준다.
// stdout은 4KiB까지만 읽고, stderr와 나머지 출력은 보관하지 않는다.
func detectToolVersion(name string) (string, error) {
	exe, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	out := &capped{max: 4096}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", err
	}
	v := hooks.ParseVersion(out.buf.Bytes())
	if v == "" {
		return "", errors.New("version not found")
	}
	return v, nil
}

type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// cmdHooks는 `hooks install|uninstall|status|print`다.
func cmdHooks(args []string, env Env) int {
	if len(args) == 0 {
		usage(env.Stderr)
		return ExitUsage
	}
	sub := args[0]
	fs := newFlags("hooks "+sub, env)
	tool := fs.String("tool", "", "claude|codex (status는 생략 시 둘 다)")
	yes := fs.Bool("yes", false, "미리보기를 확인했고 설정 파일 변경을 승인함")
	scope := fs.String("scope", "user", "설치 범위(user만 지원)")
	goos := fs.String("os", env.GOOS, "print 대상 OS")
	exeFlag := fs.String("exe", "", "print에 넣을 실행 파일 경로")
	asJSON := fs.Bool("json", false, "status를 JSON으로 출력(GUI용)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	if *asJSON && sub != "status" {
		fmt.Fprintln(env.Stderr, "--json은 hooks status에서만 쓸 수 있습니다")
		return ExitUsage
	}
	if *scope != "user" {
		fmt.Fprintln(env.Stderr, "--scope는 user만 지원합니다")
		return ExitUsage
	}
	if sub != "print" && (*goos != env.GOOS || *exeFlag != "") {
		fmt.Fprintln(env.Stderr, "--os, --exe는 hooks print에서만 쓸 수 있습니다")
		return ExitUsage
	}
	tools := []string{hooks.ToolClaude}
	switch *tool {
	case "":
		if sub == "status" {
			tools = []string{hooks.ToolClaude, hooks.ToolCodex}
		}
	case hooks.ToolClaude, hooks.ToolCodex:
		tools = []string{*tool}
	default:
		fmt.Fprintln(env.Stderr, "--tool은 claude 또는 codex입니다")
		return ExitUsage
	}
	switch sub {
	case "print", "install", "uninstall", "status":
	default:
		usage(env.Stderr)
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
	if sub == "print" {
		spec, err := hooks.Generate(hooks.Params{Tool: tools[0], GOOS: *goos, Exe: exe})
		if err != nil {
			fmt.Fprintln(env.Stderr, "Hook 항목을 만들 수 없습니다:", err)
			return ExitError
		}
		_, _ = env.Stdout.Write(spec.JSON())
		return ExitOK
	}
	if *asJSON {
		return hooksStatusJSON(env, dirs, exe, tools)
	}
	code := ExitOK
	for _, t := range tools {
		spec, err := hooks.Generate(hooks.Params{Tool: t, GOOS: env.GOOS, Exe: exe})
		if err != nil {
			fmt.Fprintln(env.Stderr, "Hook 항목을 만들 수 없습니다:", err)
			return ExitError
		}
		var c int
		switch sub {
		case "status":
			c = hooksStatus(env, dirs, spec)
		case "install", "uninstall":
			if t == hooks.ToolCodex {
				fmt.Fprintln(env.Stderr, "Codex Hook 자동 설치는 지원하지 않습니다. 사용자 범위 설정 위치·신뢰 흐름이 실측되지 않았습니다(#17·#18). "+
					"`hooks print --tool codex` 출력을 검토해 직접 등록하세요.")
				return ExitError
			}
			if sub == "install" && strings.Contains(filepath.ToSlash(exe), "/go-build") {
				fmt.Fprintln(env.Stderr, "go run 임시 실행 파일로는 설치할 수 없습니다. 빌드한 바이너리를 고정 위치에 두고 실행하세요")
				return ExitError
			}
			c = hooksApply(env, dirs, spec, sub == "install", *yes)
		}
		if c != ExitOK {
			code = c
		}
	}
	return code
}

func toolLabel(t string) string {
	if t == hooks.ToolCodex {
		return "Codex"
	}
	return "Claude Code"
}

// reportVersion은 감지한 도구 버전과 검증 수준을 출력하고 버전을 돌려준다.
func reportVersion(env Env, t string) string {
	v, err := env.ToolVersion(t)
	if err != nil || v == "" {
		fmt.Fprintf(env.Stdout, "%s 버전: 감지 실패(설치되지 않았거나 PATH에 없음) → unverified\n", toolLabel(t))
		return ""
	}
	sup := hooks.AssessVersion(t, v, env.GOOS)
	note := ""
	if sup.Mode != "" {
		note = fmt.Sprintf(" (실측 모드 %s 한정)", sup.Mode)
	}
	fmt.Fprintf(env.Stdout, "%s 버전: %s, %s → %s%s\n", toolLabel(t), v, env.GOOS, sup.Level, note)
	return v
}

func hooksStatus(env Env, dirs paths.Dirs, spec hooks.Spec) int {
	reportVersion(env, spec.Tool)
	if spec.Tool == hooks.ToolCodex {
		fmt.Fprintln(env.Stdout, "Codex Hook: print 전용(자동 설치·상태 확인 미지원)")
		return ExitOK
	}
	path := hooks.ClaudeSettingsPath(dirs.ClaudeProjects)
	st, err := hooks.Inspect(path, spec)
	if err != nil {
		fmt.Fprintf(env.Stdout, "Claude Code Hook: %s (%s)\n", hooks.StateUnreadable, hooksErrText(err))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "Claude Code Hook: %s (이벤트 %d/%d 최신, 누락 %d, 변경됨 %d, 다른 Hook %d개 보존)\n",
		st.State, st.Current, len(spec.Events), st.Missing, st.Modified, st.Foreign)
	return ExitOK
}

// ToolReport는 `hooks status --json`의 도구 하나다. 버전 문자열·enum·개수만 담고
// 설정 파일 경로, Hook 명령 원문, 다른 Hook 내용은 넣지 않는다.
type ToolReport struct {
	Tool string `json:"tool"`
	// Detected는 PATH에서 `<도구> --version`으로 버전을 얻었는지다.
	Detected bool   `json:"detected"`
	Version  string `json:"version,omitempty"`
	// Support는 어댑터 실측 표와의 일치 수준(verified / partial / unverified)이다.
	Support string `json:"support"`
	// VerifiedMode는 실측 모드(print / exec / desktop)다. 일치하지 않으면 비어 있다.
	VerifiedMode string `json:"verified_mode,omitempty"`
	// HookInstall은 auto(install 지원) 또는 print_only다.
	HookInstall string `json:"hook_install"`
	// HookState는 installed / partial / modified / not_installed / unreadable / unknown이다.
	HookState     string `json:"hook_state"`
	EventsCurrent int    `json:"events_current"`
	EventsTotal   int    `json:"events_total"`
	Missing       int    `json:"missing"`
	Modified      int    `json:"modified"`
	Foreign       int    `json:"foreign"`
	// Error는 unreadable일 때의 고정 코드다.
	Error string `json:"error,omitempty"`
}

func hooksStatusJSON(env Env, dirs paths.Dirs, exe string, tools []string) int {
	out := struct {
		OS    string       `json:"os"`
		Tools []ToolReport `json:"tools"`
	}{OS: env.GOOS}
	code := ExitOK
	for _, t := range tools {
		spec, err := hooks.Generate(hooks.Params{Tool: t, GOOS: env.GOOS, Exe: exe})
		if err != nil {
			fmt.Fprintln(env.Stderr, "Hook 항목을 만들 수 없습니다:", err)
			return ExitError
		}
		r := ToolReport{Tool: t, Support: "unverified", HookInstall: "auto", HookState: "unknown", EventsTotal: len(spec.Events)}
		if v, err := env.ToolVersion(t); err == nil && v != "" {
			sup := hooks.AssessVersion(t, v, env.GOOS)
			r.Detected, r.Version, r.Support, r.VerifiedMode = true, v, string(sup.Level), sup.Mode
		}
		if t == hooks.ToolCodex {
			r.HookInstall = "print_only"
		} else if st, err := hooks.Inspect(hooks.ClaudeSettingsPath(dirs.ClaudeProjects), spec); err != nil {
			r.HookState, r.Error = string(hooks.StateUnreadable), hooksErrCode(err)
			code = ExitError
		} else {
			r.HookState, r.EventsCurrent, r.Missing, r.Modified, r.Foreign = string(st.State), st.Current, st.Missing, st.Modified, st.Foreign
		}
		out.Tools = append(out.Tools, r)
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(env.Stdout, string(data))
	return code
}

func hooksErrCode(err error) string {
	switch {
	case errors.Is(err, hooks.ErrParse):
		return "parse_error"
	case errors.Is(err, hooks.ErrShape):
		return "shape_error"
	case errors.Is(err, hooks.ErrNotRegular):
		return "not_regular"
	case errors.Is(err, hooks.ErrReadOnly):
		return "read_only"
	case errors.Is(err, hooks.ErrTooLarge):
		return "too_large"
	case errors.Is(err, hooks.ErrChanged):
		return "changed"
	default:
		return "io_error"
	}
}

func hooksApply(env Env, dirs paths.Dirs, spec hooks.Spec, install, yes bool) int {
	path := hooks.ClaudeSettingsPath(dirs.ClaudeProjects)
	shown := displayPath(path, dirs)
	version := ""
	if install {
		version = reportVersion(env, spec.Tool)
	}
	op := hooks.Uninstall
	if install {
		op = hooks.Install
	}
	res, err := op(path, spec, env.Now(), yes)
	if err != nil && len(res.Changes) == 0 {
		fmt.Fprintf(env.Stderr, "%s을(를) 변경하지 않았습니다: %s\n", shown, hooksErrText(err))
		return ExitError
	}
	if len(res.Changes) == 0 {
		if install {
			fmt.Fprintf(env.Stdout, "이미 설치돼 있습니다(변경 없음): %s\n", shown)
			saveClaudeVersion(env, dirs, version)
		} else {
			fmt.Fprintf(env.Stdout, "제거할 Agent Office Hook이 없습니다: %s\n", shown)
		}
		return ExitOK
	}
	fmt.Fprintf(env.Stdout, "대상: %s\n변경 미리보기:\n", shown)
	for _, c := range res.Changes {
		sign := map[hooks.Action]string{hooks.ActionAdd: "+", hooks.ActionReplace: "~", hooks.ActionRemove: "-"}[c.Action]
		fmt.Fprintf(env.Stdout, "  %s %s\n", sign, c.Event)
	}
	if install {
		fmt.Fprintf(env.Stdout, "추가할 핸들러: {\"type\":\"command\",\"command\":%q,\"timeout\":%d}\n", spec.Command, spec.Timeout)
	}
	fmt.Fprintln(env.Stdout, "다른 키와 Agent Office 소유가 아닌 Hook은 그대로 둡니다.")
	if !yes {
		fmt.Fprintln(env.Stdout, "파일을 바꾸지 않았습니다. 위 내용을 확인했으면 --yes를 붙여 다시 실행하세요.")
		return ExitOK
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s을(를) 변경하지 않았습니다: %s\n", shown, hooksErrText(err))
		if res.Backup != "" {
			fmt.Fprintf(env.Stderr, "백업: %s\n", displayPath(res.Backup, dirs))
		}
		return ExitError
	}
	if res.Backup != "" {
		fmt.Fprintf(env.Stdout, "백업(0600): %s\n", displayPath(res.Backup, dirs))
	}
	if install {
		fmt.Fprintln(env.Stdout, "Hook을 설치했습니다. 실행 중인 Claude Code 세션은 /hooks 확인 또는 재시작이 필요할 수 있습니다.")
		saveClaudeVersion(env, dirs, version)
	} else {
		fmt.Fprintln(env.Stdout, "Agent Office Hook만 제거했습니다.")
	}
	return ExitOK
}

// saveClaudeVersion은 등록된 Collector 설정에 감지한 Claude 버전을 기록한다. 실행 모드는
// 설치 시점에 알 수 없으므로 claude_mode는 바꾸지 않는다.
func saveClaudeVersion(env Env, dirs paths.Dirs, version string) {
	if version == "" {
		return
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil || cfg.ClaudeVersion == version {
		return
	}
	cfg.ClaudeVersion = version
	if config.Save(dirs.ConfigFile(), cfg) == nil {
		fmt.Fprintf(env.Stdout, "Collector 설정 claude_version을 %s로 기록했습니다.\n", version)
	}
}

func hooksErrText(err error) string {
	switch {
	case errors.Is(err, hooks.ErrParse):
		return "JSON 객체로 읽을 수 없습니다(주석·중복 키·문법 오류). 직접 고친 뒤 다시 실행하세요"
	case errors.Is(err, hooks.ErrShape):
		return "hooks 항목 형식이 예상과 다릅니다"
	case errors.Is(err, hooks.ErrNotRegular):
		return "일반 파일이 아닙니다(심볼릭 링크 등)"
	case errors.Is(err, hooks.ErrReadOnly):
		return "읽기 전용 파일입니다"
	case errors.Is(err, hooks.ErrTooLarge):
		return "파일이 너무 큽니다"
	case errors.Is(err, hooks.ErrChanged):
		return "작업 중 다른 프로그램이 파일을 바꿨습니다. 다시 실행하세요"
	default:
		return "파일을 읽거나 쓸 수 없습니다"
	}
}
