// Package claude는 Claude Code Hook stdin과 transcript JSONL 한 줄을 제공자 중립
// 후보로 바꾼다. 규칙은 tests/poc/collector/dedup_reference.py(#20)와 같다.
//
// 허용 필드만 정확한 키 이름으로 읽는다. prompt, tool_input, tool_response, error,
// last_assistant_message, cwd, transcript_path, background_tasks, session_crons,
// message.content의 본문은 어떤 후보 필드에도 복사하지 않는다.
package claude

import (
	"strings"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

// Hook 이벤트 이름.
const (
	hookSessionStart       = "SessionStart"
	hookSessionEnd         = "SessionEnd"
	hookUserPromptSubmit   = "UserPromptSubmit"
	hookStop               = "Stop"
	hookPreToolUse         = "PreToolUse"
	hookPostToolUse        = "PostToolUse"
	hookPostToolUseFailure = "PostToolUseFailure"
	hookPermissionRequest  = "PermissionRequest"
	hookSubagentStart      = "SubagentStart"
	hookSubagentStop       = "SubagentStop"

	// transcriptEvent는 transcript 줄 검증 수준을 조회할 때 쓰는 가상 이벤트 이름이다.
	transcriptEvent = "transcript"

	// planInputEvent는 TodoWrite 입력(tool_input.todos / tool_use.input.todos) 형식의 검증 수준을
	// 조회하는 가상 이벤트 이름이다. 실측(#19, #20)에서 TodoWrite 호출을 보존하지 않았으므로
	// 어떤 대상에도 없고 계획 후보는 항상 unverified → inferred다.
	planInputEvent = "plan:TodoWrite"
	planTool       = "TodoWrite"
)

// VerifiedTargets는 실측(#19, #20)으로 검증된 런타임이다.
// PostToolUseFailure는 등록했지만 10회 모두 미관측이라 검증 이벤트가 아니다.
var VerifiedTargets = []adapter.Target{
	{
		Runtime: adapter.Runtime{Product: "claude-code", Version: "2.1.308", OS: "linux", Mode: "print"},
		Level:   adapter.SupportVerified,
		Observed: adapter.Set(
			hookSessionStart, hookUserPromptSubmit, hookPreToolUse, hookPostToolUse,
			hookSubagentStart, hookSubagentStop, hookPermissionRequest, hookStop, hookSessionEnd,
			transcriptEvent,
		),
	},
}

var hookEventTypes = map[string]adapter.EventType{
	hookSessionStart:       adapter.EventSessionStarted,
	hookSessionEnd:         adapter.EventSessionEnded,
	hookUserPromptSubmit:   adapter.EventTurnStarted,
	hookStop:               adapter.EventTurnCompleted,
	hookPreToolUse:         adapter.EventToolStarted,
	hookPostToolUse:        adapter.EventToolCompleted,
	hookPostToolUseFailure: adapter.EventToolFailed,
	// PermissionRequest에는 tool_use_id가 없다(공식 문서·#19 실측).
	hookPermissionRequest: adapter.EventApprovalRequested,
	hookSubagentStart:     adapter.EventAgentStarted,
	hookSubagentStop:      adapter.EventAgentCompleted,
}

// ParseHook은 Hook stdin payload 한 건을 후보 하나로 바꾼다.
//
//   - 알 수 없는 hook_event_name은 EventUnknown 후보(세션·턴 참조만)로 반환한다.
//   - session_id가 없거나 식별자 형식이 아니면 ErrMissingSession.
//   - 도구 이벤트에 tool_use_id가 없으면 ErrMissingRef(참조 구현과 같이 후보 없음).
//
// 어떤 입력에서도 panic하지 않는다.
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
	if !known {
		return adapter.Candidate{
			Provider:   adapter.ProviderClaude,
			Source:     adapter.SourceHook,
			EventType:  adapter.EventUnknown,
			SessionRef: session,
			TurnRef:    obj.Ref("prompt_id"),
			Activity:   adapter.ActivityUnknown,
			Confidence: adapter.ConfidenceInferred,
			Support:    adapter.SupportUnverified,
		}, nil
	}
	support := adapter.Assess(VerifiedTargets, rt, name)
	c = adapter.Candidate{
		Provider:   adapter.ProviderClaude,
		Source:     adapter.SourceHook,
		EventType:  eventType,
		SessionRef: session,
		TurnRef:    obj.Ref("prompt_id"),
		AgentRef:   obj.Ref("agent_id"),
		Activity:   adapter.ActivityUnknown,
		Confidence: adapter.ConfidenceFor(support),
		Support:    support,
	}
	if obj.Has("agent_type") {
		c.AgentType = agentTypeCategory(obj.String("agent_type"))
	}
	switch eventType {
	case adapter.EventToolStarted, adapter.EventToolCompleted, adapter.EventToolFailed:
		c.ToolUseRef = obj.Ref("tool_use_id")
		if c.ToolUseRef == "" {
			return adapter.Candidate{}, adapter.ErrMissingRef
		}
		c.ToolCategory, c.Activity = toolCategory(obj.String("tool_name"))
		if eventType != adapter.EventToolFailed && obj.String("tool_name") == planTool {
			if input, ok := obj.Object("tool_input"); ok {
				c.Plan = todoPlan(input, rt)
			}
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

// ParseTranscriptLine은 transcript JSONL 한 줄을 0개 이상의 후보로 바꾼다.
// assistant·user 줄만 활동으로 읽고 api-request-blob 등 내부 유형은 (nil, nil)이다.
func ParseTranscriptLine(line []byte, rt adapter.Runtime) (out []adapter.Candidate, err error) {
	defer func() {
		if recover() != nil {
			out, err = nil, adapter.ErrMalformed
		}
	}()
	item, err := adapter.DecodeObject(line, adapter.MaxTranscriptLineBytes)
	if err != nil {
		return nil, err
	}
	lineType := item.String("type")
	if lineType != "assistant" && lineType != "user" {
		return nil, nil
	}
	session := item.Ref("sessionId")
	if session == "" {
		return nil, adapter.ErrMissingSession
	}
	message, ok := item.Object("message")
	if !ok {
		return nil, adapter.ErrMalformed
	}
	support := adapter.Assess(VerifiedTargets, rt, transcriptEvent)
	base := adapter.Candidate{
		Provider:   adapter.ProviderClaude,
		Source:     adapter.SourceJSONL,
		SessionRef: session,
		TurnRef:    item.Ref("promptId"),
		AgentRef:   item.Ref("agentId"),
		Activity:   adapter.ActivityUnknown,
		Confidence: adapter.ConfidenceFor(support),
		Support:    support,
	}
	blocks := message.Objects("content")
	hasToolResult := false
	for _, block := range blocks {
		blockType := block.String("type")
		switch {
		case lineType == "assistant" && blockType == "tool_use":
			if ref := block.Ref("id"); ref != "" {
				c := base
				c.EventType = adapter.EventToolStarted
				c.ToolUseRef = ref
				c.ToolCategory, c.Activity = toolCategory(block.String("name"))
				if block.String("name") == planTool {
					if input, ok := block.Object("input"); ok {
						c.Plan = todoPlan(input, rt)
					}
				}
				out = append(out, c)
			}
		case lineType == "user" && blockType == "tool_result":
			hasToolResult = true
			if ref := block.Ref("tool_use_id"); ref != "" {
				c := base
				c.EventType = adapter.EventToolCompleted
				if block.IsTrue("is_error") {
					c.EventType = adapter.EventToolFailed
				}
				c.ToolUseRef = ref
				out = append(out, c)
			}
		}
	}
	isPromptLine := lineType == "user" && item.String("agentId") == "" && !item.IsTrue("isMeta") && !hasToolResult
	if base.TurnRef != "" && isPromptLine {
		c := base
		c.EventType = adapter.EventTurnStarted
		c.AgentRef = ""
		out = append(out, c)
	}
	return out, nil
}

// todoPlan은 TodoWrite 입력의 todos[].status만 읽는다. 할 일 문장(content·activeForm)은 읽지 않는다.
func todoPlan(input adapter.Object, rt adapter.Runtime) *adapter.Plan {
	steps := adapter.PlanSteps(input, "todos")
	if steps == nil {
		return nil
	}
	return &adapter.Plan{Steps: steps, Support: adapter.Assess(VerifiedTargets, rt, planInputEvent)}
}

// toolCategory는 도구 이름을 보수적으로 분류한다. 이름 원문은 반환하지 않는다.
// Bash는 테스트·빌드·임의 명령을 구분할 수 없어 activity unknown이다.
func toolCategory(name string) (adapter.ToolCategory, adapter.Activity) {
	switch name {
	case "":
		return adapter.ToolNone, adapter.ActivityUnknown
	case "Read":
		return adapter.ToolRead, adapter.ActivityReading
	case "Grep", "Glob", "LS":
		return adapter.ToolSearch, adapter.ActivityReading
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return adapter.ToolEdit, adapter.ActivityCoding
	case "WebSearch", "WebFetch":
		return adapter.ToolWeb, adapter.ActivityBrowsing
	case "Agent", "Task":
		return adapter.ToolDelegate, adapter.ActivityDelegating
	case "Bash":
		return adapter.ToolShell, adapter.ActivityUnknown
	case "TodoWrite", "ExitPlanMode", "EnterPlanMode":
		return adapter.ToolPlan, adapter.ActivityUnknown
	}
	if strings.HasPrefix(name, "mcp__") {
		return adapter.ToolMCP, adapter.ActivityUnknown
	}
	return adapter.ToolOther, adapter.ActivityUnknown
}

// agentTypeCategory는 내장 유형만 구분하고 사용자 정의 이름은 custom으로 감춘다.
func agentTypeCategory(name string) adapter.AgentTypeCategory {
	switch name {
	case "":
		return adapter.AgentTypeUnknown
	case "general-purpose":
		return adapter.AgentTypeGeneral
	case "Explore":
		return adapter.AgentTypeExplore
	case "Plan":
		return adapter.AgentTypePlan
	}
	return adapter.AgentTypeCustom
}
