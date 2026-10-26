// Package normalize는 어댑터 후보(adapter.Candidate)를 이벤트 v1 객체로 바꾼다(#32).
//
// 출력은 packages/event-schema/schema/event-v1.schema.json의 13개 필수 필드와 선택 필드 plan_steps(#38)·workspace_name(#181)뿐이다. 제공자 원본
// 세션·에이전트 ID는 장치 범위 UUIDv5로 바꿔 PC 밖으로 내보내지 않고, enum 필드는 v1 집합에
// 속한 값만 통과시킨다(2차 방어선). 큐·전송(#33)과 polling 실행 루프(#34)는 범위 밖이다.
package normalize

// SchemaVersion은 이벤트 v1 `schema_version` 상수다.
const SchemaVersion = "1.0"

// MaxSequence는 JavaScript 안전 정수 상한(2^53-1)이다.
const MaxSequence int64 = 1<<53 - 1

// Event는 이벤트 v1 객체다. 필드를 추가하면 서버가 거부하므로(additionalProperties: false)
// 새 필드는 새 스키마 버전과 개인정보 검토를 거쳐야 한다.
type Event struct {
	SchemaVersion string  `json:"schema_version"`
	EventID       string  `json:"event_id"`
	DeviceID      string  `json:"device_id"`
	SessionID     string  `json:"session_id"`
	AgentID       string  `json:"agent_id"`
	ParentAgentID *string `json:"parent_agent_id"`
	Provider      string  `json:"provider"`
	Source        string  `json:"source"`
	EventType     string  `json:"event_type"`
	Activity      string  `json:"activity"`
	Confidence    string  `json:"confidence"`
	OccurredAt    string  `json:"occurred_at"`
	Sequence      int64   `json:"sequence"`
	// PlanSteps는 plan.updated에서만 쓰는 선택 필드다(#38). 단계 상태 enum만 담고 문장은 담지 않는다.
	PlanSteps []string `json:"plan_steps,omitempty"`
	// WorkspaceName은 작업 폴더의 마지막 이름이다(#181). PC별 동의가 켜진 Hook 이벤트에만 있다.
	WorkspaceName string `json:"workspace_name,omitempty"`
}

// Record는 정규화 결과다. Event만 서버로 보낸다.
type Record struct {
	Event Event
	// DeterministicID가 false면 EventID는 무작위 UUIDv4다. 큐(#33)에 처음 기록할 때의 값을
	// 영속화하고 재전송에서 그대로 써야 한다.
	DeterministicID bool
}

// v1 enum 집합. 스키마와의 일치는 schema_test.go가 확인한다.
var (
	providers   = set("codex", "claude", "unknown")
	sources     = set("hook", "jsonl", "app_server", "inferred")
	confidences = set("observed", "inferred")
	eventTypes  = set(
		"session.discovered", "session.started", "session.ended",
		"turn.started", "turn.completed", "turn.interrupted",
		"tool.started", "tool.completed", "tool.failed",
		"approval.requested", "approval.resolved",
		"agent.started", "agent.completed", "plan.updated", "unknown",
	)
	activities = set(
		"reading", "browsing", "coding", "testing", "planning", "delegating",
		"reporting", "thinking", "waiting_user", "idle", "unknown",
	)
)

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// enumOr는 값이 enum에 있으면 그대로, 없으면 fallback을 반환한다. 원문 값을 통과시키지 않는다.
func enumOr(values map[string]bool, v, fallback string) string {
	if values[v] {
		return v
	}
	return fallback
}
