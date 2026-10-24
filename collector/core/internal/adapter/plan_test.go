package adapter_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
)

// 계획 단계 문장 자리에 sentinel을 넣는다. 상태만 후보에 남아야 한다(#38).
func todoInput(statuses ...string) string {
	items := make([]string, len(statuses))
	for i, s := range statuses {
		items[i] = fmt.Sprintf(`{"content":"%[1]s todo %[2]d /home/%[1]s","activeForm":"%[1]s doing","status":%[3]s,"id":"%[1]s"}`, sentinel, i, s)
	}
	return `{"todos":[` + strings.Join(items, ",") + `]}`
}

func planInput(statuses ...string) string {
	items := make([]string, len(statuses))
	for i, s := range statuses {
		items[i] = fmt.Sprintf(`{"step":"%[1]s step %[2]d /home/%[1]s","status":%[3]s}`, sentinel, i, s)
	}
	return `{"explanation":"` + sentinel + `","plan":[` + strings.Join(items, ",") + `]}`
}

func claudeHook(event, tool, input string) []byte {
	return []byte(fmt.Sprintf(`{"hook_event_name":%q,"session_id":"s1","prompt_id":"p1","tool_use_id":"tu1","tool_name":%q,"tool_input":%s}`, event, tool, input))
}

var mixed = []string{`"completed"`, `"in_progress"`, `"pending"`, `"Pending"`, `"` + sentinel + `"`, `3`, `null`}
var mixedWant = []string{"completed", "in_progress", "pending", "unknown", "unknown", "unknown", "unknown"}

func TestClaudeTodoWriteHookPlan(t *testing.T) {
	rt := claude.VerifiedTargets[0].Runtime
	for _, ev := range []string{"PreToolUse", "PostToolUse"} {
		c, err := claude.ParseHook(claudeHook(ev, "TodoWrite", todoInput(mixed...)), rt)
		if err != nil || c.Plan == nil {
			t.Fatalf("%s: %v %+v", ev, err, c)
		}
		if !reflect.DeepEqual(c.Plan.Steps, mixedWant) || c.Plan.Support != adapter.SupportUnverified {
			t.Fatalf("%s: plan %+v", ev, c.Plan)
		}
		if c.EventType == adapter.EventPlanUpdated || c.ToolCategory != adapter.ToolPlan {
			t.Fatalf("%s: tool candidate must stay a tool event: %+v", ev, c)
		}
		assertNoSentinel(t, ev, c, err)
		p, ok := adapter.PlanUpdate(c)
		if !ok || p.EventType != adapter.EventPlanUpdated || p.Activity != adapter.ActivityPlanning ||
			p.Confidence != adapter.ConfidenceInferred || p.SessionRef != "s1" || p.ToolUseRef != "tu1" {
			t.Fatalf("%s: plan candidate %+v", ev, p)
		}
		if adapter.DedupKey("d", p) != "d|claude|s1|plan.updated|tu1" {
			t.Fatalf("dedup key %q", adapter.DedupKey("d", p))
		}
	}
	// 실패한 호출, 다른 도구, 비어 있거나 형식이 다른 입력은 계획을 만들지 않는다.
	for _, payload := range [][]byte{
		claudeHook("PostToolUseFailure", "TodoWrite", todoInput(`"pending"`)),
		claudeHook("PreToolUse", "Bash", todoInput(`"pending"`)),
		claudeHook("PreToolUse", "TodoWrite", `{"todos":[]}`),
		claudeHook("PreToolUse", "TodoWrite", `{"todos":"`+sentinel+`"}`),
		claudeHook("PreToolUse", "TodoWrite", `{"Todos":[{"status":"pending"}]}`),
		claudeHook("PreToolUse", "TodoWrite", `"`+sentinel+`"`),
		claudeHook("PermissionRequest", "TodoWrite", todoInput(`"pending"`)),
	} {
		c, err := claude.ParseHook(payload, rt)
		if c.Plan != nil {
			t.Fatalf("unexpected plan %+v", c.Plan)
		}
		if _, ok := adapter.PlanUpdate(c); ok {
			t.Fatal("unexpected plan candidate")
		}
		assertNoSentinel(t, "no plan", c, err)
	}
}

func TestPlanStepsCapAndNonObjects(t *testing.T) {
	statuses := make([]string, 60)
	for i := range statuses {
		statuses[i] = `"completed"`
	}
	c, err := claude.ParseHook(claudeHook("PreToolUse", "TodoWrite", todoInput(statuses...)), claude.VerifiedTargets[0].Runtime)
	if err != nil || c.Plan == nil || len(c.Plan.Steps) != adapter.MaxPlanSteps {
		t.Fatalf("cap: %v %+v", err, c.Plan)
	}
	obj, _ := adapter.DecodeObject([]byte(`{"todos":["`+sentinel+`",1,{"status":"pending"}]}`), 1<<10)
	if got := adapter.PlanSteps(obj, "todos"); !reflect.DeepEqual(got, []string{"unknown", "unknown", "pending"}) {
		t.Fatalf("steps %v", got)
	}
}

