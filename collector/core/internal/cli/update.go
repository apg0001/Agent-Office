package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/service"
	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

// DefaultManifestBase는 채널별 manifest가 올라가는 GitHub Release(태그 collector-<채널>)다.
const DefaultManifestBase = "https://github.com/apg0001/Agent-Office/releases/download/collector-"

// cmdUpdate: `update check|apply [--channel stable|beta] [--manifest-url URL] [--yes]`.
// 업데이트는 알린 뒤 사용자가 apply로 적용한다(자동 적용은 하지 않음).
func cmdUpdate(args []string, env Env) int {
	if len(args) == 0 || (args[0] != "check" && args[0] != "apply") {
		fmt.Fprintln(env.Stderr, "사용법: agent-office-collector update check|apply [--channel stable|beta] [--manifest-url URL] [--yes]")
		return ExitUsage
	}
	sub := args[0]
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	channel := fs.String("channel", "stable", "")
	manifestURL := fs.String("manifest-url", "", "")
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args[1:]); err != nil || (*channel != "stable" && *channel != "beta") {
		return ExitUsage
	}
	if *manifestURL == "" {
		*manifestURL = DefaultManifestBase + *channel + "/manifest.json"
	}
	o := update.Options{
		ManifestURL: *manifestURL, Channel: *channel, Current: env.Version,
		GOOS: env.GOOS, GOARCH: runtime.GOARCH, Keys: update.ParseKeys(update.TrustedKeys),
		Client: env.HTTP,
	}
	ctx, cancel := context.WithTimeout(env.Context, 2*time.Minute)
	defer cancel()
	avail, err := update.Check(ctx, o)
	if err != nil {
		fmt.Fprintln(env.Stderr, "업데이트 확인 실패:", update.Code(err))
		return ExitError
	}
	if avail == nil {
		fmt.Fprintf(env.Stdout, "최신 버전입니다(%s, %s 채널)\n", env.Version, *channel)
		return ExitOK
	}
	fmt.Fprintf(env.Stdout, "새 버전 %s이 있습니다(현재 %s, %s 채널)\n", avail.Version, env.Version, *channel)
	if sub == "check" {
		return ExitOK
	}
	if !*yes {
		fmt.Fprintln(env.Stdout, "적용하려면 --yes를 붙여 다시 실행하세요.")
		return ExitOK
	}
	exe, err := env.Executable()
	if err != nil {
		fmt.Fprintln(env.Stderr, "실행 파일 위치를 알 수 없습니다")
		return ExitError
	}
	work, err := os.MkdirTemp(filepath.Dir(exe), ".agent-office-update-")
	if err != nil {
		work, err = os.MkdirTemp("", "agent-office-update-")
		if err != nil {
			fmt.Fprintln(env.Stderr, "임시 디렉터리를 만들 수 없습니다")
			return ExitError
		}
	}
	defer os.RemoveAll(work)
	archive, err := update.Download(ctx, o, avail.Artifact, work)
	if err != nil {
		fmt.Fprintln(env.Stderr, "다운로드 실패:", update.Code(err))
		return ExitError
	}
	next := filepath.Join(work, update.BinaryName(env.GOOS))
	if err := update.Extract(archive, avail.Artifact.Format, env.GOOS, next); err != nil {
		fmt.Fprintln(env.Stderr, "압축 해제 실패:", update.Code(err))
		return ExitError
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "경로를 알 수 없습니다")
		return ExitError
	}
	hooks := update.Hooks{
		Version: func(path string) (string, error) {
			out, err := exec.CommandContext(ctx, path, "version").Output()
			return string(out), err
		},
		Restart: func() error { return restartService(env, dirs) },
		Healthy: func() error { return waitHealthy(env, dirs, avail.Version) },
	}
	if err := update.Apply(exe, next, avail.Version, hooks); err != nil {
		fmt.Fprintln(env.Stderr, "적용 실패:", update.Code(err))
		if update.Code(err) == "managed_by_package" {
			fmt.Fprintln(env.Stderr, "패키지로 설치했다면 패키지 관리자(apt·dnf)로 업데이트하세요.")
		}
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s으로 업데이트했습니다. 이전 실행 파일은 %s.bak에 있습니다.\n", avail.Version, filepath.Base(exe))
	return ExitOK
}

// restartService는 자동 시작 서비스가 설치돼 있을 때만 다시 시작한다. 큐는 SQLite에 남아 있어
// 정상 종료(SIGTERM) 후 재시작해도 체크포인트에서 이어 간다.
func restartService(env Env, dirs paths.Dirs) error {
	loc, err := service.Location(env.GOOS, env.Getenv, dirs)
	if err != nil {
		return err
	}
	if _, err := os.Stat(loc); err != nil {
		return nil
	}
	var cmds [][]string
	switch env.GOOS {
	case "linux":
		cmds = [][]string{{"systemctl", "--user", "restart", service.UnitName}}
	case "darwin":
		cmds = [][]string{{"launchctl", "kickstart", "-k", "gui/" + strconv.Itoa(os.Getuid()) + "/" + service.LaunchdLabel}}
	case "windows":
		cmds = [][]string{{"schtasks", "/End", "/TN", service.TaskName}, {"schtasks", "/Run", "/TN", service.TaskName}}
	default:
		return service.ErrUnsupportedOS
	}
	for _, c := range cmds {
		if _, err := env.Command(c...); err != nil && c[1] != "/End" {
			return err
		}
	}
	return nil
}

// waitHealthy는 서비스가 있으면 새 버전의 run이 상태 파일을 쓸 때까지 기다린다.
func waitHealthy(env Env, dirs paths.Dirs, version string) error {
	loc, err := service.Location(env.GOOS, env.Getenv, dirs)
	if err != nil {
		return err
	}
	if _, err := os.Stat(loc); err != nil {
		return nil
	}
	deadline := env.Now().Add(30 * time.Second)
	for env.Now().Before(deadline) {
		snap, err := runner.ReadSnapshot(dirs.StatusFile())
		if err == nil && snap.CollectorVersion == version && snap.State != runner.StateStopped &&
			!strings.HasPrefix(snap.State, "error") {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("collector did not report the new version")
}
