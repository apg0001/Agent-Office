package normalize

import (
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

const testDevice = "11111111-2222-4333-8444-555555555555"

var observed = time.Date(2026, 10, 23, 1, 2, 3, 0, time.FixedZone("KST", 9*3600))

func newNormalizer(t *testing.T, opts Options) *Normalizer {
	t.Helper()
	if opts.DeviceID == "" {
		opts.DeviceID = testDevice
	}
	n, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func verifiedCandidate(et adapter.EventType) adapter.Candidate {
	return adapter.Candidate{
		Provider:   adapter.ProviderClaude,
		Source:     adapter.SourceHook,
		EventType:  et,
		SessionRef: "sess-1",
		ToolUseRef: "toolu_1",
		TurnRef:    "turn-1",
		Activity:   adapter.ActivityReading,
		Confidence: adapter.ConfidenceObserved,
		Support:    adapter.SupportVerified,
	}
}

func mustAccept(t *testing.T, n *Normalizer, in Input) Event {
	t.Helper()
	rec, out := n.Normalize(in)
	if out != OutcomeAccepted {
		t.Fatalf("outcome %s", out)
	}
	if err := validateEvent(loadSchema(t), mustMarshal(t, rec.Event)); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return rec.Event
}

func TestNewRejectsNonUUIDDevice(t *testing.T) {
	for _, d := range []string{"", "device-synthetic-1", "11111111222243338444555555555555"} {
		if _, err := New(Options{DeviceID: d}); err != ErrInvalidDeviceID {
			t.Fatalf("%q: %v", d, err)
		}
	}
	n := newNormalizer(t, Options{DeviceID: "11111111-2222-4333-8444-55555555555A"})
	if n.DeviceID() != "11111111-2222-4333-8444-55555555555a" {
		t.Fatal("device id must be lowercased")
	}
}

func TestIdentityMappingIsDeterministicAndScoped(t *testing.T) {
	a := newNormalizer(t, Options{})
	b := newNormalizer(t, Options{}) // 재시작
	c := verifiedCandidate(adapter.EventTurnCompleted)
	ea := mustAccept(t, a, Input{Candidate: c, ObservedAt: observed})
	eb := mustAccept(t, b, Input{Candidate: c, ObservedAt: observed})
	if ea.SessionID != eb.SessionID || ea.AgentID != eb.AgentID {
		t.Fatal("same session must map to same UUIDs across restarts")
	}
	if ea.SessionID == "sess-1" || ea.AgentID == ea.SessionID || ea.ParentAgentID != nil {
		t.Fatalf("lead mapping: %+v", ea)
	}
	if ea.SessionID != SessionID(testDevice, "claude", "sess-1") || ea.AgentID != LeadAgentID(testDevice, "claude", "sess-1") {
		t.Fatal("mapping functions disagree")
	}
	// 다른 장치·제공자·세션은 다른 UUID다.
	other := newNormalizer(t, Options{DeviceID: "99999999-2222-4333-8444-555555555555"})
	if mustAccept(t, other, Input{Candidate: c, ObservedAt: observed}).SessionID == ea.SessionID {
		t.Fatal("device must scope session id")
	}
	if SessionID(testDevice, "codex", "sess-1") == ea.SessionID || SessionID(testDevice, "claude", "sess-2") == ea.SessionID {
		t.Fatal("provider and session must scope session id")
	}
	if SubagentID(testDevice, "claude", "sess-1", "a1") == SubagentID(testDevice, "claude", "sess-2", "a1") {
		t.Fatal("subagent id must be session scoped")
	}
}

func TestIdentityNamespaceDiffersFromEventIDNamespace(t *testing.T) {
	if IdentityNamespace == adapter.EventIDNamespace {
		t.Fatal("namespaces must differ")
	}
	name := "session|" + testDevice + "|claude|sess-1"
	if adapter.UUIDv5(adapter.EventIDNamespace, name) == SessionID(testDevice, "claude", "sess-1") {
		t.Fatal("identity uuid must not equal event-id namespace uuid")
	}
}

func TestSubagentEventsReferenceLeadAsParent(t *testing.T) {
	n := newNormalizer(t, Options{})
	lead := mustAccept(t, n, Input{Candidate: verifiedCandidate(adapter.EventSessionStarted), ObservedAt: observed})
	sub := verifiedCandidate(adapter.EventAgentStarted)
	sub.AgentRef = "a0001"
	started := mustAccept(t, n, Input{Candidate: sub, ObservedAt: observed})
	tool := verifiedCandidate(adapter.EventToolStarted)
	tool.AgentRef = "a0001"
	inner := mustAccept(t, n, Input{Candidate: tool, ObservedAt: observed})
	for _, ev := range []Event{started, inner} {
		if ev.ParentAgentID == nil || *ev.ParentAgentID != lead.AgentID || ev.SessionID != lead.SessionID {
			t.Fatalf("subagent parent: %+v", ev)
		}
	}
	if started.AgentID != inner.AgentID || started.AgentID == lead.AgentID {
		t.Fatal("subagent id must be stable and distinct from lead")
	}
}

func TestEnumGuardConvertsOrDrops(t *testing.T) {
	n := newNormalizer(t, Options{})
	c := verifiedCandidate(adapter.EventType("tool.exec"))
	c.Provider = adapter.Provider("gemini")
	c.Activity = adapter.Activity("hacking")
	ev := mustAccept(t, n, Input{Candidate: c, ObservedAt: observed})
	if ev.EventType != "unknown" || ev.Provider != "unknown" || ev.Activity != "unknown" || ev.Confidence != "inferred" {
		t.Fatalf("converted: %+v", ev)
	}
	// 변환된 unknown은 키가 없어 중복 제거되지 않는다.
	mustAccept(t, n, Input{Candidate: c, ObservedAt: observed})

	bad := verifiedCandidate(adapter.EventToolStarted)
	bad.Source = adapter.Source("stdout")
	if _, out := n.Normalize(Input{Candidate: bad, ObservedAt: observed}); out != OutcomeDroppedInvalid {
		t.Fatalf("bad source: %s", out)
	}
	conf := verifiedCandidate(adapter.EventToolStarted)
	conf.Confidence = adapter.Confidence("certain")
	if ev := mustAccept(t, n, Input{Candidate: conf, ObservedAt: observed}); ev.Confidence != "inferred" {
		t.Fatal("invalid confidence must downgrade to inferred")
	}
}

func TestConfidencePolicy(t *testing.T) {
	cases := []struct {
		support adapter.SupportLevel
		conf    adapter.Confidence
		source  adapter.Source
		want    string
	}{
		{adapter.SupportVerified, adapter.ConfidenceObserved, adapter.SourceHook, "observed"},
		{adapter.SupportPartial, adapter.ConfidenceInferred, adapter.SourceHook, "inferred"},
		{adapter.SupportUnverified, adapter.ConfidenceInferred, adapter.SourceJSONL, "inferred"},
		// 어댑터가 실수로 observed를 줘도 미검증이면 낮춘다.
		{adapter.SupportPartial, adapter.ConfidenceObserved, adapter.SourceHook, "inferred"},
		{adapter.SupportVerified, adapter.ConfidenceObserved, adapter.Source("inferred"), "inferred"},
	}
	for i, tc := range cases {
		n := newNormalizer(t, Options{})
		c := verifiedCandidate(adapter.EventSessionStarted)
		c.Support, c.Confidence, c.Source = tc.support, tc.conf, tc.source
		if ev := mustAccept(t, n, Input{Candidate: c, ObservedAt: observed}); ev.Confidence != tc.want {
			t.Fatalf("case %d: %s", i, ev.Confidence)
		}
	}
}

func TestUnknownPolicy(t *testing.T) {
	n := newNormalizer(t, Options{})
	c := adapter.Candidate{
		Provider: adapter.ProviderCodex, Source: adapter.SourceHook, EventType: adapter.EventUnknown,
		SessionRef: "s1", Activity: adapter.ActivityUnknown, Confidence: adapter.ConfidenceInferred,
		Support: adapter.SupportUnverified,
	}
	if ev := mustAccept(t, n, Input{Candidate: c, ObservedAt: observed}); ev.EventType != "unknown" {
		t.Fatal("unknown with session must be sent")
	}
	for _, ref := range []string{"", "has space", "../path"} {
		c.SessionRef = ref
		if _, out := n.Normalize(Input{Candidate: c, ObservedAt: observed}); out != OutcomeDroppedUnknown {
			t.Fatalf("%q: %s", ref, out)
		}
	}
	known := verifiedCandidate(adapter.EventToolStarted)
	known.SessionRef = ""
	if _, out := n.Normalize(Input{Candidate: known, ObservedAt: observed}); out != OutcomeDroppedInvalid {
		t.Fatalf("known without session: %s", out)
	}
	badAgent := verifiedCandidate(adapter.EventToolStarted)
	badAgent.AgentRef = "a b"
	if _, out := n.Normalize(Input{Candidate: badAgent, ObservedAt: observed}); out != OutcomeDroppedInvalid {
		t.Fatalf("bad agent ref: %s", out)
	}
	got := n.Counters()
	if got.Accepted != 1 || got.DroppedUnknown != 3 || got.DroppedInvalid != 2 {
		t.Fatalf("counters %+v", got)
	}
}

func TestOccurredAtSelection(t *testing.T) {
	n := newNormalizer(t, Options{})
	c := verifiedCandidate(adapter.EventSessionStarted)
	earlier := observed.Add(-90 * time.Second)
	cases := []struct {
		source time.Time
		want   time.Time
	}{
		{time.Time{}, observed},
		{earlier, earlier},
		{observed.Add(4 * time.Minute), observed.Add(4 * time.Minute)},
		{observed.Add(time.Hour), observed}, // 미래 시각은 믿지 않음
		{time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), observed},
	}
	for i, tc := range cases {
		ev := mustAccept(t, n, Input{Candidate: c, ObservedAt: observed, SourceTime: tc.source})
		got, err := time.Parse(time.RFC3339Nano, ev.OccurredAt)
		if err != nil || !got.Equal(tc.want) || ev.OccurredAt[len(ev.OccurredAt)-1] != 'Z' {
			t.Fatalf("case %d: %s", i, ev.OccurredAt)
		}
	}
	if _, out := n.Normalize(Input{Candidate: c}); out != OutcomeDroppedInvalid {
		t.Fatal("missing observation time must be dropped")
	}
}

func TestSequenceIsMonotonicPerSessionAndPersisted(t *testing.T) {
	store := NewMemorySequenceStore()
	n := newNormalizer(t, Options{Sequences: store})
	s1 := verifiedCandidate(adapter.EventSessionStarted)
	s2 := s1
	s2.SessionRef = "sess-2"
	var seq1, seq2 []int64
	for i := 0; i < 3; i++ {
		seq1 = append(seq1, mustAccept(t, n, Input{Candidate: s1, ObservedAt: observed}).Sequence)
		seq2 = append(seq2, mustAccept(t, n, Input{Candidate: s2, ObservedAt: observed}).Sequence)
	}
	if seq1[0] != 0 || seq1[2] != 2 || seq2[0] != 0 || seq2[2] != 2 {
		t.Fatalf("seq %v %v", seq1, seq2)
	}
	restarted := newNormalizer(t, Options{Sequences: store})
	if got := mustAccept(t, restarted, Input{Candidate: s1, ObservedAt: observed}).Sequence; got != 3 {
		t.Fatalf("after restart %d", got)
	}
	// 중복은 sequence를 소비하지 않는다.
	dup := verifiedCandidate(adapter.EventToolStarted)
	mustAccept(t, restarted, Input{Candidate: dup, ObservedAt: observed})
	if _, out := restarted.Normalize(Input{Candidate: dup, ObservedAt: observed}); out != OutcomeDuplicate {
		t.Fatal("duplicate expected")
	}
	if got := mustAccept(t, restarted, Input{Candidate: s1, ObservedAt: observed}).Sequence; got != 5 {
		t.Fatalf("after duplicate %d", got)
	}
	// 상한에 닿으면 버리고 계속 실패한다.
	sid := SessionID(testDevice, "claude", "sess-1")
	store.Set(testDevice, sid, MaxSequence)
	if mustAccept(t, restarted, Input{Candidate: s1, ObservedAt: observed}).Sequence != MaxSequence {
		t.Fatal("max sequence must be usable")
	}
	if _, out := restarted.Normalize(Input{Candidate: s1, ObservedAt: observed}); out != OutcomeDroppedInvalid {
		t.Fatalf("exhausted: %s", out)
	}
}

func TestKeyedEventIDsAreDeterministicKeylessAreRandom(t *testing.T) {
	n := newNormalizer(t, Options{})
	keyed := verifiedCandidate(adapter.EventToolStarted)
	rec, _ := n.Normalize(Input{Candidate: keyed, ObservedAt: observed})
	want := adapter.UUIDv5(adapter.EventIDNamespace, testDevice+"|claude|sess-1|tool.started|toolu_1")
	if !rec.DeterministicID || rec.Event.EventID != want {
		t.Fatalf("keyed id %s", rec.Event.EventID)
	}
	keyless := verifiedCandidate(adapter.EventTurnCompleted)
	r1, _ := n.Normalize(Input{Candidate: keyless, ObservedAt: observed})
	r2, _ := n.Normalize(Input{Candidate: keyless, ObservedAt: observed})
	if r1.DeterministicID || r1.Event.EventID == r2.Event.EventID || !IsUUID(r1.Event.EventID) {
		t.Fatal("keyless ids must be random UUIDs")
	}
}
