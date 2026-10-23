package claude

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

const session = `"session_id":"00000000-0000-4000-8000-000000000001"`

func TestParseHookEventMapping(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		event   adapter.EventType
		support adapter.SupportLevel
		ref     string
	}{
		{"start", `{"hook_event_name":"SessionStart",` + session + `,"source":"startup"}`, adapter.EventSessionStarted, adapter.SupportVerified, ""},
		{"end", `{"hook_event_name":"SessionEnd",` + session + `,"reason":"other"}`, adapter.EventSessionEnded, adapter.SupportVerified, ""},
		{"prompt", `{"hook_event_name":"UserPromptSubmit",` + session + `,"prompt_id":"p1","prompt":"<synthetic>"}`, adapter.EventTurnStarted, adapter.SupportVerified, "p1"},
		{"stop", `{"hook_event_name":"Stop",` + session + `,"prompt_id":"p1"}`, adapter.EventTurnCompleted, adapter.SupportVerified, ""},
		{"pre", `{"hook_event_name":"PreToolUse",` + session + `,"tool_name":"Read","tool_use_id":"t1"}`, adapter.EventToolStarted, adapter.SupportVerified, "t1"},
		{"post", `{"hook_event_name":"PostToolUse",` + session + `,"tool_name":"Read","tool_use_id":"t1"}`, adapter.EventToolCompleted, adapter.SupportVerified, "t1"},
		{"failure-unobserved", `{"hook_event_name":"PostToolUseFailure",` + session + `,"tool_use_id":"t1"}`, adapter.EventToolFailed, adapter.SupportUnverified, "t1"},
		{"permission", `{"hook_event_name":"PermissionRequest",` + session + `,"tool_name":"Write"}`, adapter.EventApprovalRequested, adapter.SupportVerified, ""},
		{"substart", `{"hook_event_name":"SubagentStart",` + session + `,"agent_id":"a1","agent_type":"Explore"}`, adapter.EventAgentStarted, adapter.SupportVerified, "a1"},
		{"substop", `{"hook_event_name":"SubagentStop",` + session + `,"agent_id":"a1"}`, adapter.EventAgentCompleted, adapter.SupportVerified, "a1"},
		{"unknown-event", `{"hook_event_name":"Notification",` + session + `,"message":"<synthetic>"}`, adapter.EventUnknown, adapter.SupportUnverified, ""},
		{"missing-event-name", `{` + session + `}`, adapter.EventUnknown, adapter.SupportUnverified, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseHook([]byte(tc.payload), verifiedRuntime)
			if err != nil {
				t.Fatal(err)
			}
			if c.EventType != tc.event || c.Support != tc.support || c.DedupRef() != tc.ref ||
				c.Provider != adapter.ProviderClaude || c.Source != adapter.SourceHook {
				t.Fatalf("got %+v", c)
			}
			if c.Confidence != adapter.ConfidenceFor(tc.support) {
				t.Fatalf("confidence %s for %s", c.Confidence, c.Support)
			}
		})
	}
}

func TestParseHookErrors(t *testing.T) {
	cases := map[string]struct {
		payload string
		err     error
	}{
		"not-json":         {`not json`, adapter.ErrMalformed},
		"array":            {`[]`, adapter.ErrMalformed},
		"no-session":       {`{"hook_event_name":"SessionStart"}`, adapter.ErrMissingSession},
		"session-not-str":  {`{"hook_event_name":"SessionStart","session_id":12}`, adapter.ErrMissingSession},
		"session-path":     {`{"hook_event_name":"SessionStart","session_id":"/home/x y"}`, adapter.ErrMissingSession},
		"tool-no-id":       {`{"hook_event_name":"PreToolUse",` + session + `,"tool_name":"Read"}`, adapter.ErrMissingRef},
		"tool-bad-id":      {`{"hook_event_name":"PostToolUse",` + session + `,"tool_use_id":"a b"}`, adapter.ErrMissingRef},
		"agent-no-id":      {`{"hook_event_name":"SubagentStart",` + session + `}`, adapter.ErrMissingRef},
		"permission-no-id": {`{"hook_event_name":"PermissionRequest"}`, adapter.ErrMissingSession},
	}
	for name, tc := range cases {
		if _, err := ParseHook([]byte(tc.payload), verifiedRuntime); !errors.Is(err, tc.err) {
			t.Errorf("%s: err=%v want %v", name, err, tc.err)
		}
	}
	big := make([]byte, adapter.MaxHookPayloadBytes+1)
	if _, err := ParseHook(big, verifiedRuntime); !errors.Is(err, adapter.ErrTooLarge) {
		t.Errorf("oversized: %v", err)
	}
}