func TestClaudeTranscriptTodoWritePlan(t *testing.T) {
	line := `{"type":"assistant","sessionId":"s1","promptId":"p1","message":{"content":[{"type":"tool_use","id":"tu1","name":"TodoWrite","input":` +
		todoInput(`"pending"`, `"completed"`) + `},{"type":"tool_use","id":"tu2","name":"Bash","input":` + todoInput(`"pending"`) + `}]}}`
	cs, err := claude.ParseTranscriptLine([]byte(line), claude.VerifiedTargets[0].Runtime)
	if err != nil || len(cs) != 2 {
		t.Fatalf("%v %d", err, len(cs))
	}
	if cs[0].Plan == nil || !reflect.DeepEqual(cs[0].Plan.Steps, []string{"pending", "completed"}) || cs[1].Plan != nil {
		t.Fatalf("plans %+v %+v", cs[0].Plan, cs[1].Plan)
	}
	assertNoSentinel(t, "transcript", cs, err)
	hook, _ := claude.ParseHook(claudeHook("PreToolUse", "TodoWrite", todoInput(`"pending"`)), claude.VerifiedTargets[0].Runtime)
	hp, _ := adapter.PlanUpdate(hook)
	tp, _ := adapter.PlanUpdate(cs[0])
	if adapter.DedupKey("d", hp) != adapter.DedupKey("d", tp) {
		t.Fatal("hook and transcript plan keys must match")
	}
}

func TestCodexUpdatePlanHook(t *testing.T) {
	for _, rt := range []adapter.Runtime{codex.VerifiedTargets[0].Runtime, codex.VerifiedTargets[1].Runtime, {}} {
		payload := []byte(`{"hook_event_name":"PreToolUse","session_id":"s1","turn_id":"u1","tool_use_id":"c1","tool_name":"update_plan","tool_input":` + planInput(mixed...) + `}`)
		c, err := codex.ParseHook(payload, rt)
		if err != nil || c.Plan == nil || !reflect.DeepEqual(c.Plan.Steps, mixedWant) || c.Plan.Support != adapter.SupportUnverified {
			t.Fatalf("%+v: %v %+v", rt, err, c.Plan)
		}
		assertNoSentinel(t, "codex", c, err)
		p, ok := adapter.PlanUpdate(c)
		if !ok || p.Confidence != adapter.ConfidenceInferred || adapter.DedupKey("d", p) != "d|codex|s1|plan.updated|c1" {
			t.Fatalf("plan candidate %+v", p)
		}
	}
	// 문자열로 인코딩된 arguments는 해석하지 않는다.
	c, err := codex.ParseHook([]byte(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_use_id":"c1","tool_name":"update_plan","tool_input":"{\"plan\":[{\"status\":\"pending\"}]}"}`), codex.VerifiedTargets[0].Runtime)
	if err != nil || c.Plan != nil {
		t.Fatalf("string input: %v %+v", err, c.Plan)
	}
}

func FuzzPlanParsers(f *testing.F) {
	f.Add([]byte(todoInput(`"pending"`, `"completed"`)))
	f.Add([]byte(planInput(`"in_progress"`, `7`)))
	f.Add([]byte(`{"todos":[1,{"status":{}}],"plan":null}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		rt := claude.VerifiedTargets[0].Runtime
		check := func(c adapter.Candidate) {
			if c.Plan == nil {
				return
			}
			if len(c.Plan.Steps) == 0 || len(c.Plan.Steps) > adapter.MaxPlanSteps {
				t.Fatalf("steps len %d", len(c.Plan.Steps))
			}
			for _, s := range c.Plan.Steps {
				if adapter.PlanStatusOf(s) != s {
					t.Fatalf("status outside enum")
				}
			}
		}
		c, _ := claude.ParseHook(claudeHook("PreToolUse", "TodoWrite", string(input)), rt)
		check(c)
		c, _ = codex.ParseHook([]byte(`{"hook_event_name":"PreToolUse","session_id":"s","tool_use_id":"c","tool_name":"update_plan","tool_input":`+string(input)+`}`), rt)
		check(c)
		cs, _ := claude.ParseTranscriptLine([]byte(`{"type":"assistant","sessionId":"s","message":{"content":[{"type":"tool_use","id":"t","name":"TodoWrite","input":`+string(input)+`}]}}`), rt)
		for _, c := range cs {
			check(c)
		}
	})
}
