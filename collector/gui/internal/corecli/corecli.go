// Package corecli는 GUI(#40)가 headless Collector Core(`agent-office-collector`)를
// 하위 프로세스로 호출하는 계층이다. GUI는 수집 루프를 품지 않고, Core의 명령과
// JSON 출력만 쓴다. 그래서 GUI 창을 닫아도 자동 시작된 Core의 수집은 계속된다.
//
// 이 패키지는 장치 토큰을 다루지 않는다. Core 설정 파일(config.json)을 읽지 않고,
// 등록 코드는 명령 인자 대신 표준 입력(`register --code-stdin`)으로만 넘긴다.
package corecli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// EnvBin은 Core 실행 파일 위치를 직접 지정하는 환경 변수다.
const EnvBin = "AGENT_OFFICE_COLLECTOR_BIN"

// BinName은 OS별 Core 실행 파일 이름이다.
func BinName(goos string) string {
	if goos == "windows" {
		return "agent-office-collector.exe"
	}
	return "agent-office-collector"
}

var (
	// ErrCoreMissing: Core 실행 파일을 찾지 못했다.
	ErrCoreMissing = errors.New("corecli: core executable not found")
	// ErrTimeout: Core 명령이 제한 시간 안에 끝나지 않았다.
	ErrTimeout = errors.New("corecli: core command timed out")
	// ErrStart: Core 프로세스를 시작하지 못했다.
	ErrStart = errors.New("corecli: core command could not start")
	// ErrMalformed: Core 출력이 예상한 JSON 형식이 아니다.
	ErrMalformed = errors.New("corecli: malformed core output")
)

// Locate는 Core 실행 파일을 찾는다. 순서: $AGENT_OFFICE_COLLECTOR_BIN(절대 경로) →
// GUI·트레이 실행 파일 옆(SiblingCandidates) → PATH.
func Locate(getenv func(string) string, guiExe string, goos string) (string, error) {
	if p := getenv(EnvBin); p != "" {
		if !filepath.IsAbs(p) {
			return "", ErrCoreMissing
		}
		if isFile(p) {
			return p, nil
		}
		return "", ErrCoreMissing
	}
	name := BinName(goos)
	// GUI·트레이 실행 파일 옆(macOS 트레이 번들이면 바깥 GUI 번들의 Contents/MacOS).
	if p, ok := FindSibling(guiExe, goos, "agent-office-collector"); ok {
		return p, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs, nil
		}
	}
	return "", ErrCoreMissing
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// Result는 Core 명령 한 번의 결과다. 출력은 상한까지만 보관한다.
type Result struct {
	Exit   int
	Stdout string
	Stderr string
}

// Runner는 Core 명령 실행기다.
type Runner struct {
	// Bin은 Core 실행 파일 절대 경로다.
	Bin string
	// Env는 하위 프로세스 환경이다. nil이면 GUI 환경을 물려준다.
	Env []string
	// Timeout은 명령당 상한이다(0이면 DefaultTimeout).
	Timeout time.Duration
}

const (
	// DefaultTimeout은 status·hooks 등 짧은 명령의 상한이다.
	DefaultTimeout = 20 * time.Second
	maxStdout      = 1 << 20
	maxStderr      = 8 << 10
)

// Run은 Core를 args로 실행한다. stdin이 비어 있지 않으면 표준 입력으로 넘긴다.
// 종료 코드가 0이 아니어도 오류로 보지 않고 Result.Exit로 돌려준다.
func (r Runner) Run(ctx context.Context, stdin string, args ...string) (Result, error) {
	if r.Bin == "" {
		return Result{}, ErrCoreMissing
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	cmd.Env = r.Env
	if stdin != "" {
		cmd.Stdin = bytes.NewReader([]byte(stdin))
	}
	stdout, stderr := &capped{max: maxStdout}, &capped{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	err := cmd.Run()
	res := Result{Stdout: stdout.buf.String(), Stderr: stderr.buf.String()}
	if ctx.Err() != nil {
		return res, ErrTimeout
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.Exit = exitErr.ExitCode()
	default:
		return res, ErrStart
	}
	return res, nil
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