func TestUnverifiedRuntimeIsNotClaimedAsSupported(t *testing.T) {
	payload := []byte(`{"hook_event_name":"PreToolUse",` + session + `,"tool_name":"Read","tool_use_id":"t1"}`)
	runtimes := []adapter.Runtime{
		{Product: "claude-code", Version: "2.1.309", OS: "linux", Mode: "print"},
		{Product: "claude-code", Version: "2.1.308", OS: "windows", Mode: "print"},
		{Product: "claude-code", Version: "2.1.308", OS: "linux", Mode: "interactive"},
		{},
	}
	for _, rt := range runtimes {
		c, err := ParseHook(payload, rt)
		if err != nil {
			t.Fatal(err)
		}
		if c.EventType != adapter.EventToolStarted || c.Support != adapter.SupportUnverified || c.Confidence != adapter.ConfidenceInferred {
			t.Errorf("%+v: %+v", rt, c)
		}
	}
	cs, err := ParseTranscriptLine([]byte(`{"type":"assistant","sessionId":"s","message":{"content":[{"type":"tool_use","id":"t1","name":"Read"}]}}`), adapter.Runtime{})
	if err != nil || len(cs) != 1 || cs[0].Support != adapter.SupportUnverified || cs[0].Confidence != adapter.ConfidenceInferred {
		t.Fatalf("transcript unverified: %+v %v", cs, err)
	}
}

func TestToolCategoryMapping(t *testing.T) {
	cases := map[string]struct {
		cat adapter.ToolCategory
		act adapter.Activity
	}{
		"Read":           {adapter.ToolRead, adapter.ActivityReading},
		"Grep":           {adapter.ToolSearch, adapter.ActivityReading},
		"Glob":           {adapter.ToolSearch, adapter.ActivityReading},
		"Edit":           {adapter.ToolEdit, adapter.ActivityCoding},
		"Write":          {adapter.ToolEdit, adapter.ActivityCoding},
		"NotebookEdit":   {adapter.ToolEdit, adapter.ActivityCoding},
		"WebSearch":      {adapter.ToolWeb, adapter.ActivityBrowsing},
		"WebFetch":       {adapter.ToolWeb, adapter.ActivityBrowsing},
		"Agent":          {adapter.ToolDelegate, adapter.ActivityDelegating},
		"Task":           {adapter.ToolDelegate, adapter.ActivityDelegating},
		"Bash":           {adapter.ToolShell, adapter.ActivityUnknown},
		"TodoWrite":      {adapter.ToolPlan, adapter.ActivityUnknown},
		"mcp__srv__tool": {adapter.ToolMCP, adapter.ActivityUnknown},
		"read":           {adapter.ToolOther, adapter.ActivityUnknown},
		"SomethingNew":   {adapter.ToolOther, adapter.ActivityUnknown},
	}
	for name, want := range cases {
		payload := fmt.Sprintf(`{"hook_event_name":"PreToolUse",%s,"tool_name":%q,"tool_use_id":"t1"}`, session, name)
		c, err := ParseHook([]byte(payload), verifiedRuntime)
		if err != nil || c.ToolCategory != want.cat || c.Activity != want.act {
			t.Errorf("%s: %s/%s err=%v", name, c.ToolCategory, c.Activity, err)
		}
	}
	// 도구 이름 없는 도구 이벤트와 비도구 이벤트는 activity unknown이다.
	c, _ := ParseHook([]byte(`{"hook_event_name":"PreToolUse",`+session+`,"tool_use_id":"t1"}`), verifiedRuntime)
	if c.ToolCategory != adapter.ToolNone || c.Activity != adapter.ActivityUnknown {
		t.Errorf("no tool name: %+v", c)
	}
}

func TestAgentTypeCategory(t *testing.T) {
	cases := map[string]adapter.AgentTypeCategory{
		`"general-purpose"`: adapter.AgentTypeGeneral,
		`"Explore"`:         adapter.AgentTypeExplore,
		`"Plan"`:            adapter.AgentTypePlan,
		`"my-private-name"`: adapter.AgentTypeCustom,
		`""`:                adapter.AgentTypeUnknown,
		`7`:                 adapter.AgentTypeUnknown,
	}
	for raw, want := range cases {
		payload := `{"hook_event_name":"SubagentStart",` + session + `,"agent_id":"a1","agent_type":` + raw + `}`
		c, err := ParseHook([]byte(payload), verifiedRuntime)
		if err != nil || c.AgentType != want {
			t.Errorf("%s: %s %v", raw, c.AgentType, err)
		}
	}
	c, _ := ParseHook([]byte(`{"hook_event_name":"SubagentStart",`+session+`,"agent_id":"a1"}`), verifiedRuntime)
	if c.AgentType != adapter.AgentTypeNone {
		t.Errorf("absent agent_type: %s", c.AgentType)
	}
}

