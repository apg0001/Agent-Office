package cli

// 설치 파일 기반 승인형 업데이트(#180, ADR-0006).
//
//	update check --installer [--json]                  새 버전의 플랫폼 설치 파일이 있는지
//	update download --installer [--json]               설치 파일을 받아 크기·SHA-256 확인(자동 다운로드 모드)
//	update install --installer --yes [--relaunch 트레이] [--result 결과.json] [--json]
//	                                                   받기·검증 → 업데이트 도우미를 띄우고 곧바로 끝남
//	update run-plan 계획.json                           (내부) 업데이트 도우미: 설치 프로그램 실행·결과 기록·트레이 재실행
//	update post-install [--json]                       설치 뒤 후처리: 소유 Hook·서비스 정의 재적용, Core 재시작, 연결 확인
//
// 도우미는 Core 실행 파일을 상태 폴더의 updates/<버전>/agent-office-update-helper로 복사해 실행한다. Windows
// 설치 프로그램이 실행 파일 이름으로 Core·트레이·GUI를 끝내므로 다른 이름이어야 설치 중에도 살아남는다.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/hooks"
	"github.com/apg0001/Agent-Office/collector/core/internal/instance"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/service"
	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

// HelperName은 업데이트 도우미 실행 파일 이름(확장자 제외)이다.
const HelperName = "agent-office-update-helper"

// installerTimeout은 설치 프로그램 실행 상한이다(사용자가 OS 승인 창에 응답하는 시간 포함).
const installerTimeout = 30 * time.Minute

// helperSettle은 도우미가 설치 프로그램을 실행하기 전 기다리는 시간이다. 승인한 트레이가 끝나 트레이 잠금을
// 놓을 시간을 준다(그래야 설치 뒤 다시 띄운 새 트레이가 곧바로 뜬다). 테스트는 0으로 둔다.
var helperSettle = 2 * time.Second

// trayWait는 트레이 잠금이 풀리기를 기다리는 상한이다.
var trayWait = 60 * time.Second

// waitUnlocked는 다른 프로세스가 path 잠금을 놓을 때까지 최대 limit 기다린다(GUI 트레이 잠금, internal/single).
func waitUnlocked(path string, limit time.Duration) {
	if path == "" {
		return
	}
	deadline := time.Now().Add(limit)
	for instance.Held(path) && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
	}
}

// installerReport는 update check|download|install --installer --json 출력이다. 버전·enum만 담는다.
type installerReport struct {
	// Status는 latest / available / downloaded / installer_started / error다.
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Version string `json:"version,omitempty"`
	Current string `json:"current"`
	Channel string `json:"channel"`
	Kind    string `json:"kind,omitempty"`
}

func printJSON(env Env, v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintln(env.Stdout, string(data))
}

