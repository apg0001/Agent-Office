package adapter

import "encoding/json"

// MaxPlanSteps는 이벤트 v1 `plan_steps` 최대 길이다. 더 긴 계획은 앞 50개 상태만 남긴다.
const MaxPlanSteps = 50

// 이벤트 v1 `plan_steps` 원소 enum.
const (
	PlanPending    = "pending"
	PlanInProgress = "in_progress"
	PlanCompleted  = "completed"
	PlanUnknown    = "unknown"
)

// Plan은 계획 도구(Claude TodoWrite, Codex update_plan) 입력에서 꺼낸 단계 상태 목록이다.
// 단계 문장(content·activeForm·step 등)은 읽지 않으며 상태 enum만 담는다(#38).
type Plan struct {
	Steps []string
	// Support는 계획 입력 형식의 검증 수준이다. Hook 이벤트 자체의 수준과 별개다.
	Support SupportLevel
}

// PlanStatusOf는 상태 문자열을 enum으로 바꾼다. 정확히 일치하지 않으면 unknown이다.
func PlanStatusOf(s string) string {
	switch s {
	case PlanPending, PlanInProgress, PlanCompleted:
		return s
	}
	return PlanUnknown
}

// PlanSteps는 input[listKey] 배열의 각 원소에서 `status`만 읽어 상태 목록을 만든다.
// 배열이 아니거나 비어 있으면 nil이다. 객체가 아닌 원소와 문자열이 아닌 상태는 unknown이다.
func PlanSteps(input Object, listKey string) []string {
	raw, ok := input[listKey]
	if !ok {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return nil
	}
	if len(items) > MaxPlanSteps {
		items = items[:MaxPlanSteps]
	}
	steps := make([]string, 0, len(items))
	for _, item := range items {
		var obj Object
		if json.Unmarshal(item, &obj) != nil || obj == nil {
			steps = append(steps, PlanUnknown)
			continue
		}
		steps = append(steps, PlanStatusOf(obj.String("status")))
	}
	return steps
}

// PlanUpdate는 계획이 붙은 도구 후보에서 같은 세션·에이전트·도구 호출의 plan.updated 후보를
// 만든다. 중복 제거 키는 `plan.updated|tool_use_id`라 Hook Pre/Post와 transcript tool_use가
// 같은 호출을 여러 번 보고해도 한 번만 통과한다.
func PlanUpdate(c Candidate) (Candidate, bool) {
	if c.Plan == nil || len(c.Plan.Steps) == 0 || c.ToolUseRef == "" {
		return Candidate{}, false
	}
	p := c
	p.EventType = EventPlanUpdated
	p.Activity = ActivityPlanning
	p.ToolCategory = ToolPlan
	p.Support = c.Plan.Support
	p.Confidence = ConfidenceFor(c.Plan.Support)
	return p, true
}
