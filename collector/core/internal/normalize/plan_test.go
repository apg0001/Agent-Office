package normalize

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"
)

// 할 일·계획 문장 자리에 sentinel을 넣는다(#38). 출력에는 상태 enum만 남아야 한다.
func planTodos(statuses ...string) string {
	items := make([]string, len(statuses))
	for i, s := range statuses {
		items[i] = fmt.Sprintf(`{"content":"%[1]s todo %[2]d /home/%[1]s/x","activeForm":"%[1]s doing","status":%[3]q}`, sentinel, i, s)
	}
	return `{"todos":[` + strings.Join(items, ",") + `]}`
}

func todoHook(event, toolUse string, statuses ...string) []byte {
	return []byte(fmt.Sprintf(`{"hook_event_name":%q,"session_id":"sess-1","prompt_id":"p-1","tool_use_id":%q,"tool_name":"TodoWrite","tool_input":%s,"tool_response":"%s"}`,
		event, toolUse, planTodos(statuses...), sentinel))
}

func todoTranscript(toolUse string, statuses ...string) tail.Result {
	line := fmt.Sprintf(`{"type":"assistant","sessionId":"sess-1","promptId":"p-1","timestamp":"2026-10-23T01:02:00Z","message":{"content":[{"type":"tool_use","id":%q,"name":"TodoWrite","input":%s}]}}`,
		toolUse, planTodos(statuses...))
	return tail.Result{Records: [][]byte{[]byte(line)}}
}

func byType(recs []Record) map[string][]Event {
	out := map[string][]Event{}
	for _, r := range recs {
		out[r.Event.EventType] = append(out[r.Event.EventType], r.Event)
	}
	return out
}

func TestPipelineEmitsToolAndPlanEvents(t *testing.T) {
	schema := loadSchema(t)
	p := NewPipeline(newNormalizer(t, Options{}))
	recs := p.Hook(adapter.ProviderClaude, claudeRuntime, todoHook("PreToolUse", "tu-1", "completed", "in_progress", "pending", "blocked"), observed)
	if len(recs) != 2 {
		t.Fatalf("records %d", len(recs))
	}
	tool, plan := recs[0].Event, recs[1].Event
	if tool.EventType != "tool.started" || tool.PlanSteps != nil {
		t.Fatalf("tool event %+v", tool)
	}
	if plan.EventType != "plan.updated" || plan.Activity != "planning" || plan.Confidence != "inferred" ||
		!reflect.DeepEqual(plan.PlanSteps, []string{"completed", "in_progress", "pending", "unknown"}) {
		t.Fatalf("plan event %+v", plan)
	}
	if plan.SessionID != tool.SessionID || plan.AgentID != tool.AgentID || plan.Sequence != tool.Sequence+1 ||
		!recs[1].DeterministicID || plan.EventID == tool.EventID {
		t.Fatalf("plan must share session/agent with its own deterministic id: %+v %+v", tool, plan)
	}
	for _, r := range recs {
		if err := validateEvent(schema, mustMarshal(t, r.Event)); err != nil {
			t.Fatalf("schema: %v", err)
		}
		assertClean(t, "plan record", r)
	}
	assertClean(t, "counters", p.Counters())
}

func TestPlanDedupAcrossHookAndTranscript(t *testing.T) {
	for _, hooksFirst := range []bool{true, false} {
		p := NewPipeline(newNormalizer(t, Options{}))
		hooks := func() []Record {
			out := p.Hook(adapter.ProviderClaude, claudeRuntime, todoHook("PreToolUse", "tu-1", "pending", "pending"), observed)
			return append(out, p.Hook(adapter.ProviderClaude, claudeRuntime, todoHook("PostToolUse", "tu-1", "pending", "pending"), observed)...)
		}
		transcript := func() []Record {
			return p.Transcript(todoTranscript("tu-1", "pending", "pending"), claudeRuntime, observed)
		}
		var recs []Record
		if hooksFirst {
			recs = append(hooks(), transcript()...)
		} else {
			recs = append(transcript(), hooks()...)
		}
		got := byType(recs)
		if len(got["plan.updated"]) != 1 || len(got["tool.started"]) != 1 || len(got["tool.completed"]) != 1 {
			t.Fatalf("hooksFirst=%v: %d plan, %d started, %d completed", hooksFirst,
				len(got["plan.updated"]), len(got["tool.started"]), len(got["tool.completed"]))
		}
		// tool.started 1 + plan.updated 2(Post·다른 소스)가 중복으로 걸러진다.
		if c := p.Counters(); c.DuplicateSuppressed != 3 {
			t.Fatalf("hooksFirst=%v suppressed %d", hooksFirst, c.DuplicateSuppressed)
		}
		// 다른 TodoWrite 호출은 새 계획이다.
		next := p.Hook(adapter.ProviderClaude, claudeRuntime, todoHook("PreToolUse", "tu-2", "completed", "completed"), observed)
		if g := byType(next)["plan.updated"]; len(g) != 1 || !reflect.DeepEqual(g[0].PlanSteps, []string{"completed", "completed"}) {
			t.Fatalf("second plan %+v", next)
		}
	}
}