func cmdUpdateInstaller(sub string, args []string, env Env) int {
	fs := newFlags("update "+sub, env)
	channel := fs.String("channel", "stable", "")
	manifestURL := fs.String("manifest-url", "", "")
	installer := fs.Bool("installer", false, "설치 파일 기반 업데이트")
	asJSON := fs.Bool("json", false, "")
	yes := fs.Bool("yes", false, "")
	relaunch := fs.String("relaunch", "", "설치 뒤 다시 띄울 트레이 실행 파일")
	resultPath := fs.String("result", "", "도우미가 결과를 쓸 파일")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || !*installer || (*channel != "stable" && *channel != "beta") {
		fmt.Fprintln(env.Stderr, "사용법: agent-office-collector update check|download|install --installer [--channel stable|beta] [--json] [--yes]")
		return ExitUsage
	}
	rep := installerReport{Current: env.Version, Channel: *channel}
	done := func(status, code string) int {
		rep.Status, rep.Code = status, code
		if *asJSON {
			printJSON(env, rep)
		} else {
			switch status {
			case "latest":
				fmt.Fprintf(env.Stdout, "최신 버전입니다(%s, %s 채널)\n", env.Version, *channel)
			case "available":
				fmt.Fprintf(env.Stdout, "새 버전 %s이 있습니다(현재 %s, %s 채널, 설치 파일 %s)\n", rep.Version, env.Version, *channel, rep.Kind)
			case "downloaded":
				fmt.Fprintf(env.Stdout, "새 버전 %s 설치 파일을 받아 확인했습니다\n", rep.Version)
			case "installer_started":
				fmt.Fprintf(env.Stdout, "새 버전 %s 설치를 시작했습니다. 설치가 끝나면 트레이가 다시 뜹니다\n", rep.Version)
			default:
				fmt.Fprintln(env.Stderr, "업데이트 실패:", code)
			}
		}
		if status == "error" {
			return ExitError
		}
		return ExitOK
	}
	exe, err := env.Executable()
	if err != nil {
		return done("error", "executable_unknown")
	}
	dirs, err := env.Dirs()
	if err != nil {
		return done("error", "dirs_unavailable")
	}
	if *relaunch != "" && !filepath.IsAbs(*relaunch) || *resultPath != "" && !filepath.IsAbs(*resultPath) {
		return done("error", "bad_path")
	}
	kind := update.DetectKind(env.GOOS, func(a ...string) bool { _, err := env.Command(a...); return err == nil })
	rep.Kind = kind
	if kind == "" || !update.Managed(exe) {
		// 명령행(압축본)으로 설치한 Core는 `update apply`로 자체 업데이트한다.
		return done("error", "not_installer_install")
	}
	o := updateOptions(env, *channel, *manifestURL)
	ctx, cancel := context.WithTimeout(env.Context, 15*time.Minute)
	defer cancel()
	avail, err := update.CheckInstaller(ctx, o, kind)
	if err != nil {
		return done("error", update.Code(err))
	}
	if avail == nil {
		return done("latest", "")
	}
	rep.Version = avail.Version
	if sub == "check" {
		return done("available", "")
	}
	if sub == "install" && !*yes {
		return done("error", "confirmation_required")
	}
	root := filepath.Join(dirs.State, "updates")
	stage := filepath.Join(root, avail.Version)
	if sub == "install" {
		cleanOldStages(root, avail.Version)
	}
	files, err := update.DownloadInstallers(ctx, o, avail.Installers, stage)
	if err != nil {
		return done("error", update.Code(err))
	}
	if sub == "download" {
		return done("downloaded", "")
	}
	plan := update.Plan{
		Schema: update.PlanSchema, From: env.Version, To: avail.Version, GOOS: env.GOOS, Kind: kind,
		Core: exe, Relaunch: *relaunch, Result: *resultPath,
	}
	if env.Getenv(EnvElevate) == update.ElevateSudoN {
		plan.Elevate = update.ElevateSudoN
	}
	for i, in := range avail.Installers {
		plan.Files = append(plan.Files, update.PlanFile{Path: files[i], SHA256: in.SHA256, Size: in.Size})
	}
	planPath := filepath.Join(stage, "plan.json")
	if err := update.WriteJSON(planPath, plan); err != nil {
		return done("error", "write_failed")
	}
	helper := filepath.Join(stage, HelperName)
	if env.GOOS == "windows" {
		helper += ".exe"
	}
	_ = os.Remove(helper)
	if err := update.CopyFile(exe, helper); err != nil {
		return done("error", "write_failed")
	}
	if err := env.Spawn(helper, "update", "run-plan", planPath); err != nil {
		return done("error", "helper_start_failed")
	}
	return done("installer_started", "")
}

// cleanOldStages는 다른 버전의 업데이트 준비 폴더를 지운다(실패는 무시: 실행 중인 도우미 등).
func cleanOldStages(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}