func TestParseTranscriptLine(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		types []adapter.EventType
		err   error
	}{
		{"internal-blob", `{"type":"api-request-blob","sessionId":"s","message":{"content":[{"type":"tool_use","id":"t"}]}}`, nil, nil},
		{"system", `{"type":"system","sessionId":"s"}`, nil, nil},
		{"no-type", `{"sessionId":"s"}`, nil, nil},
		{"prompt", `{"type":"user","sessionId":"s","promptId":"p","message":{"content":"<synthetic>"}}`, []adapter.EventType{adapter.EventTurnStarted}, nil},
		{"meta-prompt", `{"type":"user","sessionId":"s","promptId":"p","isMeta":true,"message":{"content":"<synthetic>"}}`, nil, nil},
		{"subagent-prompt", `{"type":"user","sessionId":"s","agentId":"a","promptId":"p","message":{"content":"<synthetic>"}}`, nil, nil},
		{"tool-result-ok", `{"type":"user","sessionId":"s","promptId":"p","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"<synthetic>"}]}}`, []adapter.EventType{adapter.EventToolCompleted}, nil},
		{"tool-result-err", `{"type":"user","sessionId":"s","message":{"content":[{"type":"tool_result","tool_use_id":"t","is_error":true}]}}`, []adapter.EventType{adapter.EventToolFailed}, nil},
		{"tool-result-err-string", `{"type":"user","sessionId":"s","message":{"content":[{"type":"tool_result","tool_use_id":"t","is_error":"true"}]}}`, []adapter.EventType{adapter.EventToolCompleted}, nil},
		{"two-tool-uses", `{"type":"assistant","sessionId":"s","message":{"content":[{"type":"text","text":"<synthetic>"},{"type":"tool_use","id":"t1","name":"Read"},"junk",{"type":"tool_use","id":"t2","name":"Bash"},{"type":"tool_use"}]}}`, []adapter.EventType{adapter.EventToolStarted, adapter.EventToolStarted}, nil},
		{"tool-use-in-user", `{"type":"user","sessionId":"s","message":{"content":[{"type":"tool_use","id":"t1"}]}}`, nil, nil},
		{"no-session", `{"type":"user","message":{"content":"x"}}`, nil, adapter.ErrMissingSession},
		{"message-not-object", `{"type":"user","sessionId":"s","message":"x"}`, nil, adapter.ErrMalformed},
		{"broken", `{"type":"user"`, nil, adapter.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := ParseTranscriptLine([]byte(tc.line), verifiedRuntime)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err=%v want %v", err, tc.err)
			}
			if len(cs) != len(tc.types) {
				t.Fatalf("got %+v", cs)
			}
			for i, c := range cs {
				if c.EventType != tc.types[i] || c.Source != adapter.SourceJSONL || c.Support != adapter.SupportVerified {
					t.Fatalf("got %+v", c)
				}
			}
		})
	}
	big := []byte(`{"type":"user","x":"` + strings.Repeat("a", adapter.MaxTranscriptLineBytes) + `"}`)
	if _, err := ParseTranscriptLine(big, verifiedRuntime); !errors.Is(err, adapter.ErrTooLarge) {
		t.Errorf("oversized: %v", err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"hook_event_name":"PreToolUse","session_id":"s","tool_use_id":"t","tool_name":"Read"}`))
	f.Add([]byte(`{"type":"assistant","sessionId":"s","message":{"content":[{"type":"tool_use","id":"t"}]}}`))
	f.Add([]byte(`{"type":"user","sessionId":"s","message":{"content":[null,1,{"type":"tool_result"}]}}`))
	f.Add([]byte("\xEF\xBB\xBF{}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := ParseHook(data, verifiedRuntime)
		if err == nil && (c.SessionRef == "" || !adapter.ValidRef(c.SessionRef)) {
			t.Fatalf("candidate without valid session: %+v", c)
		}
		cs, _ := ParseTranscriptLine(data, verifiedRuntime)
		for _, c := range cs {
			if !adapter.ValidRef(c.SessionRef) || c.DedupRef() == "" {
				t.Fatalf("transcript candidate without refs: %+v", c)
			}
		}
	})
}