func TestCodexUpdatePlanPipeline(t *testing.T) {
	schema := loadSchema(t)
	payload := []byte(`{"hook_event_name":"PreToolUse","session_id":"sess-c","turn_id":"u-1","tool_use_id":"call-1","tool_name":"update_plan","tool_input":{"explanation":"` +
		sentinel + `","plan":[{"step":"` + sentinel + ` /home/x","status":"in_progress"},{"step":"` + sentinel + `","status":"pending"}]}}`)
	for _, rt := range []adapter.Runtime{codex.VerifiedTargets[0].Runtime, codex.VerifiedTargets[1].Runtime} {
		p := NewPipeline(newNormalizer(t, Options{}))
		recs := p.Hook(adapter.ProviderCodex, rt, payload, observed)
		got := byType(recs)
		if len(recs) != 2 || len(got["tool.started"]) != 1 || len(got["plan.updated"]) != 1 {
			t.Fatalf("records %+v", recs)
		}
		plan := got["plan.updated"][0]
		if plan.Provider != "codex" || plan.Confidence != "inferred" || !reflect.DeepEqual(plan.PlanSteps, []string{"in_progress", "pending"}) {
			t.Fatalf("plan %+v", plan)
		}
		for _, r := range recs {
			if err := validateEvent(schema, mustMarshal(t, r.Event)); err != nil {
				t.Fatalf("schema: %v", err)
			}
			assertClean(t, "codex plan", r)
		}
		// CLI PostToolUse는 unknown으로 낮아지므로 계획도 만들지 않는다.
		post := []byte(strings.Replace(string(payload), "PreToolUse", "PostToolUse", 1))
		for _, r := range p.Hook(adapter.ProviderCodex, codex.VerifiedTargets[0].Runtime, post, observed) {
			if r.Event.EventType == "plan.updated" || r.Event.EventType == "tool.completed" {
				t.Fatalf("CLI PostToolUse emitted %s", r.Event.EventType)
			}
		}
	}
}

func TestPlanStepsOnlyOnPlanUpdated(t *testing.T) {
	n := newNormalizer(t, Options{})
	plan := &adapter.Plan{Steps: []string{"pending", sentinel}, Support: adapter.SupportVerified}
	base := adapter.Candidate{
		Provider: adapter.ProviderClaude, Source: adapter.SourceHook, EventType: adapter.EventToolStarted,
		SessionRef: "s", ToolUseRef: "t", Activity: adapter.ActivityUnknown, Confidence: adapter.ConfidenceObserved,
		Support: adapter.SupportVerified, Plan: plan,
	}
	rec, out := n.Normalize(Input{Candidate: base, ObservedAt: observed})
	if out != OutcomeAccepted || rec.Event.PlanSteps != nil {
		t.Fatalf("tool event carries plan: %+v", rec.Event)
	}
	pc, ok := adapter.PlanUpdate(base)
	if !ok {
		t.Fatal("plan candidate")
	}
	// 어댑터를 거치지 않은 enum 밖 상태와 50개 초과도 2차 방어선에서 걸러진다.
	pc.Plan = &adapter.Plan{Steps: append([]string{sentinel}, make([]string, 60)...), Support: adapter.SupportVerified}
	rec, out = n.Normalize(Input{Candidate: pc, ObservedAt: observed})
	if out != OutcomeAccepted || len(rec.Event.PlanSteps) != adapter.MaxPlanSteps || rec.Event.PlanSteps[0] != "unknown" {
		t.Fatalf("sanitize: %s %d", out, len(rec.Event.PlanSteps))
	}
	if err := validateEvent(loadSchema(t), mustMarshal(t, rec.Event)); err != nil {
		t.Fatalf("schema: %v", err)
	}
	assertClean(t, "sanitized", rec)
}