// cmdRunPlan은 업데이트 도우미다. 실패해도 이전 버전은 설치 프로그램이 바꾸기 전 그대로이며, 끝나면 항상
// 트레이를 다시 띄워(새 트레이가 결과를 알리고 후처리를 한다) 사용자에게 결과가 보이게 한다.
func cmdRunPlan(args []string, env Env) int {
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		return ExitUsage
	}
	var plan update.Plan
	if err := update.ReadJSON(args[0], &plan); err != nil || plan.Validate() != nil || plan.GOOS != env.GOOS {
		return ExitError
	}
	trayLock := ""
	if plan.Result != "" {
		trayLock = filepath.Join(filepath.Dir(plan.Result), "tray.lock")
	}
	out := update.Outcome{Schema: update.PlanSchema, From: plan.From, To: plan.To}
	finish := func(code string) int {
		out.Code, out.FinishedAt = code, env.Now().UTC()
		dest := plan.Result
		if dest == "" {
			dest = filepath.Join(filepath.Dir(args[0]), "outcome.json")
		}
		_ = update.WriteJSON(dest, out)
		waitUnlocked(trayLock, trayWait)
		if plan.Relaunch != "" {
			if fi, err := os.Stat(plan.Relaunch); err == nil && fi.Mode().IsRegular() {
				_ = env.Spawn(plan.Relaunch)
			}
		}
		if code == "installed" {
			return ExitOK
		}
		return ExitError
	}
	// 승인한 트레이가 끝날 때까지(트레이 잠금이 풀릴 때까지) 기다린다. Windows 설치 프로그램이 파일을 바꿀 수 있고,
	// 설치 뒤 다시 띄운 트레이가 단일 실행 잠금에 막히지 않는다.
	waitUnlocked(trayLock, trayWait)
	time.Sleep(helperSettle)
	var files []string
	for _, f := range plan.Files {
		// 계획을 쓴 뒤 바뀌지 않았는지 실행 직전에 다시 확인한다.
		if err := update.VerifyFile(f.Path, f.SHA256, f.Size); err != nil {
			return finish(update.Code(err))
		}
		files = append(files, f.Path)
	}
	argv, err := update.InstallCommand(plan.GOOS, plan.Kind, plan.Elevate, files)
	if err != nil {
		return finish(update.Code(err))
	}
	exit, tail, err := env.RunLong(installerTimeout, argv...)
	switch {
	case err != nil:
		return finish("installer_start_failed")
	case exit != 0 && installerCancelled(plan.GOOS, exit, tail):
		return finish("installer_cancelled")
	case exit != 0:
		return finish("installer_failed")
	}
	if v, err := env.Command(plan.Core, "version"); err != nil || !strings.Contains(string(v), " "+plan.To+" ") {
		return finish("version_mismatch")
	}
	for _, f := range files {
		_ = os.Remove(f)
	}
	return finish("installed")
}

// installerCancelled는 사용자가 OS 승인 창에서 취소했는지다(macOS osascript -128, pkexec 126).
func installerCancelled(goos string, exit int, tail []byte) bool {
	switch goos {
	case "darwin":
		return bytes.Contains(tail, []byte("-128"))
	case "linux":
		return exit == 126
	}
	return false
}

// postInstallReport는 update post-install 결과다. 도구 이름·enum·개수만 담는다.
type postInstallReport struct {
	// Status는 ok(할 일 없음 또는 모두 성공) / attention(사용자 확인 필요)이다.
	Status  string       `json:"status"`
	Version string       `json:"version"`
	Hooks   []hookAction `json:"hooks"`
	// Service는 absent / foreign / reinstalled / failed / unsupported다.
	Service string `json:"service"`
	// Health는 healthy / unhealthy / skipped(서비스 없음)다.
	Health string `json:"health"`
	// CodexRetrust는 Codex Hook 항목이 바뀌어 Codex에서 다시 신뢰해야 하는지다.
	CodexRetrust bool `json:"codex_retrust"`
	// LegacyFiles는 정리할 수 있는 이전 명령행 설치 파일 수다(legacy status).
	LegacyFiles int `json:"legacy_files"`
}

type hookAction struct {
	Tool string `json:"tool"`
	// Action은 absent / unchanged / reapplied / failed / unreadable이다.
	Action string `json:"action"`
}

