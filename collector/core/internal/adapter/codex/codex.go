// Package codex는 Codex CLI·Desktop Hook stdin payload를 제공자 중립 후보로 바꾼다.
//
// Codex transcript 내부 형식은 안정 계약이 아니므로(#17, ADR-0002) 이 패키지는 읽지 않는다.
// prompt, tool_input, tool_response, last_assistant_message, cwd, transcript_path,
// agent_transcript_path, model 값은 어떤 후보 필드에도 복사하지 않는다.
package codex

import (
	"strings"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

const (
	hookSessionStart      = "SessionStart"
	hookSessionEnd        = "SessionEnd"
	hookUserPromptSubmit  = "UserPromptSubmit"
	hookStop              = "Stop"
	hookPreToolUse        = "PreToolUse"
	hookPostToolUse       = "PostToolUse"
	hookPermissionRequest = "PermissionRequest"
	hookSubagentStart     = "SubagentStart"
	hookSubagentStop      = "SubagentStop"

	// ProductCLI와 ProductDesktop은 Runtime.Product 값이다.
	ProductCLI     = "codex-cli"
	ProductDesktop = "codex-desktop"

	// planInputEvent는 update_plan 입력(tool_input.plan) 형식의 검증 수준을 조회하는 가상
	// 이벤트 이름이다. #17·#18 실측에서 update_plan Hook payload를 관측하지 않았으므로 어떤 대상에도
	// 없고 계획 후보는 항상 unverified → inferred다.
	planInputEvent = "plan:update_plan"
	planTool       = "update_plan"
)

// VerifiedTargets는 실측 런타임이다.
//   - CLI exec(#17): 4회 반복 관측, PostToolUse는 4회 모두 미관측.
//   - Desktop(#18): 1회 관측이라 partial. SessionStart는 미관측.
var VerifiedTargets = []adapter.Target{
	{
		Runtime: adapter.Runtime{Product: ProductCLI, Version: "0.162.0-alpha.17.2", OS: "windows", Mode: "exec"},
		Level:   adapter.SupportVerified,
		Observed: adapter.Set(
			hookSessionStart, hookUserPromptSubmit, hookPreToolUse, hookStop, hookSessionEnd,
		),
	},
	{
		Runtime: adapter.Runtime{Product: ProductDesktop, Version: "26.1007.2314.0", OS: "windows", Mode: "desktop"},
		Level:   adapter.SupportPartial,
		Observed: adapter.Set(
			hookUserPromptSubmit, hookPreToolUse, hookPostToolUse, hookPermissionRequest,
			hookSubagentStart, hookSubagentStop, hookStop, hookSessionEnd,
		),
	},
}

var hookEventTypes = map[string]adapter.EventType{
	hookSessionStart:      adapter.EventSessionStarted,
	hookSessionEnd:        adapter.EventSessionEnded,
	hookUserPromptSubmit:  adapter.EventTurnStarted,
	hookStop:              adapter.EventTurnCompleted,
	hookPreToolUse:        adapter.EventToolStarted,
	hookPostToolUse:       adapter.EventToolCompleted,
	hookPermissionRequest: adapter.EventApprovalRequested,
	hookSubagentStart:     adapter.EventAgentStarted,
	hookSubagentStop:      adapter.EventAgentCompleted,
}

// ParseHook은 Codex Hook stdin payload 한 건을 후보 하나로 바꾼다.
//
//   - 알 수 없는 이벤트, 그리고 Desktop이 아닌 런타임(CLI·미상)의 PostToolUse는 EventUnknown
//     후보다. CLI exec에서 PostToolUse가 관측되지 않았으므로 Hook만으로 tool.completed를 만들지 않는다.
//   - session_id가 없으면 ErrMissingSession, 도구·하위 에이전트 이벤트에 식별자가 없으면 ErrMissingRef.
//
// 턴 참조는 `turn_id`다. Codex 키는 Hook 단일 소스 멱등용이며 교차 소스 일치는 미검증이다.
func ParseHook(data []byte, rt adapter.Runtime) (c adapter.Candidate, err error) {
	defer func() {
		if recover() != nil {
			c, err = adapter.Candidate{}, adapter.ErrMalformed
		}
	}()
	obj, err := adapter.DecodeObject(data, adapter.MaxHookPayloadBytes)
	if err != nil {
		return adapter.Candidate{}, err
	}
	session := obj.Ref("session_id")
	if session == "" {
		return adapter.Candidate{}, adapter.ErrMissingSession
	}
	name := obj.String("hook_event_name")
	eventType, known := hookEventTypes[name]
	if !known || (name == hookPostToolUse && rt.Product != ProductDesktop) {
		return adapter.Candidate{
			Provider:   adapter.ProviderCodex,
			Source:     adapter.SourceHook,
			EventType:  adapter.EventUnknown,
			SessionRef: session,
			TurnRef:    obj.Ref("turn_id"),
			Activity:   adapter.ActivityUnknown,
			Confidence: adapter.ConfidenceInferred,
			Support:    adapter.SupportUnverified,
		}, nil
	}
	support := adapter.Assess(VerifiedTargets, rt, name)
	c = adapter.Candidate{
		Provider:   adapter.ProviderCodex,
		Source:     adapter.SourceHook,
		EventType:  eventType,
		SessionRef: session,
		TurnRef:    obj.Ref("turn_id"),
		AgentRef:   obj.Ref("agent_id"),
		Activity:   adapter.ActivityUnknown,
		Confidence: adapter.ConfidenceFor(support),
		Support:    support,
	}
	if obj.Has("agent_type") {
		// Codex agent_type 값은 실측에서 보존하지 않았으므로 분류하지 않는다.
		c.AgentType = adapter.AgentTypeUnknown
	}
	switch eventType {
	case adapter.EventToolStarted, adapter.EventToolCompleted:
		c.ToolUseRef = obj.Ref("tool_use_id")
		if c.ToolUseRef == "" {
			return adapter.Candidate{}, adapter.ErrMissingRef
		}
		c.ToolCategory, c.Activity = toolCategory(obj.String("tool_name"))
		if obj.String("tool_name") == planTool {
			c.Plan = updatePlan(obj, rt)
		}
	case adapter.EventApprovalRequested:
		c.ToolCategory, _ = toolCategory(obj.String("tool_name"))
	case adapter.EventAgentStarted, adapter.EventAgentCompleted:
		if c.AgentRef == "" {
			return adapter.Candidate{}, adapter.ErrMissingRef
		}
	}
	return c, nil
}

// updatePlan은 update_plan 입력의 plan[].status만 읽는다. 단계 문장(step)과 explanation은 읽지 않는다.
// tool_input이 객체가 아니면(예: 인코딩된 문자열) 계획을 만들지 않는다.
func updatePlan(obj adapter.Object, rt adapter.Runtime) *adapter.Plan {
	input, ok := obj.Object("tool_input")
	if !ok {
		return nil
	}
	steps := adapter.PlanSteps(input, "plan")
	if steps == nil {
		return nil
	}
	return &adapter.Plan{Steps: steps, Support: adapter.Assess(VerifiedTargets, rt, planInputEvent)}
}

// toolCategory는 Codex 도구 이름을 보수적으로 분류한다. 실측 이름은 Bash뿐이다.
// apply_patch는 Codex 파일 편집 도구 이름으로 알려져 있지만 미관측이다.
// 하위 에이전트 관리 도구 이름은 확정되지 않아(#18 `other`) 분류하지 않는다.
func toolCategory(name string) (adapter.ToolCategory, adapter.Activity) {
	switch name {
	case "":
		return adapter.ToolNone, adapter.ActivityUnknown
	case "Bash":
		return adapter.ToolShell, adapter.ActivityUnknown
	case "apply_patch":
		return adapter.ToolEdit, adapter.ActivityCoding
	case "update_plan":
		return adapter.ToolPlan, adapter.ActivityUnknown
	}
	if strings.HasPrefix(name, "mcp__") {
		return adapter.ToolMCP, adapter.ActivityUnknown
	}
	return adapter.ToolOther, adapter.ActivityUnknown
}
