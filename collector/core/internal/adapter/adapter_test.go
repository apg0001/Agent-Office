package adapter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDv5MatchesPythonReference(t *testing.T) {
	// 기대값은 Python uuid.uuid5(EVENT_ID_NAMESPACE, key)로 계산했다.
	key := "device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.started|toolu_synthetic_0001"
	if got, want := UUIDv5(EventIDNamespace, key), "4dfd1880-2479-5d5c-a596-2d4aece97e4d"; got != want {
		t.Fatalf("UUIDv5 = %s, want %s", got, want)
	}
	if got := UUIDv5(EventIDNamespace, key); got[14] != '5' || !uuidPattern.MatchString(got) {
		t.Fatalf("UUIDv5 형식 오류: %s", got)
	}
}

func TestDedupKindAndRef(t *testing.T) {
	cases := []struct {
		c        Candidate
		kind     string
		ref      string
		keyEmpty bool
	}{
		{Candidate{EventType: EventToolCompleted, ToolUseRef: "t1"}, "tool.finished", "t1", false},
		{Candidate{EventType: EventToolFailed, ToolUseRef: "t1"}, "tool.finished", "t1", false},
		{Candidate{EventType: EventToolStarted, ToolUseRef: "t1", AgentRef: "a1"}, "tool.started", "t1", false},
		{Candidate{EventType: EventTurnStarted, TurnRef: "p1"}, "turn.started", "p1", false},
		{Candidate{EventType: EventAgentStarted, AgentRef: "a1"}, "agent.started", "a1", false},
		{Candidate{EventType: EventTurnCompleted, TurnRef: "p1"}, "turn.completed", "", true},
		{Candidate{EventType: EventSessionStarted}, "session.started", "", true},
		{Candidate{EventType: EventApprovalRequested, TurnRef: "p1"}, "approval.requested", "", true},
		{Candidate{EventType: EventUnknown, TurnRef: "p1"}, "unknown", "", true},
	}
	for _, tc := range cases {
		tc.c.Provider, tc.c.SessionRef = ProviderClaude, "s1"
		if tc.c.DedupKind() != tc.kind || tc.c.DedupRef() != tc.ref {
			t.Errorf("%s: kind=%s ref=%s", tc.c.EventType, tc.c.DedupKind(), tc.c.DedupRef())
		}
		key := DedupKey("d", tc.c)
		if (key == "") != tc.keyEmpty {
			t.Errorf("%s: key=%q", tc.c.EventType, key)
		}
		if !tc.keyEmpty && key != "d|claude|s1|"+tc.kind+"|"+tc.ref {
			t.Errorf("key format: %s", key)
		}
	}
}

func TestEventIDKeylessIsRandomV4(t *testing.T) {
	c := Candidate{Provider: ProviderClaude, SessionRef: "s", EventType: EventSessionStarted}
	a, detA, errA := EventID("d", c)
	b, detB, errB := EventID("d", c)
	if errA != nil || errB != nil || detA || detB || a == b || a[14] != '4' || !uuidPattern.MatchString(a) {
		t.Fatalf("keyless event id: %s %s", a, b)
	}
	c = Candidate{Provider: ProviderClaude, SessionRef: "s", EventType: EventToolStarted, ToolUseRef: "t"}
	a, detA, _ = EventID("d", c)
	b, _, _ = EventID("d", c)
	if !detA || a != b {
		t.Fatalf("keyed event id not deterministic")
	}
}

func TestDeduplicatorRestartAndKeyless(t *testing.T) {
	tool := Candidate{Provider: ProviderClaude, SessionRef: "s", EventType: EventToolCompleted, ToolUseRef: "t"}
	failed := tool
	failed.EventType = EventToolFailed
	keyless := Candidate{Provider: ProviderClaude, SessionRef: "s", EventType: EventTurnCompleted}

	d := NewDeduplicator("d")
	if !d.Accept(tool) || d.Accept(failed) || !d.Accept(keyless) || !d.Accept(keyless) {
		t.Fatal("first-wins / keyless rule broken")
	}
	if d.Suppressed() != 1 {
		t.Fatalf("suppressed = %d", d.Suppressed())
	}
	restarted := NewDeduplicator("d", d.Seen()...)
	if restarted.Accept(failed) {
		t.Fatal("persisted key replayed after restart")
	}
	other := NewDeduplicator("other-device")
	if !other.Accept(tool) {
		t.Fatal("device scope must separate keys")
	}
}

