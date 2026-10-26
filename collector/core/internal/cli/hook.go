package cli

import (
	"flag"
	"io"
	"os"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/spool"
)

// runHook은 Hook stdin 한 건을 후보로 바꿔 spool에 넣는다.
//
// fail-open 규칙: 잘못된 인자·입력, 미등록, 일시중지, spool 가득 참, 디스크 오류, panic,
// stdin이 끝나지 않는 경우(HookTimeout) 모두 아무것도 출력하지 않고 0으로 끝난다.
// Claude Code는 일부 Hook의 stdout을 모델 문맥에 넣으므로 stdout에는 절대 쓰지 않는다.
// Hook 원문(prompt, tool_input 등)은 디스크에 쓰지 않고 어댑터 화이트리스트 후보만 남긴다.
func runHook(args []string, env Env) (code int) {
	defer func() {
		_ = recover()
		code = ExitOK
	}()
	if len(args) == 0 {
		return ExitOK
	}
	provider := args[0]
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	product := fs.String("product", "", "")
	version := fs.String("tool-version", "", "")
	mode := fs.String("mode", "", "")
	if fs.Parse(args[1:]) != nil {
		return ExitOK
	}
	observedAt := env.Now()
	payload, ok := readLimited(env.Stdin, adapter.MaxHookPayloadBytes, env.HookTimeout)
	if !ok {
		return ExitOK
	}
	dirs, err := env.Dirs()
	if err != nil {
		return ExitOK
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if err != nil {
		return ExitOK
	}
	if p, err := config.LoadPause(dirs.PauseFile()); err != nil || p.Paused {
		return ExitOK
	}
	rt := adapter.Runtime{Version: *version, OS: env.GOOS, Mode: *mode}
	opts := hookOptions(cfg, dirs)
	var c adapter.Candidate
	switch provider {
	case "claude":
		rt.Product = "claude-code"
		// Hook payload에는 버전이 없다. 플래그가 없으면 hooks install이 기록한 감지 버전을 쓴다.
		if rt.Version == "" {
			rt.Version, rt.Mode = cfg.ClaudeVersion, cfg.ClaudeMode
		}
		c, err = claude.ParseHookWith(payload, rt, opts)
	case "codex":
		rt.Product = codex.ProductCLI
		if *product == codex.ProductDesktop {
			rt.Product = codex.ProductDesktop
		}
		c, err = codex.ParseHookWith(payload, rt, opts)
	default:
		return ExitOK
	}
	if err != nil {
		return ExitOK
	}
	_ = spool.Write(dirs.SpoolDir(), spool.FromCandidate(c, observedAt), 0)
	return ExitOK
}

// hookOptions는 PC별 동의(#181)가 켜져 있을 때만 작업 폴더 이름을 읽게 한다. 홈 폴더 자체에서
// 실행한 세션은 마지막 이름이 OS 사용자 이름이므로 보내지 않는다.
func hookOptions(cfg config.Config, dirs paths.Dirs) adapter.HookOptions {
	if !cfg.ShareWorkspaceName {
		return adapter.HookOptions{}
	}
	opts := adapter.HookOptions{WorkspaceName: true}
	if dirs.Home != "" {
		opts.ExcludeDirs = append(opts.ExcludeDirs, dirs.Home)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		opts.ExcludeDirs = append(opts.ExcludeDirs, home)
	}
	return opts
}

// readLimited는 최대 limit 바이트를 timeout 안에 읽는다. 넘거나 늦으면 false다.
// 늦은 경우 읽기 고루틴은 프로세스 종료와 함께 사라진다.
func readLimited(r io.Reader, limit int, timeout time.Duration) ([]byte, bool) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
		ch <- result{data, err}
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case res := <-ch:
		if res.err != nil || len(res.data) > limit || len(res.data) == 0 {
			return nil, false
		}
		return res.data, true
	case <-t.C:
		return nil, false
	}
}
