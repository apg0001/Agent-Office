package adapter_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
)

// sentinel은 민감 필드 값에만 넣는다. 어떤 후보·오류 문자열에도 나타나면 안 된다.
const sentinel = "SENTINELx9q"

// sensitiveFields는 Claude·Codex Hook에서 관측·문서화된 원문 필드다.
var sensitiveFields = []string{
	"prompt", "tool_input", "tool_response", "error", "last_assistant_message", "cwd",
	"transcript_path", "agent_transcript_path", "scratchpad_dir", "model", "message",
	"background_tasks", "session_crons", "description", "command", "reason", "source",
	"permission_mode", "effort", "unregistered_future_field",
}

func sensitivePayload(event string, extra string) []byte {
	var b strings.Builder
	b.WriteString(`{"hook_event_name":"` + event + `","session_id":"s1","prompt_id":"p1","turn_id":"u1","tool_use_id":"t1","agent_id":"a1"`)
	// 원문 필드 이름과 값, 도구 이름·하위 에이전트 유형 원문에 모두 sentinel을 넣는다.
	b.WriteString(`,"tool_name":"` + sentinel + `Tool","agent_type":"` + sentinel + `Agent"`)
	for _, f := range sensitiveFields {
		fmt.Fprintf(&b, `,%q:{"text":"%s","command":"%s /home/%s","items":[{"prompt":"%s"}]}`, f, sentinel, sentinel, sentinel, sentinel)
	}
	b.WriteString(extra + `}`)
	return []byte(b.String())
}

func assertNoSentinel(t *testing.T, label string, v any, err error) {
	t.Helper()
	out := fmt.Sprintf("%#v", v)
	if err != nil {
		out += err.Error()
	}
	if strings.Contains(out, sentinel) || strings.Contains(out, "/home/") {
		t.Fatalf("%s: sensitive value leaked", label)
	}
}

var hookEvents = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse",
	"PostToolUseFailure", "PermissionRequest", "SubagentStart", "SubagentStop", "Notification",
	sentinel,
}

func TestHookOutputsNeverContainSensitiveValues(t *testing.T) {
	runtimes := []adapter.Runtime{claude.VerifiedTargets[0].Runtime, codex.VerifiedTargets[0].Runtime, codex.VerifiedTargets[1].Runtime, {}}
	for _, event := range hookEvents {
		payload := sensitivePayload(event, "")
		for _, rt := range runtimes {
			c, err := claude.ParseHook(payload, rt)
			assertNoSentinel(t, "claude "+event, c, err)
			if err == nil && (c.ToolCategory == "" && strings.Contains(event, "Tool") && event != "PermissionRequest") {
				t.Fatalf("tool category missing for %s", event)
			}
			c, err = codex.ParseHook(payload, rt)
			assertNoSentinel(t, "codex "+event, c, err)
		}
	}
}

func TestIdentifierFieldsRejectRawValues(t *testing.T) {
	raw := `"` + sentinel + ` /home/user/project"`
	for _, key := range []string{"session_id", "prompt_id", "turn_id", "tool_use_id", "agent_id"} {
		for _, event := range []string{"PreToolUse", "SubagentStart", "UserPromptSubmit"} {
			payload := []byte(strings.Replace(string(sensitivePayload(event, "")), `"`+key+`":"`, `"`+key+`":`+raw+`,"_old_`+key+`":"`, 1))
			c, err := claude.ParseHook(payload, adapter.Runtime{})
			assertNoSentinel(t, "claude "+key, c, err)
			c, err = codex.ParseHook(payload, adapter.Runtime{})
			assertNoSentinel(t, "codex "+key, c, err)
		}
	}
}

func TestTranscriptOutputsNeverContainSensitiveValues(t *testing.T) {
	lines := []string{
		`{"type":"user","sessionId":"s1","promptId":"p1","cwd":"/home/` + sentinel + `","message":{"role":"user","content":"` + sentinel + `"}}`,
		`{"type":"assistant","sessionId":"s1","message":{"content":[{"type":"text","text":"` + sentinel + `"},{"type":"thinking","thinking":"` + sentinel + `"},{"type":"tool_use","id":"t1","name":"` + sentinel + `","input":{"command":"` + sentinel + `"}}]}}`,
		`{"type":"user","sessionId":"s1","agentId":"a1","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":[{"type":"text","text":"` + sentinel + `"}]}]}}`,
		`{"type":"user","sessionId":"` + sentinel + ` x","message":{"content":"` + sentinel + `"}}`,
		`{"type":"assistant","sessionId":"s1","message":{"content":[{"type":"tool_use","id":"` + sentinel + `/x"}]}}`,
		`{"type":"user","sessionId":"s1","message":{"content":"` + sentinel,
		`{"type":"api-request-blob","sessionId":"s1","message":{"content":"` + sentinel + `"}}`,
	}
	for i, line := range lines {
		cs, err := claude.ParseTranscriptLine([]byte(line), adapter.Runtime{})
		assertNoSentinel(t, fmt.Sprintf("line %d", i), cs, err)
	}
}

func TestMalformedInputErrorsDoNotEchoInput(t *testing.T) {
	inputs := []string{
		sentinel,
		`{"session_id":"s1","prompt":"` + sentinel,
		`{"session_id":` + sentinel + `}`,
		`["` + sentinel + `"]`,
		"\xff\xfe" + sentinel,
	}
	for _, in := range inputs {
		_, err := claude.ParseHook([]byte(in), adapter.Runtime{})
		assertNoSentinel(t, "claude malformed", nil, err)
		_, err = codex.ParseHook([]byte(in), adapter.Runtime{})
		assertNoSentinel(t, "codex malformed", nil, err)
		cs, err := claude.ParseTranscriptLine([]byte(in), adapter.Runtime{})
		assertNoSentinel(t, "transcript malformed", cs, err)
	}
}
