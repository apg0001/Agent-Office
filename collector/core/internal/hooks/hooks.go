// Package hooks는 Claude Code·Codex Hook 설정 항목을 생성하고, 사용자 설정 파일에
// 소유 항목만 안전하게 추가·제거한다(#31).
//
//   - 생성(Generate)은 순수 함수다. OS별 명령 인용을 골든 테스트로 고정한다.
//   - 소유 판정은 Hook 핸들러의 명령 서명(실행 파일 이름 `agent-office-collector[.exe]` +
//     `hook <provider>`)으로 한다. Claude 설정 검증이 모르는 키를 거부할 수 있으므로
//     핸들러에 별도 표시 키를 넣지 않는다.
//   - 설치·제거는 기존 키 순서와 남의 Hook을 보존하고, 쓰기 전에 0600 백업을 남기며,
//     임시 파일 + rename으로 원자적으로 쓴다. 파싱할 수 없는 파일은 건드리지 않는다.
package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// Tool 값.
const (
	ToolClaude = "claude"
	ToolCodex  = "codex"
)

// TimeoutSeconds는 Hook 핸들러 타임아웃이다. Claude 기본값(600초)은 도구 호출마다 지연을
// 더하므로(#19 실측) 짧게 명시한다. `hook` 명령은 stdin을 최대 2초만 기다린다.
const TimeoutSeconds = 5

// ExeBase는 소유 판정에 쓰는 실행 파일 이름(확장자 제외)이다.
const ExeBase = "agent-office-collector"

var (
	// ErrInvalidExecutable은 실행 파일 경로가 절대 경로가 아니거나 안전하게 인용할 수 없을 때다.
	ErrInvalidExecutable = errors.New("hooks: executable path must be absolute and quotable")
	// ErrUnsupportedTool은 알 수 없는 도구다.
	ErrUnsupportedTool = errors.New("hooks: unsupported tool")
	// ErrUnsupportedOS는 알 수 없는 OS다.
	ErrUnsupportedOS = errors.New("hooks: unsupported operating system")
)

// Event는 등록할 Hook 이벤트 하나다.
type Event struct {
	Name string
	// Matcher가 비어 있지 않으면 matcher 그룹에 넣는다(Claude 도구 이벤트, #19 하네스와 동일).
	Matcher string
}

// claudeEvents는 #19(Claude Code 2.1.308, Linux arm64, -p)에서 반복 관측한 이벤트다.
// PostToolUseFailure·Notification·PreCompact는 등록해도 미관측이라 넣지 않는다.
var claudeEvents = []Event{
	{Name: "SessionStart"},
	{Name: "UserPromptSubmit"},
	{Name: "PreToolUse", Matcher: "*"},
	{Name: "PostToolUse", Matcher: "*"},
	{Name: "PermissionRequest", Matcher: "*"},
	{Name: "SubagentStart"},
	{Name: "SubagentStop"},
	{Name: "Stop"},
	{Name: "SessionEnd"},
}

// codexEvents는 #17(CLI exec)·#18(Desktop, 부분 관측)에서 관측한 이벤트의 합집합이다.
var codexEvents = []Event{
	{Name: "SessionStart"},
	{Name: "UserPromptSubmit"},
	{Name: "PreToolUse"},
	{Name: "PostToolUse"},
	{Name: "PermissionRequest"},
	{Name: "SubagentStart"},
	{Name: "SubagentStop"},
	{Name: "Stop"},
	{Name: "SessionEnd"},
}

// Params는 생성 입력이다.
type Params struct {
	Tool string
	GOOS string
	// Exe는 Collector 실행 파일 절대 경로다.
	Exe string
}

// Spec은 생성된 Hook 항목 집합이다.
type Spec struct {
	Tool    string
	GOOS    string
	Command string
	Timeout int
	Events  []Event
}

// Generate는 Hook 항목을 만든다. 파일을 쓰지 않는다.
func Generate(p Params) (Spec, error) {
	var events []Event
	switch p.Tool {
	case ToolClaude:
		events = claudeEvents
	case ToolCodex:
		events = codexEvents
	default:
		return Spec{}, ErrUnsupportedTool
	}
	cmd, err := Command(p.GOOS, p.Exe, p.Tool)
	if err != nil {
		return Spec{}, err
	}
	return Spec{Tool: p.Tool, GOOS: p.GOOS, Command: cmd, Timeout: TimeoutSeconds, Events: events}, nil
}

