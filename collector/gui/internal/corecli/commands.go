package corecli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Report는 Core `status --json`·`diagnose --json` 출력 중 GUI가 쓰는 필드다.
// Core는 토큰·호스트 이름·경로 원문·장치 ID를 넣지 않는다(collector-cli.md).
type Report struct {
	CollectorVersion string            `json:"collector_version"`
	OS               string            `json:"os"`
	Arch             string            `json:"arch"`
	GoVersion        string            `json:"go_version,omitempty"`
	Registration     string            `json:"registration"`
	ServerScheme     string            `json:"server_scheme,omitempty"`
	ServerLoopback   bool              `json:"server_loopback,omitempty"`
	Paused           bool              `json:"paused"`
	PauseState       string            `json:"pause_state"`
	ProcessRunning   bool              `json:"process_running"`
	Run              *RunSnapshot      `json:"run,omitempty"`
	Queue            *QueueReport      `json:"queue,omitempty"`
	Service          string            `json:"service"`
	Locations        map[string]string `json:"locations,omitempty"`
	Server           string            `json:"server,omitempty"`
}

// RunSnapshot은 실행 중인 `run`이 남긴 마지막 상태다.
type RunSnapshot struct {
	State             string           `json:"state"`
	UpdatedAt         time.Time        `json:"updated_at"`
	WatchRoot         string           `json:"watch_root"`
	WatchedFiles      int              `json:"watched_files"`
	QueueDepth        int              `json:"queue_depth"`
	Counters          map[string]int64 `json:"counters"`
	Errors            []ErrorStat      `json:"errors"`
	LastSendOKAt      *time.Time       `json:"last_send_ok_at,omitempty"`
	LastHeartbeatOKAt *time.Time       `json:"last_heartbeat_ok_at,omitempty"`
}

// ErrorStat은 오류 코드별 개수다. 오류 문자열 원문은 없다.
type ErrorStat struct {
	Code   string    `json:"code"`
	Count  int64     `json:"count"`
	LastAt time.Time `json:"last_at"`
}

// QueueReport는 로컬 큐 개수다.
type QueueReport struct {
	Depth         int    `json:"depth"`
	DroppedOldest int64  `json:"dropped_oldest"`
	DeadLetter    int64  `json:"dead_letter"`
	State         string `json:"state"`
}

// ToolsReport는 Core `hooks status --json` 출력이다.
type ToolsReport struct {
	OS    string       `json:"os"`
	Tools []ToolReport `json:"tools"`
}

// ToolReport는 도구 하나의 탐지·Hook 상태다.
type ToolReport struct {
	Tool          string `json:"tool"`
	Detected      bool   `json:"detected"`
	Version       string `json:"version,omitempty"`
	Support       string `json:"support"`
	VerifiedMode  string `json:"verified_mode,omitempty"`
	HookInstall   string `json:"hook_install"`
	HookState     string `json:"hook_state"`
	EventsCurrent int    `json:"events_current"`
	EventsTotal   int    `json:"events_total"`
	Missing       int    `json:"missing"`
	Modified      int    `json:"modified"`
	Foreign       int    `json:"foreign"`
	Error         string `json:"error,omitempty"`
}

// 알려진 enum. 모르는 값은 "unknown"으로 바꿔 화면에 임의 문자열이 나오지 않게 한다.
var (
	registrations = set("registered", "not_registered", "config_invalid")
	runStates     = set("starting", "running", "paused", "device_revoked", "not_registered", "config_invalid", "stopped")
	services      = set("installed", "absent", "foreign", "unsupported")
	hookStates    = set("installed", "partial", "modified", "not_installed", "unreadable", "unknown")
	supports      = set("verified", "partial", "unverified")
	errorCode     = func(s string) bool { return isCode(s, 64) }
)

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

// isCode는 소문자·숫자·밑줄로 된 짧은 코드인지다.
func isCode(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func enum(v string, allowed map[string]bool) string {
	if allowed[v] {
		return v
	}
	return "unknown"
}

// ParseReport는 status·diagnose JSON을 읽고 enum·코드를 정리한다.
func ParseReport(out string) (Report, error) {
	var r Report
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil {
		return Report{}, ErrMalformed
	}
	if r.Registration == "" && r.Service == "" {
		return Report{}, ErrMalformed
	}
	r.Registration = enum(r.Registration, registrations)
	r.Service = enum(r.Service, services)
	if r.Server != "" && !isCode(r.Server, 32) {
		r.Server = "unknown"
	}
	if r.Run != nil {
		r.Run.State = enum(r.Run.State, runStates)
		// watch_root는 available / not_found / unavailable 같은 코드만 허용한다.
		if !isCode(r.Run.WatchRoot, 32) {
			r.Run.WatchRoot = "unknown"
		}
		errs := r.Run.Errors[:0]
		for _, e := range r.Run.Errors {
			if errorCode(e.Code) {
				errs = append(errs, e)
			}
		}
		r.Run.Errors = errs
		for k := range r.Run.Counters {
			if !isCode(k, 64) {
				delete(r.Run.Counters, k)
			}
		}
	}
	if r.Queue != nil && !isCode(r.Queue.State, 32) {
		r.Queue.State = "unknown"
	}
	return r, nil
}

// ParseTools는 `hooks status --json`을 읽는다.
func ParseTools(out string) (ToolsReport, error) {
	var t ToolsReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &t); err != nil || len(t.Tools) == 0 {
		return ToolsReport{}, ErrMalformed
	}
	for i := range t.Tools {
		tr := &t.Tools[i]
		if tr.Tool != "claude" && tr.Tool != "codex" {
			return ToolsReport{}, ErrMalformed
		}
		tr.Support = enum(tr.Support, supports)
		tr.HookState = enum(tr.HookState, hookStates)
		if tr.HookInstall != "auto" && tr.HookInstall != "print_only" {
			tr.HookInstall = "print_only"
		}
		if !validVersion(tr.Version) {
			tr.Version, tr.Detected = "", false
		}
		if tr.Error != "" && !isCode(tr.Error, 32) {
			tr.Error = "unknown"
		}
	}
	return t, nil
}

