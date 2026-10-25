// Package adapter는 Codex·Claude 입력 어댑터가 공통으로 만드는 제공자 중립 후보(Candidate)와
// 교차 소스 중복 제거 규칙(#20 참조 구현 이식)을 정의한다.
//
// 후보는 화이트리스트 식별자와 enum만 담는다. 프롬프트, 도구 입력·출력, 명령, cwd,
// transcript 경로 같은 원문 값은 어떤 필드에도 들어가지 않는다. 이벤트 v1 최종 정규화
// (device_id·agent_id UUID 매핑, occurred_at, sequence)는 #32 범위다.
package adapter

// Provider는 이벤트 v1 `provider` enum이다.
type Provider string

const (
	ProviderClaude  Provider = "claude"
	ProviderCodex   Provider = "codex"
	ProviderUnknown Provider = "unknown"
)

// Source는 이벤트 v1 `source` enum 중 어댑터가 만드는 값이다.
type Source string

const (
	SourceHook  Source = "hook"
	SourceJSONL Source = "jsonl"
)

// EventType은 이벤트 v1 `event_type` enum이다.
type EventType string

const (
	EventSessionStarted    EventType = "session.started"
	EventSessionEnded      EventType = "session.ended"
	EventTurnStarted       EventType = "turn.started"
	EventTurnCompleted     EventType = "turn.completed"
	EventToolStarted       EventType = "tool.started"
	EventToolCompleted     EventType = "tool.completed"
	EventToolFailed        EventType = "tool.failed"
	EventApprovalRequested EventType = "approval.requested"
	EventAgentStarted      EventType = "agent.started"
	EventAgentCompleted    EventType = "agent.completed"
	EventPlanUpdated       EventType = "plan.updated"
	EventUnknown           EventType = "unknown"
)

// Activity는 이벤트 v1 `activity` enum 중 어댑터가 도구 분류로 정하는 값이다.
type Activity string

const (
	ActivityReading    Activity = "reading"
	ActivityBrowsing   Activity = "browsing"
	ActivityCoding     Activity = "coding"
	ActivityPlanning   Activity = "planning"
	ActivityDelegating Activity = "delegating"
	ActivityUnknown    Activity = "unknown"
)

// Confidence는 이벤트 v1 `confidence` enum이다.
type Confidence string

const (
	ConfidenceObserved Confidence = "observed"
	ConfidenceInferred Confidence = "inferred"
)

// ToolCategory는 도구 이름 원문 대신 남기는 안전한 분류다.
type ToolCategory string

const (
	ToolNone     ToolCategory = ""
	ToolRead     ToolCategory = "read"
	ToolSearch   ToolCategory = "search"
	ToolEdit     ToolCategory = "edit"
	ToolShell    ToolCategory = "shell"
	ToolWeb      ToolCategory = "web"
	ToolDelegate ToolCategory = "delegate"
	ToolPlan     ToolCategory = "plan"
	ToolMCP      ToolCategory = "mcp"
	ToolOther    ToolCategory = "other"
)

// AgentTypeCategory는 하위 에이전트 유형 원문(사용자 정의 이름 포함) 대신 남기는 분류다.
type AgentTypeCategory string

const (
	AgentTypeNone    AgentTypeCategory = ""
	AgentTypeGeneral AgentTypeCategory = "general"
	AgentTypeExplore AgentTypeCategory = "explore"
	AgentTypePlan    AgentTypeCategory = "plan"
	AgentTypeCustom  AgentTypeCategory = "custom"
	AgentTypeUnknown AgentTypeCategory = "unknown"
)

// Candidate는 정규화 직전 후보다. 문자열 필드는 ValidRef를 통과한 불투명 식별자뿐이다.
type Candidate struct {
	Provider  Provider
	Source    Source
	EventType EventType
	// SessionRef는 제공자 세션 ID(Claude `session_id`/`sessionId`, Codex `session_id`)다.
	SessionRef string
	// TurnRef는 턴 ID(Claude `prompt_id`/`promptId`, Codex `turn_id`)다.
	TurnRef string
	// ToolUseRef는 도구 호출 ID(`tool_use_id`, transcript `tool_use.id`)다.
	ToolUseRef string
	// AgentRef는 이벤트가 속한 하위 에이전트 ID다. 메인 에이전트면 빈 값이다.
	// agent.started/agent.completed에서는 시작·종료된 하위 에이전트 자신이다.
	AgentRef     string
	ToolCategory ToolCategory
	Activity     Activity
	AgentType    AgentTypeCategory
	Confidence   Confidence
	Support      SupportLevel
	// Plan은 계획 도구 호출의 단계 상태다(#38). 도구 후보에 붙고 PlanUpdate가 plan.updated 후보를 만든다.
	Plan *Plan
}

// DedupKind는 교차 소스 키의 종류다. 완료와 실패는 같은 호출의 끝이므로 합친다.
func (c Candidate) DedupKind() string {
	if c.EventType == EventToolCompleted || c.EventType == EventToolFailed {
		return "tool.finished"
	}
	return string(c.EventType)
}

// DedupRef는 두 소스가 공유하는 식별자다. 한 소스에서만 관측되는 이벤트는 빈 값이다.
func (c Candidate) DedupRef() string {
	switch c.EventType {
	case EventTurnStarted:
		return c.TurnRef
	case EventToolStarted, EventToolCompleted, EventToolFailed, EventPlanUpdated:
		return c.ToolUseRef
	case EventAgentStarted, EventAgentCompleted:
		return c.AgentRef
	default:
		return ""
	}
}