// Command는 `<exe> hook <tool>` 셸 명령 문자열이다.
//
//   - Linux·macOS: 실행 파일을 작은따옴표로 인용한다(작은따옴표 자체는 닫고-이스케이프-다시 열기).
//     Hook 명령은 sh로 실행된다.
//   - Windows: 구분자를 슬래시로 바꾸고 큰따옴표로 인용한다. cmd.exe와 Git Bash 모두 같은 뜻으로
//     읽도록 큰따옴표, 퍼센트, 느낌표, 달러, 백틱이 든 경로는 거부한다(실기 미검증).
func Command(goos, exe, tool string) (string, error) {
	if exe == "" || strings.ContainsAny(exe, "\x00\r\n\t") {
		return "", ErrInvalidExecutable
	}
	switch goos {
	case "linux", "darwin":
		if !strings.HasPrefix(exe, "/") {
			return "", ErrInvalidExecutable
		}
		return shQuote(exe) + " hook " + tool, nil
	case "windows":
		if len(exe) < 3 || exe[1] != ':' || (exe[2] != '\\' && exe[2] != '/') || strings.ContainsAny(exe, "\"%!$`") {
			return "", ErrInvalidExecutable
		}
		return `"` + strings.ReplaceAll(exe, `\`, "/") + `" hook ` + tool, nil
	default:
		return "", ErrUnsupportedOS
	}
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// handler는 Hook 핸들러 JSON이다(키 순서 고정).
type handler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

func (s Spec) handlerJSON() json.RawMessage {
	return encode(handler{Type: "command", Command: s.Command, Timeout: s.Timeout})
}

// groupJSON은 이벤트 하나에 추가할 matcher 그룹이다.
func (s Spec) groupJSON(e Event) json.RawMessage {
	var obj object
	if e.Matcher != "" {
		obj = obj.set("matcher", encode(e.Matcher))
	}
	obj = obj.set("hooks", json.RawMessage("["+string(s.handlerJSON())+"]"))
	return obj.marshal()
}

// JSON은 이 항목만 담은 `{"hooks": {...}}` 문서다(print·골든 테스트·Codex 수동 설치용).
func (s Spec) JSON() []byte {
	var hooksObj object
	for _, e := range s.Events {
		hooksObj = hooksObj.set(e.Name, json.RawMessage("["+string(s.groupJSON(e))+"]"))
	}
	doc := object{}.set("hooks", hooksObj.marshal())
	return pretty(doc.marshal())
}

// IsOwnedCommand는 명령이 이 도구가 만든 `hook <tool>` 호출인지 판정한다.
// 실행 파일 위치가 바뀐 이전 설치도 소유로 본다.
func IsOwnedCommand(cmd, tool string) bool {
	prog, rest, ok := splitProgram(strings.TrimSpace(cmd))
	if !ok {
		return false
	}
	base := prog
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	if base != ExeBase {
		return false
	}
	f := strings.Fields(rest)
	return len(f) >= 2 && f[0] == "hook" && f[1] == tool
}

// splitProgram은 명령의 첫 토큰(인용 해제)과 나머지를 나눈다.
func splitProgram(cmd string) (prog, rest string, ok bool) {
	if cmd == "" {
		return "", "", false
	}
	switch cmd[0] {
	case '\'':
		var b strings.Builder
		i := 1
		for i < len(cmd) {
			if cmd[i] == '\'' {
				if strings.HasPrefix(cmd[i:], `'\''`) {
					b.WriteByte('\'')
					i += 4
					continue
				}
				return b.String(), cmd[i+1:], true
			}
			b.WriteByte(cmd[i])
			i++
		}
		return "", "", false
	case '"':
		j := strings.IndexByte(cmd[1:], '"')
		if j < 0 {
			return "", "", false
		}
		return cmd[1 : 1+j], cmd[2+j:], true
	default:
		j := strings.IndexAny(cmd, " \t")
		if j < 0 {
			return cmd, "", true
		}
		return cmd[:j], cmd[j:], true
	}
}

func encode(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return json.RawMessage(bytes.TrimRight(b.Bytes(), "\n"))
}

func pretty(compact []byte) []byte {
	var b bytes.Buffer
	if err := json.Indent(&b, compact, "", "  "); err != nil {
		return compact
	}
	b.WriteByte('\n')
	return b.Bytes()
}
