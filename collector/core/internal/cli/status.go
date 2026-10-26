package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/instance"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/queue"
	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/service"
)

const healthPath = "/api/v1/system/health"

// Report는 status·diagnose 출력이다. 개수·enum·버전·시각과 기호 위치만 담는다.
// 장치 토큰, 서버 호스트 이름, 로컬 경로 원문, 세션 ID, 이벤트 내용은 넣지 않는다.
type Report struct {
	CollectorVersion string `json:"collector_version"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	GoVersion        string `json:"go_version,omitempty"`
	// Registration은 registered / not_registered / config_invalid다.
	Registration string `json:"registration"`
	// ServerScheme·ServerLoopback은 서버 주소 대신 남기는 안전한 요약이다.
	ServerScheme   string `json:"server_scheme,omitempty"`
	ServerLoopback bool   `json:"server_loopback,omitempty"`
	// TokenStorage는 장치 토큰 보관 위치다(#41): os_keyring(OS 보안 저장소) / file_fallback(0600 설정 파일,
	// 보안 저장소를 쓸 수 없거나 강제 파일 모드). 미등록이면 비어 있다. 저장소 항목 존재는 확인하지 않는다.
	TokenStorage string `json:"token_storage,omitempty"`
	// WorkspaceNameSharing은 작업 폴더 마지막 이름 전송 동의(#181)다. 미등록이면 false다.
	WorkspaceNameSharing bool   `json:"workspace_name_sharing"`
	Paused               bool   `json:"paused"`
	PauseState           string `json:"pause_state"`
	// ProcessRunning은 run 프로세스가 실행 잠금을 갖고 있는지다.
	ProcessRunning bool `json:"process_running"`
	// Run은 마지막 status.json 내용이다(없으면 nil).
	Run *runner.Snapshot `json:"run,omitempty"`
	// Queue는 로컬 큐 DB 개수다(파일이 없으면 nil).
	Queue *QueueReport `json:"queue,omitempty"`
	// Service는 자동 시작 정의 상태다(installed / absent / foreign / unsupported).
	Service string `json:"service"`
	// Locations는 OS별 기호 위치다(실제 경로 아님).
	Locations map[string]string `json:"locations,omitempty"`
	// Server는 diagnose의 서버 도달 결과다(reachable / http_<status> / network / timeout / skipped / not_configured).
	Server string `json:"server,omitempty"`
}

// QueueReport는 큐 개수다.
type QueueReport struct {
	Depth              int              `json:"depth"`
	DroppedOldest      int64            `json:"dropped_oldest"`
	DeadLetter         int64            `json:"dead_letter"`
	DeadLetterByReason map[string]int64 `json:"dead_letter_by_reason"`
	State              string           `json:"state"`
}

func cmdStatus(args []string, env Env, diagnose bool) int {
	name := "status"
	if diagnose {
		name = "diagnose"
	}
	fs := newFlags(name, env)
	asJSON := fs.Bool("json", false, "JSON으로 출력")
	offline := fs.Bool("offline", false, "서버 도달 확인 생략(diagnose)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	rep, cfg := buildReport(env, dirs, diagnose)
	if diagnose {
		switch {
		case cfg == nil:
			rep.Server = "not_configured"
		case *offline:
			rep.Server = "skipped"
		default:
			rep.Server = probeServer(env, cfg.ServerURL)
		}
	}
	if *asJSON {
		data, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(env.Stdout, string(data))
		return ExitOK
	}
	printReport(env.Stdout, rep, diagnose)
	return ExitOK
}

func buildReport(env Env, dirs paths.Dirs, diagnose bool) (Report, *config.Config) {
	rep := Report{CollectorVersion: env.Version, OS: env.GOOS, Arch: runtime.GOARCH, Service: "unsupported"}
	var cfgOut *config.Config
	switch cfg, err := config.Load(dirs.ConfigFile()); {
	case err == nil:
		rep.Registration = "registered"
		if u, err := url.Parse(cfg.ServerURL); err == nil {
			rep.ServerScheme = u.Scheme
			h := u.Hostname()
			ip := net.ParseIP(h)
			rep.ServerLoopback = h == "localhost" || (ip != nil && ip.IsLoopback())
		}
		rep.TokenStorage = "file_fallback"
		if cfg.InKeyring() {
			rep.TokenStorage = "os_keyring"
		}
		rep.WorkspaceNameSharing = cfg.ShareWorkspaceName
		cfgOut = &cfg
	case errors.Is(err, config.ErrNotRegistered):
		rep.Registration = "not_registered"
	default:
		rep.Registration = "config_invalid"
	}
	p, err := config.LoadPause(dirs.PauseFile())
	rep.Paused = p.Paused
	rep.PauseState = "ok"
	if err != nil {
		rep.PauseState = "invalid_treated_as_paused"
	}
	rep.ProcessRunning = instance.Held(dirs.LockFile())
	if snap, err := runner.ReadSnapshot(dirs.StatusFile()); err == nil {
		rep.Run = &snap
	}
	if _, err := os.Stat(dirs.QueueFile()); err == nil {
		qr := &QueueReport{State: "ok"}
		q, err := queue.Open(dirs.QueueFile(), queue.Options{})
		if err != nil {
			qr.State = "unavailable"
			if errors.Is(err, queue.ErrSchemaTooNew) {
				qr.State = "schema_too_new"
			}
		} else {
			ctx, cancel := context.WithTimeout(env.Context, 5*time.Second)
			st, err := q.Stats(ctx)
			cancel()
			_ = q.Close()
			if err != nil {
				qr.State = "unavailable"
			} else {
				qr.Depth, qr.DroppedOldest, qr.DeadLetter, qr.DeadLetterByReason = st.Depth, st.DroppedOldest, st.DeadLetter, st.DeadLetterByReason
			}
		}
		rep.Queue = qr
	}
	if loc, err := service.Location(env.GOOS, env.Getenv, dirs); err == nil {
		rep.Service = serviceState(loc)
	}
	if diagnose {
		rep.GoVersion = runtime.Version()
		rep.Locations = locations(env.GOOS, env.Getenv(paths.EnvHome) != "")
	}
	return rep, cfgOut
}

func serviceState(loc string) string {
	fi, err := os.Lstat(loc)
	if err != nil {
		return "absent"
	}
	if !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return "foreign"
	}
	data, err := os.ReadFile(loc)
	if err != nil || !service.Owned(data) {
		return "foreign"
	}
	return "installed"
}

// locations는 실제 경로 대신 OS 규칙 이름을 돌려준다.
func locations(goos string, override bool) map[string]string {
	if override {
		return map[string]string{"config": "$" + paths.EnvHome + "/config", "state": "$" + paths.EnvHome + "/state"}
	}
	switch goos {
	case "windows":
		return map[string]string{`config`: `%APPDATA%\AgentOffice\Collector`, `state`: `%LOCALAPPDATA%\AgentOffice\Collector`}
	case "darwin":
		return map[string]string{"config": "~/Library/Application Support/AgentOffice/Collector", "state": "~/Library/Application Support/AgentOffice/Collector/state"}
	default:
		return map[string]string{"config": "$XDG_CONFIG_HOME/agent-office/collector", "state": "$XDG_STATE_HOME/agent-office/collector"}
	}
}

// probeServer는 인증 없이 health 엔드포인트만 확인한다. 결과는 고정 코드다.
func probeServer(env Env, base string) string {
	ctx, cancel := context.WithTimeout(env.Context, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+healthPath, nil)
	if err != nil {
		return "network"
	}
	resp, err := env.HTTP.Do(req)
	if err != nil {
		var te interface{ Timeout() bool }
		if errors.As(err, &te) && te.Timeout() || ctx.Err() != nil {
			return "timeout"
		}
		return "network"
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "reachable"
	}
	return fmt.Sprintf("http_%d", resp.StatusCode)
}

func printReport(w io.Writer, r Report, diagnose bool) {
	fmt.Fprintf(w, "Agent Office Collector %s (%s/%s)\n", r.CollectorVersion, r.OS, r.Arch)
	if r.GoVersion != "" {
		fmt.Fprintf(w, "Go 런타임: %s\n", r.GoVersion)
	}
	reg := map[string]string{"registered": "등록됨", "not_registered": "미등록(register 필요)", "config_invalid": "설정 파일 손상(register --force 필요)"}[r.Registration]
	fmt.Fprintf(w, "등록: %s\n", reg)
	if r.ServerScheme != "" {
		loop := ""
		if r.ServerLoopback {
			loop = ", loopback"
		}
		fmt.Fprintf(w, "서버: %s%s (주소는 표시하지 않음)\n", r.ServerScheme, loop)
	}
	switch r.TokenStorage {
	case "os_keyring":
		fmt.Fprintln(w, "장치 토큰 보관: OS 보안 저장소")
	case "file_fallback":
		fmt.Fprintln(w, "장치 토큰 보관: 설정 파일(0600, 대체 경로: OS 보안 저장소를 쓸 수 없음)")
	}
	if r.Registration == "registered" {
		share := "꺼짐(기본)"
		if r.WorkspaceNameSharing {
			share = "켜짐(마지막 폴더 이름만)"
		}
		fmt.Fprintf(w, "작업 폴더 이름 보내기: %s\n", share)
	}
	pause := "수집 중"
	if r.Paused {
		pause = "일시중지"
	}
	if r.PauseState != "ok" {
		pause += " (pause 파일 손상: 안전하게 일시중지로 처리)"
	}
	fmt.Fprintf(w, "수집 설정: %s\n", pause)
	running := "없음"
	if r.ProcessRunning {
		running = "실행 중"
	}
	fmt.Fprintf(w, "run 프로세스: %s\n", running)
	if r.Run != nil {
		fmt.Fprintf(w, "마지막 실행 상태: %s (갱신 %s)\n", r.Run.State, r.Run.UpdatedAt.Format(time.RFC3339))
		fmt.Fprintf(w, "감시 루트: %s, 감시 파일 %d개\n", r.Run.WatchRoot, r.Run.WatchedFiles)
		if r.Run.LastSendOKAt != nil {
			fmt.Fprintf(w, "마지막 전송 성공: %s\n", r.Run.LastSendOKAt.Format(time.RFC3339))
		}
		if r.Run.LastHeartbeatOKAt != nil {
			fmt.Fprintf(w, "마지막 heartbeat 성공: %s\n", r.Run.LastHeartbeatOKAt.Format(time.RFC3339))
		}
		keys := make([]string, 0, len(r.Run.Counters))
		for k := range r.Run.Counters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			parts := make([]string, len(keys))
			for i, k := range keys {
				parts[i] = fmt.Sprintf("%s=%d", k, r.Run.Counters[k])
			}
			fmt.Fprintf(w, "개수: %s\n", strings.Join(parts, " "))
		}
		for _, e := range r.Run.Errors {
			fmt.Fprintf(w, "오류 %s: %d회, 마지막 %s\n", e.Code, e.Count, e.LastAt.Format(time.RFC3339))
		}
		if r.Run.State == runner.StateDeviceRevoked {
			fmt.Fprintln(w, "조치: 서버가 장치 토큰을 거부했습니다. 웹에서 새 코드를 받아 `register --force`로 다시 등록하세요")
		}
	}
	if r.Queue != nil {
		fmt.Fprintf(w, "로컬 큐: %s, 대기 %d건, drop-oldest %d건, dead-letter %d건\n", r.Queue.State, r.Queue.Depth, r.Queue.DroppedOldest, r.Queue.DeadLetter)
	}
	fmt.Fprintf(w, "자동 시작 정의: %s\n", r.Service)
	if diagnose {
		keys := make([]string, 0, len(r.Locations))
		for k := range r.Locations {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "위치 %s: %s\n", k, r.Locations[k])
		}
		fmt.Fprintf(w, "서버 도달: %s\n", r.Server)
	}
}
