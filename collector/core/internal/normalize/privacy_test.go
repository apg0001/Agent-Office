package normalize

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"
)

// sentinel은 식별자 형식도 통과하므로 원본 ID 자리에 넣으면 UUID 매핑이 실제로 감추는지 확인된다.
const sentinel = "SENTINELq7z"

var hookNames = []string{
	"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse",
	"PostToolUseFailure", "PermissionRequest", "SubagentStart", "SubagentStop", "Notification", sentinel,
}

// 원본 ID 필드(식별자 형식)와 원문 필드(본문·경로·명령)에 모두 sentinel을 넣는다.
func sentinelHook(event string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `{"hook_event_name":%q`, event)
	for _, id := range []string{"session_id", "prompt_id", "turn_id", "tool_use_id", "agent_id"} {
		fmt.Fprintf(&b, `,%q:"%s-%s"`, id, sentinel, id)
	}
	for _, f := range []string{
		"tool_name", "agent_type", "prompt", "tool_input", "tool_response", "error", "last_assistant_message",
		"cwd", "transcript_path", "agent_transcript_path", "model", "source", "reason", "permission_mode",
		"message", "background_tasks", "session_crons", "timestamp", "unregistered_future_field",
	} {
		fmt.Fprintf(&b, `,%q:"%s /home/%s/secret.txt"`, f, sentinel, sentinel)
	}
	fmt.Fprintf(&b, `,"nested":{"command":"%s","items":[{"prompt":"%s"}]}}`, sentinel, sentinel)
	return []byte(b.String())
}

func sentinelTranscript() [][]byte {
	ids := func(extra string) string {
		return fmt.Sprintf(`"sessionId":"%[1]s-s","promptId":"%[1]s-p","uuid":"%[1]s-u","cwd":"/home/%[1]s","timestamp":"%[1]s"%[2]s`, sentinel, extra)
	}
	return [][]byte{
		[]byte(`{"type":"user",` + ids("") + `,"message":{"content":"` + sentinel + `"}}`),
		[]byte(`{"type":"assistant",` + ids(`,"agentId":"`+sentinel+`-a"`) + `,"message":{"content":[{"type":"tool_use","id":"` + sentinel + `-t","name":"` + sentinel + `","input":{"command":"` + sentinel + `"}},{"type":"text","text":"` + sentinel + `"}]}}`),
		[]byte(`{"type":"user",` + ids("") + `,"message":{"content":[{"type":"tool_result","tool_use_id":"` + sentinel + `-t","is_error":"` + sentinel + `","content":"` + sentinel + `"}]}}`),
		[]byte(`{"type":"` + sentinel + `",` + ids("") + `,"message":{"content":"` + sentinel + `"}}`),
		[]byte(`{"type":"assistant","sessionId":"` + sentinel + ` with space","message":{}}`),
	}
}

func assertClean(t *testing.T, label string, v ...any) {
	t.Helper()
	for _, x := range v {
		var out string
		switch val := x.(type) {
		case error:
			if val != nil {
				out = val.Error()
			}
		default:
			raw, _ := json.Marshal(val)
			out = string(raw) + fmt.Sprintf("%#v", val)
		}
		if strings.Contains(out, sentinel) || strings.Contains(out, "/home/") {
			t.Fatalf("%s: sensitive value leaked", label)
		}
	}
}

func TestSentinelNeverReachesEventsCountersOrErrors(t *testing.T) {
	schema := loadSchema(t)
	runtimes := []adapter.Runtime{
		claudeRuntime, codex.VerifiedTargets[0].Runtime, codex.VerifiedTargets[1].Runtime, {},
		{Product: sentinel, Version: sentinel, OS: sentinel, Mode: sentinel},
	}
	p := NewPipeline(newNormalizer(t, Options{}))
	var recs []Record
	for _, rt := range runtimes {
		for _, name := range hookNames {
			for _, prov := range []adapter.Provider{adapter.ProviderClaude, adapter.ProviderCodex, adapter.Provider(sentinel)} {
				recs = append(recs, p.Hook(prov, rt, sentinelHook(name), observed)...)
			}
		}
		recs = append(recs, p.Transcript(tail.Result{Records: sentinelTranscript()}, rt, observed)...)
	}
	if len(recs) == 0 {
		t.Fatal("expected some accepted events")
	}
	for _, r := range recs {
		raw := mustMarshal(t, r.Event)
		if err := validateEvent(schema, raw); err != nil {
			t.Fatalf("schema: %v", err)
		}
		assertClean(t, "record", r)
	}
	assertClean(t, "counters", p.Counters())
	if p.Counters().DroppedInvalid == 0 || p.Counters().IgnoredLines == 0 {
		t.Fatalf("expected drops: %+v", p.Counters())
	}

	// 어댑터를 거치지 않은 후보의 모든 문자열 필드에 sentinel을 넣어도 enum 밖 값은 통과하지 않는다.
	n := newNormalizer(t, Options{})
	all := adapter.Candidate{
		Provider: adapter.Provider(sentinel), Source: adapter.SourceHook, EventType: adapter.EventType(sentinel),
		SessionRef: sentinel, TurnRef: sentinel, ToolUseRef: sentinel, AgentRef: sentinel,
		ToolCategory: adapter.ToolCategory(sentinel), Activity: adapter.Activity(sentinel),
		AgentType: adapter.AgentTypeCategory(sentinel), Confidence: adapter.Confidence(sentinel),
		Support: adapter.SupportLevel(sentinel),
	}
	rec, out := n.Normalize(Input{Candidate: all, ObservedAt: observed})
	if out != OutcomeAccepted || rec.Event.EventType != "unknown" || rec.Event.Confidence != "inferred" {
		t.Fatalf("outcome %s %+v", out, rec.Event)
	}
	assertClean(t, "direct", rec)
	for _, mutate := range []func(*adapter.Candidate){
		func(c *adapter.Candidate) { c.Source = adapter.Source(sentinel) },
		func(c *adapter.Candidate) { c.SessionRef = sentinel + " /home/x" },
		func(c *adapter.Candidate) { c.AgentRef = sentinel + "/x" },
	} {
		c := all
		mutate(&c)
		rec, _ := n.Normalize(Input{Candidate: c, ObservedAt: observed})
		assertClean(t, "dropped", rec)
	}
	assertClean(t, "direct counters", n.Counters())
	_, err := New(Options{DeviceID: sentinel})
	assertClean(t, "new error", err)
	_, err = claude.ParseHook(sentinelHook("PreToolUse"), claudeRuntime)
	assertClean(t, "adapter error", err)
}