func validVersion(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > 64 {
		return false
	}
	for _, r := range v {
		if !(unicode.IsDigit(r) || unicode.IsLetter(r) && r < 128 || strings.ContainsRune(".+-", r)) {
			return false
		}
	}
	return true
}

// Status는 `status --json`이다.
func (r Runner) Status(ctx context.Context) (Report, error) {
	res, err := r.Run(ctx, "", "status", "--json")
	if err != nil {
		return Report{}, err
	}
	if res.Exit != 0 {
		return Report{}, ErrMalformed
	}
	return ParseReport(res.Stdout)
}

// Diagnose는 `diagnose --json [--offline]`이다.
func (r Runner) Diagnose(ctx context.Context, offline bool) (Report, error) {
	args := []string{"diagnose", "--json"}
	if offline {
		args = append(args, "--offline")
	}
	res, err := r.Run(ctx, "", args...)
	if err != nil {
		return Report{}, err
	}
	if res.Exit != 0 {
		return Report{}, ErrMalformed
	}
	return ParseReport(res.Stdout)
}

// Tools는 `hooks status --json`이다. 설정 파일을 읽을 수 없으면 Core가 1로 끝나도 JSON은 있다.
func (r Runner) Tools(ctx context.Context) (ToolsReport, error) {
	res, err := r.Run(ctx, "", "hooks", "status", "--json")
	if err != nil {
		return ToolsReport{}, err
	}
	return ParseTools(res.Stdout)
}

// --- 입력 검증 ---------------------------------------------------------------

var (
	// ErrInvalidURL: 서버 주소 형식이 틀렸다.
	ErrInvalidURL = errors.New("invalid_server_url")
	// ErrInsecureURL: http는 loopback만 허용한다.
	ErrInsecureURL = errors.New("insecure_server_url")
	// ErrInvalidCode: 등록 코드 형식이 틀렸다.
	ErrInvalidCode = errors.New("invalid_registration_code")
	// ErrInvalidName: PC 이름이 비었거나 너무 길다.
	ErrInvalidName = errors.New("invalid_device_name")
)

// NormalizeServerURL은 Core `config.NormalizeServerURL`과 같은 규칙이다:
// https만, http는 localhost·127.x·::1만, 경로·쿼리·조각·사용자 정보 없음.
func NormalizeServerURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidURL
	}
	if p := strings.TrimRight(u.Path, "/"); p != "" {
		return "", ErrInvalidURL
	}
	switch u.Scheme {
	case "https":
	case "http":
		h := u.Hostname()
		if h != "localhost" && h != "::1" && !strings.HasPrefix(h, "127.") {
			return "", ErrInsecureURL
		}
	default:
		return "", ErrInvalidURL
	}
	return u.Scheme + "://" + u.Host, nil
}

// codeAlphabet은 서버 `REGISTRATION_CODE_ALPHABET`과 같다(혼동 문자 제외).
const codeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// NormalizeCode는 서버 `normalize_registration_code`와 같은 규칙으로 `XXXX-XXXX`를 만든다.
func NormalizeCode(raw string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if r == '-' || r == ' ' {
			continue
		}
		if !strings.ContainsRune(codeAlphabet, r) || b.Len() >= 8 {
			return "", ErrInvalidCode
		}
		b.WriteRune(r)
	}
	if b.Len() != 8 {
		return "", ErrInvalidCode
	}
	s := b.String()
	return s[:4] + "-" + s[4:], nil
}

// NormalizeName은 앞뒤 공백을 지우고 1~80자, 제어 문자 없음을 확인한다.
func NormalizeName(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(s); n < 1 || n > 80 || !utf8.ValidString(s) {
		return "", ErrInvalidName
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", ErrInvalidName
		}
	}
	return s, nil
}

// Register는 `register --server S --code-stdin --name N [--force]`다. 코드는 표준 입력으로만 넘긴다.
func (r Runner) Register(ctx context.Context, server, code, name string, force bool) (Result, error) {
	s, err := NormalizeServerURL(server)
	if err != nil {
		return Result{}, err
	}
	c, err := NormalizeCode(code)
	if err != nil {
		return Result{}, err
	}
	n, err := NormalizeName(name)
	if err != nil {
		return Result{}, err
	}
	args := []string{"register", "--server", s, "--code-stdin", "--name", n}
	if force {
		args = append(args, "--force")
	}
	return r.Run(ctx, c+"\n", args...)
}
