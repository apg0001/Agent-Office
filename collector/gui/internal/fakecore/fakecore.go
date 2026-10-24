// Package fakecore는 테스트용 가짜 Collector Core다. 테스트 바이너리가 AO_FAKE_CORE=1로
// 자기 자신을 다시 실행하면 Core처럼 동작한다. 실제 Core와의 계약은 app 통합 테스트가 확인한다.
package fakecore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	// EnvActive가 "1"이면 Main이 가짜 Core로 동작한다.
	EnvActive = "AO_FAKE_CORE"
	// EnvLog는 호출 인자·표준 입력을 JSON 줄로 남길 파일이다.
	EnvLog = "AO_FAKE_LOG"
	// EnvScenario는 동작 변형이다(leak, slow, garbage, unreadable).
	EnvScenario = "AO_FAKE_SCENARIO"
	// SentinelToken은 가짜 Core가 실수로 흘리는 토큰 형식 문자열이다(leak 시나리오).
	SentinelToken = "aod_SENTINEL_TOKEN_gui_DO_NOT_SHOW"
	// GoodCode는 등록이 성공하는 코드다.
	GoodCode = "ABCD-EFGH"
)

// Call은 기록된 호출 하나다.
type Call struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

// Env는 Runner.Env에 넣을 환경이다.
func Env(logPath, scenario string) []string {
	return append(os.Environ(), EnvActive+"=1", EnvLog+"="+logPath, EnvScenario+"="+scenario)
}

// ReadCalls는 기록된 호출을 읽는다.
func ReadCalls(path string) []Call {
	data, _ := os.ReadFile(path)
	var out []Call
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var c Call
		if json.Unmarshal([]byte(line), &c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// StatusJSON은 실제 Core `status --json` 형식의 예다.
const StatusJSON = `{
  "collector_version": "0.0.0-dev",
  "os": "linux",
  "arch": "arm64",
  "registration": "registered",
  "server_scheme": "https",
  "paused": false,
  "pause_state": "ok",
  "process_running": true,
  "run": {
    "schema_version": 1,
    "collector_version": "0.0.0-dev",
    "state": "running",
    "started_at": "2026-10-24T01:00:00Z",
    "updated_at": "2026-10-24T01:05:00Z",
    "watch_root": "available",
    "watched_files": 3,
    "queue_depth": 2,
    "counters": {"acked": 10, "enqueued_transcript": 12},
    "errors": [{"code": "http_5xx", "count": 2, "last_at": "2026-10-24T01:04:00Z"}],
    "last_send_ok_at": "2026-10-24T01:04:30Z",
    "last_heartbeat_ok_at": "2026-10-24T01:04:50Z"
  },
  "queue": {"depth": 2, "dropped_oldest": 0, "dead_letter": 1, "dead_letter_by_reason": {"http_4xx": 1}, "state": "ok"},
  "service": "installed"
}`

// ToolsJSON은 실제 Core `hooks status --json` 형식의 예다.
const ToolsJSON = `{
  "os": "linux",
  "tools": [
    {"tool": "claude", "detected": true, "version": "2.1.308", "support": "verified", "verified_mode": "print",
     "hook_install": "auto", "hook_state": "not_installed", "events_current": 0, "events_total": 9,
     "missing": 9, "modified": 0, "foreign": 1},
    {"tool": "codex", "detected": false, "support": "unverified", "hook_install": "print_only",
     "hook_state": "unknown", "events_current": 0, "events_total": 9, "missing": 0, "modified": 0, "foreign": 0}
  ]
}`

// Main은 가짜 Core 동작이다. EnvActive가 없으면 false를 돌려준다.
func Main() bool {
	if os.Getenv(EnvActive) != "1" {
		return false
	}
	os.Exit(run(os.Args[1:]))
	return true
}

func run(args []string) int {
	if len(args) == 0 {
		return 2
	}
	stdin := ""
	if len(args) > 0 && args[0] == "register" {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 1024))
		stdin = string(b)
	}
	if p := os.Getenv(EnvLog); p != "" {
		line, _ := json.Marshal(Call{Args: args, Stdin: stdin})
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
	scenario := os.Getenv(EnvScenario)
	if scenario == "slow" {
		time.Sleep(10 * time.Second)
	}
	joined := strings.Join(args, " ")
	switch {
	case scenario == "garbage" && (joined == "status --json" || strings.HasPrefix(joined, "hooks status")):
		fmt.Println("not json")
		return 0
	case joined == "status --json":
		fmt.Println(StatusJSON)
	case strings.HasPrefix(joined, "diagnose --json"):
		server := "reachable"
		if strings.HasSuffix(joined, "--offline") {
			server = "skipped"
		}
		fmt.Println(strings.Replace(StatusJSON, `"service": "installed"`, `"service": "installed", "go_version": "go1.24", "server": "`+server+`"`, 1))
	case joined == "hooks status --json":
		if scenario == "unreadable" {
			fmt.Println(strings.Replace(ToolsJSON, `"hook_state": "not_installed"`, `"hook_state": "unreadable", "error": "parse_error"`, 1))
			return 1
		}
		fmt.Println(ToolsJSON)
	case joined == "hooks install --tool claude":
		fmt.Println("대상: ~/.claude/settings.json\n변경 미리보기:\n  + SessionStart\n파일을 바꾸지 않았습니다.")
	case joined == "hooks install --tool claude --yes":
		fmt.Println("백업(0600): ~/.claude/settings.json.agent-office-backup-x\nHook을 설치했습니다.")
	case joined == "hooks uninstall --tool claude --yes":
		fmt.Println("Agent Office Hook만 제거했습니다.")
	case joined == "hooks print --tool codex":
		fmt.Println(`{"hooks":{}}`)
	case args[0] == "register":
		if scenario == "leak" {
			fmt.Fprintln(os.Stderr, "실패 "+SentinelToken)
			return 1
		}
		if strings.TrimSpace(stdin) != GoodCode {
			fmt.Fprintln(os.Stderr, "등록 코드가 잘못되었거나 만료·사용되었습니다. 웹에서 새 코드를 받으세요")
			return 1
		}
		fmt.Println("등록했습니다. 장치 ID 0d6b2a10-7a4c-4b8e-9f00-1234567890ab. " + SentinelToken)
	case joined == "pause" || joined == "resume":
	case joined == "service print":
		fmt.Println("[Unit]\nDescription=Agent Office Collector")
	case strings.HasPrefix(joined, "service install"):
		fmt.Println("자동 시작 정의를 설치했습니다: " + joined)
	default:
		fmt.Fprintln(os.Stderr, "usage")
		return 2
	}
	return 0
}