// cmdPostInstall은 설치·업데이트 뒤 후처리다. 이전에 설치돼 있던 것만 다시 적용한다(멱등).
//   - Hook: 도구별 설정에 Agent Office 소유 항목이 있으면 현재 실행 파일 경로로 다시 설치한다(명령 서명으로
//     소유 항목을 알아보고 다른 Hook은 그대로, 백업·원자적 쓰기). 명령 문자열이 바뀌면 Codex는 다시 신뢰해야 한다.
//   - 서비스: 소유 표시가 있는 자동 시작 정의가 있으면 현재 경로로 다시 쓰고 활성화·재시작한다(이전 명령행
//     설치의 Core 프로세스를 멈추고 새 Core 하나만 돈다).
//   - 연결: 서비스가 있으면 새 버전이 상태 파일을 쓸 때까지 기다린다.
func cmdPostInstall(args []string, env Env) int {
	fs := newFlags("update post-install", env)
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	exe, err := env.Executable()
	if err != nil {
		return ExitError
	}
	dirs, err := env.Dirs()
	if err != nil {
		return ExitError
	}
	rep := postInstallReport{Status: "ok", Version: env.Version, Service: "absent", Health: "skipped"}
	for _, t := range []string{hooks.ToolClaude, hooks.ToolCodex} {
		a := hookAction{Tool: t, Action: reapplyHooks(env, dirs, exe, t)}
		if a.Action == "reapplied" && t == hooks.ToolCodex {
			rep.CodexRetrust = true
		}
		if a.Action == "failed" || a.Action == "unreadable" {
			rep.Status = "attention"
		}
		rep.Hooks = append(rep.Hooks, a)
	}
	rep.Service = reinstallService(env, dirs, exe)
	switch rep.Service {
	case "reinstalled":
		if waitHealthy(env, dirs, env.Version) == nil {
			rep.Health = "healthy"
		} else {
			rep.Health, rep.Status = "unhealthy", "attention"
		}
	case "failed", "foreign":
		rep.Status = "attention"
	}
	if rep.CodexRetrust {
		rep.Status = "attention"
	}
	rep.LegacyFiles = len(findLegacy(env, dirs, exe))
	if *asJSON {
		printJSON(env, rep)
	} else {
		fmt.Fprintf(env.Stdout, "후처리(%s): 서비스 %s, 연결 %s\n", env.Version, rep.Service, rep.Health)
		for _, h := range rep.Hooks {
			fmt.Fprintf(env.Stdout, "  %s Hook: %s\n", toolLabel(h.Tool), h.Action)
		}
		if rep.CodexRetrust {
			printCodexTrustGuide(env)
		}
		if rep.LegacyFiles > 0 {
			fmt.Fprintf(env.Stdout, "이전 명령행 설치 파일 %d개를 정리할 수 있습니다: agent-office-collector legacy status\n", rep.LegacyFiles)
		}
	}
	if rep.Status != "ok" {
		return ExitError
	}
	return ExitOK
}

func reapplyHooks(env Env, dirs paths.Dirs, exe, tool string) string {
	spec, err := hooks.Generate(hooks.Params{Tool: tool, GOOS: env.GOOS, Exe: exe})
	if err != nil {
		return "failed"
	}
	path := hooksPath(dirs, tool)
	st, err := hooks.Inspect(path, spec)
	if err != nil {
		return "unreadable"
	}
	if st.State == hooks.StateNotInstalled {
		return "absent"
	}
	res, err := hooks.Install(path, spec, env.Now(), true)
	if err != nil {
		return "failed"
	}
	if len(res.Changes) == 0 {
		return "unchanged"
	}
	return "reapplied"
}

func reinstallService(env Env, dirs paths.Dirs, exe string) string {
	loc, err := service.Location(env.GOOS, env.Getenv, dirs)
	if err != nil {
		return "unsupported"
	}
	data, err := os.ReadFile(loc)
	if err != nil {
		return "absent"
	}
	if !service.Owned(data) {
		return "foreign"
	}
	p := service.Params{GOOS: env.GOOS, Exe: exe, Path: loc, UID: os.Getuid()}
	if u, d := env.Getenv("USERNAME"), env.Getenv("USERDOMAIN"); u != "" && d != "" {
		p.WindowsUser = d + `\` + u
	}
	def, err := service.Generate(p)
	if err != nil {
		return "failed"
	}
	if env.GOOS == "windows" {
		// 이전(명령행 설치 등) 경로의 Core를 먼저 멈춘다. /Create /F는 실행 중인 인스턴스를 끝내지 않는다.
		_, _ = env.Command("schtasks", "/End", "/TN", service.TaskName)
	}
	if _, err := service.Install(def, true, env.Command); err != nil {
		return "failed"
	}
	if env.GOOS == "linux" {
		// enable --now는 이미 돌고 있는 이전 Core를 다시 시작하지 않는다.
		if _, err := env.Command("systemctl", "--user", "restart", service.UnitName); err != nil {
			return "failed"
		}
	}
	return "reinstalled"
}
