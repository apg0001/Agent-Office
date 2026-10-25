package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	if sub != "print" && (flagSet(fs, "os") || flagSet(fs, "exe")) {
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

// hooksPath는 도구별 사용자 범위 Hook 설정 파일이다.
func hooksPath(dirs paths.Dirs, tool string) string {
	if tool == hooks.ToolCodex {
		return hooks.CodexHooksPath(dirs.CodexHome)
	}
	return hooks.ClaudeSettingsPath(dirs.ClaudeProjects)
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
	label := toolLabel(spec.Tool)
	st, err := hooks.Inspect(hooksPath(dirs, spec.Tool), spec)
	if err != nil {
		fmt.Fprintf(env.Stdout, "%s Hook: %s (%s)\n", label, hooks.StateUnreadable, hooksErrText(err))
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s Hook: %s (이벤트 %d/%d 최신, 누락 %d, 변경됨 %d, 다른 Hook %d개 보존)\n",
		label, st.State, st.Current, len(spec.Events), st.Missing, st.Modified, st.Foreign)
	if spec.Tool == hooks.ToolCodex && st.State != hooks.StateNotInstalled {
		fmt.Fprintln(env.Stdout, "Codex 신뢰 여부는 여기서 확인하지 않습니다. Codex에서 /hooks로 확인하세요(신뢰 전·변경 후에는 실행되지 않음).")
	}
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
	// HookInstall은 auto(install 지원)다. 이전 버전은 Codex에 print_only를 썼다.
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
		if st, err := hooks.Inspect(hooksPath(dirs, t), spec); err != nil {
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
	path := hooksPath(dirs, spec.Tool)
	shown := displayPath(path, dirs)
	if install && spec.Tool == hooks.ToolCodex && !isDir(dirs.CodexHome) {
		// Codex 홈은 Codex가 만든다. 없으면 설치되지 않았거나 CODEX_HOME이 다르다.
		fmt.Fprintf(env.Stderr, "Codex 설정 폴더가 없습니다: %s. Codex를 한 번 실행한 뒤 다시 시도하세요(CODEX_HOME을 쓰면 같은 값으로 실행)\n",
			displayPath(dirs.CodexHome, dirs))
		return ExitError
	}
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
			if spec.Tool == hooks.ToolCodex {
				printCodexTrustGuide(env)
			}
			saveToolVersion(env, dirs, spec.Tool, version)
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
		for _, e := range spec.Events {
			if t := spec.TimeoutFor(e); t != spec.Timeout {
				fmt.Fprintf(env.Stdout, "  (%s는 timeout %d)\n", e.Name, t)
			}
		}
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
	if install && spec.Tool == hooks.ToolCodex {
		fmt.Fprintln(env.Stdout, "Hook을 설치했습니다.")
		printCodexTrustGuide(env)
		saveToolVersion(env, dirs, spec.Tool, version)
	} else if install {
		fmt.Fprintln(env.Stdout, "Hook을 설치했습니다. 실행 중인 Claude Code 세션은 /hooks 확인 또는 재시작이 필요할 수 있습니다.")
		saveToolVersion(env, dirs, spec.Tool, version)
	} else {
		fmt.Fprintln(env.Stdout, "Agent Office Hook만 제거했습니다.")
	}
	return ExitOK
}

// printCodexTrustGuide는 Codex Hook 신뢰 절차를 안내한다. Collector는 Codex 신뢰 상태
// (`config.toml`의 `[hooks.state]`)를 쓰지 않는다. 사용자가 Codex에서 직접 검토·신뢰한다(#170).
func printCodexTrustGuide(env Env) {
	fmt.Fprintln(env.Stdout, "Codex는 신뢰하지 않은 Hook을 실행하지 않습니다. 새 터미널에서 대화형 codex를 실행하면 "+
		"\"Hooks need review\" 화면이 나옵니다. 1(Review hooks)로 내용을 확인하고 신뢰하거나 2(Trust all and continue)를 고르세요. "+
		"이미 실행 중이면 /hooks에서 신뢰할 수 있습니다. codex exec만 쓰는 경우에도 한 번은 대화형으로 신뢰해야 합니다. "+
		"Collector 실행 파일 위치가 바뀌거나 hooks.json이 바뀌면 다시 신뢰해야 합니다.")
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// saveToolVersion은 등록된 Collector 설정에 감지한 Claude 버전을 기록한다. 실행 모드는
// 설치 시점에 알 수 없으므로 claude_mode는 바꾸지 않는다. Codex Hook은 버전을 기록하지 않는다
// (실측 대상이 실행 모드까지 일치해야 하는데 payload로 모드를 알 수 없어 confidence가 바뀌지 않음).
func saveToolVersion(env Env, dirs paths.Dirs, tool, version string) {
	if version == "" || tool != hooks.ToolClaude {
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