func TestDecodeObject(t *testing.T) {
	if _, err := DecodeObject([]byte(strings.Repeat(" ", 11)), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	for _, bad := range []string{"", "null", "[]", "1", `"x"`, "{", `{"a":}`, strings.Repeat("[", 20000)} {
		if _, err := DecodeObject([]byte(bad), 1<<20); !errors.Is(err, ErrMalformed) {
			t.Errorf("%.20q: %v", bad, err)
		}
	}
	obj, err := DecodeObject([]byte("\xEF\xBB\xBF{\"Session_ID\":\"x\",\"session_id\":7,\"t\":true,\"f\":\"true\"}\r\n"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if obj.String("session_id") != "" || obj.String("Session_id") != "" || obj.String("Session_ID") != "x" {
		t.Fatal("keys must match exactly and only JSON strings count")
	}
	if !obj.IsTrue("t") || obj.IsTrue("f") || obj.IsTrue("missing") {
		t.Fatal("IsTrue must accept only the true literal")
	}
}

func TestValidRef(t *testing.T) {
	valid := []string{"00000000-0000-4000-8000-000000000001", "toolu_01AbC", "a0000000000000001", "call_x.y:z"}
	invalid := []string{"", "has space", "/home/user", `C:\x`, "a\"b", "é", strings.Repeat("a", MaxRefLength+1)}
	for _, s := range valid {
		if !ValidRef(s) {
			t.Errorf("valid rejected: %q", s)
		}
	}
	for _, s := range invalid {
		if ValidRef(s) {
			t.Errorf("invalid accepted: %q", s)
		}
	}
}

func TestAssess(t *testing.T) {
	rt := Runtime{Product: "p", Version: "1", OS: "linux", Mode: "m"}
	targets := []Target{{Runtime: rt, Level: SupportVerified, Observed: Set("A")}}
	if Assess(targets, rt, "A") != SupportVerified {
		t.Fatal("observed event on verified runtime")
	}
	if Assess(targets, rt, "B") != SupportUnverified {
		t.Fatal("unobserved event must be unverified")
	}
	other := rt
	other.Version = "2"
	if Assess(targets, other, "A") != SupportUnverified {
		t.Fatal("other version must be unverified")
	}
	if ConfidenceFor(SupportVerified) != ConfidenceObserved || ConfidenceFor(SupportPartial) != ConfidenceInferred ||
		ConfidenceFor(SupportUnverified) != ConfidenceInferred {
		t.Fatal("confidence mapping")
	}
}

func TestEnumsAreSubsetOfEventV1Schema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "event-schema", "schema", "event-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	ours := map[string][]string{
		"provider": {string(ProviderClaude), string(ProviderCodex), string(ProviderUnknown)},
		"source":   {string(SourceHook), string(SourceJSONL)},
		"event_type": {string(EventSessionStarted), string(EventSessionEnded), string(EventTurnStarted),
			string(EventTurnCompleted), string(EventToolStarted), string(EventToolCompleted), string(EventToolFailed),
			string(EventApprovalRequested), string(EventAgentStarted), string(EventAgentCompleted), string(EventPlanUpdated), string(EventUnknown)},
		"activity": {string(ActivityReading), string(ActivityBrowsing), string(ActivityCoding), string(ActivityPlanning),
			string(ActivityDelegating), string(ActivityUnknown)},
		"confidence": {string(ConfidenceObserved), string(ConfidenceInferred)},
	}
	for field, values := range ours {
		allowed := map[string]bool{}
		for _, v := range schema.Properties[field].Enum {
			allowed[v] = true
		}
		for _, v := range values {
			if !allowed[v] {
				t.Errorf("%s=%q not in event v1 schema", field, v)
			}
		}
	}
}
