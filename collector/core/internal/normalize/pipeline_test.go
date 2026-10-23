package normalize

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"
)

// #20 합성 이중 수집 fixture(Python 참조 구현·#29·#30과 공유).
var fixtureDir = filepath.Join("..", "..", "..", "..", "tests", "fixtures", "collector", "claude", "dual-source")

var claudeRuntime = claude.VerifiedTargets[0].Runtime

type expectedResult struct {
	AcceptedDedupKeys        []string          `json:"accepted_dedup_keys"`
	SuppressedWhenHooksFirst int               `json:"suppressed_when_hooks_first"`
	ToolFinishedEventType    map[string]string `json:"tool_finished_event_type"`
}

func loadExpected(t *testing.T) expectedResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e expectedResult
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func fixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 {
			out = append(out, append([]byte(nil), line...))
		}
	}
	return out
}

// tailFixture는 fixture transcript를 #30 tail로 읽는다(읽기 전용).
func tailFixture(t *testing.T, name string) tail.Result {
	t.Helper()
	res := tail.PollOnce(filepath.Join(fixtureDir, name), tail.Checkpoint{}, tail.Options{})
	if res.Status != tail.StatusOK || len(res.Records) == 0 {
		t.Fatalf("tail %s: %s", name, res.Status)
	}
	return res
}

func runDualSource(t *testing.T, p *Pipeline, hooksFirst bool) []Record {
	t.Helper()
	hooks := func() []Record {
		var out []Record
		for _, line := range fixtureLines(t, "hooks.jsonl") {
			out = append(out, p.Hook(adapter.ProviderClaude, claudeRuntime, line, observed)...)
		}
		return out
	}
	transcripts := func() []Record {
		var out []Record
		for _, name := range []string{"transcript.jsonl", "subagent-transcript.jsonl"} {
			out = append(out, p.Transcript(tailFixture(t, name), claudeRuntime, observed)...)
		}
		return out
	}
	if hooksFirst {
		return append(hooks(), transcripts()...)
	}
	return append(transcripts(), hooks()...)
}

func TestDualSourceFixtureEndToEnd(t *testing.T) {
	exp := loadExpected(t)
	schema := loadSchema(t)
	hookCount := len(fixtureLines(t, "hooks.jsonl"))
	for _, hooksFirst := range []bool{true, false} {
		n := newNormalizer(t, Options{})
		p := NewPipeline(n)
		recs := runDualSource(t, p, hooksFirst)

		keyedIDs := map[string]bool{}
		finished := map[string]string{}
		seqBySession := map[string][]int64{}
		var lead string
		for _, r := range recs {
			raw := mustMarshal(t, r.Event)
			if err := validateEvent(schema, raw); err != nil {
				t.Fatalf("schema: %v", err)
			}
			ev := r.Event
			if r.DeterministicID {
				if keyedIDs[ev.EventID] {
					t.Fatal("duplicate keyed event emitted")
				}
				keyedIDs[ev.EventID] = true
			}
			if ev.EventType == "tool.completed" || ev.EventType == "tool.failed" {
				finished[ev.EventID] = ev.EventType
			}
			if ev.ParentAgentID == nil {
				if lead != "" && lead != ev.AgentID {
					t.Fatal("one lead agent per session")
				}
				lead = ev.AgentID
			}
			seqBySession[ev.SessionID] = append(seqBySession[ev.SessionID], ev.Sequence)
		}
		// 통과 키 집합 = 참조 구현의 accepted_dedup_keys(장치만 UUID로 바뀜).
		want := map[string]bool{}
		for _, key := range exp.AcceptedDedupKeys {
			_, rest, _ := cutFirst(key)
			want[adapter.UUIDv5(adapter.EventIDNamespace, testDevice+"|"+rest)] = true
		}
		if len(keyedIDs) != len(want) {
			t.Fatalf("hooksFirst=%v keyed=%d want %d", hooksFirst, len(keyedIDs), len(want))
		}
		for id := range want {
			if !keyedIDs[id] {
				t.Fatalf("hooksFirst=%v missing keyed event", hooksFirst)
			}
		}
		if len(finished) != len(exp.ToolFinishedEventType) {
			t.Fatalf("finished %v", finished)
		}
		for _, r := range recs {
			if r.Event.ParentAgentID != nil && *r.Event.ParentAgentID != lead {
				t.Fatal("subagent parent must be the lead agent")
			}
		}
		for sid, seqs := range seqBySession {
			for i, s := range seqs {
				if s != int64(i) {
					t.Fatalf("session %s sequence %v", sid, seqs)
				}
			}
		}
		got := p.Counters()
		if hooksFirst {
			// Hook 13건이 모두 통과하고 transcript 키 이벤트 8건은 중복으로 억제된다.
			if len(recs) != hookCount || got.DuplicateSuppressed != exp.SuppressedWhenHooksFirst {
				t.Fatalf("hooks first: recs=%d counters=%+v", len(recs), got)
			}
		}
		if got.Accepted != len(recs) || got.DroppedInvalid != 0 || got.DroppedUnknown != 0 || got.IgnoredLines == 0 {
			t.Fatalf("counters %+v", got)
		}
	}
}

func TestDualSourceRestartWithPersistedStateEmitsNoReplays(t *testing.T) {
	store := NewMemorySequenceStore()
	first := NewPipeline(newNormalizer(t, Options{Sequences: store}))
	runDualSource(t, first, true)
	restarted := NewPipeline(newNormalizer(t, Options{Sequences: store, SeenKeys: first.Normalizer().SeenKeys()}))
	var replay []Record
	for _, name := range []string{"transcript.jsonl", "subagent-transcript.jsonl"} {
		replay = append(replay, restarted.Transcript(tailFixture(t, name), claudeRuntime, observed)...)
	}
	if len(replay) != 0 {
		t.Fatalf("replayed %d", len(replay))
	}
}

func cutFirst(key string) (string, string, bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}

func TestTranscriptTimestampUsedOnlyWhenValid(t *testing.T) {
	stamps := []string{
		`"2026-10-22T15:59:00.123Z"`,
		`"2026-10-22T15:59:00"`,       // 시간대 없음
		`"9999-12-31T00:00:00Z"`,      // 미래
		`"yesterday afternoon"`,       // 형식 아님
		`1761148740`,                  // 숫자
		`"2026-10-23T00:59:00+09:00"`, // 오프셋 → UTC
	}
	var res tail.Result
	for i, ts := range stamps {
		res.Records = append(res.Records, []byte(`{"type":"assistant","sessionId":"s1","timestamp":`+ts+
			`,"message":{"content":[{"type":"tool_use","id":"t-`+string(rune('a'+i))+`","name":"Read"}]}}`))
	}
	p := NewPipeline(newNormalizer(t, Options{}))
	recs := p.Transcript(res, claudeRuntime, observed)
	want := []string{
		"2026-10-22T15:59:00.123Z",
		"2026-10-22T16:02:03Z", "2026-10-22T16:02:03Z", "2026-10-22T16:02:03Z", "2026-10-22T16:02:03Z",
		"2026-10-22T15:59:00Z",
	}
	if len(recs) != len(want) {
		t.Fatalf("recs %d", len(recs))
	}
	for i, r := range recs {
		if r.Event.OccurredAt != want[i] {
			t.Fatalf("%d: %s want %s", i, r.Event.OccurredAt, want[i])
		}
	}
}

func TestCodexCLINeverEmitsToolCompleted(t *testing.T) {
	cli := codex.VerifiedTargets[0].Runtime
	desktop := codex.VerifiedTargets[1].Runtime
	payload := []byte(`{"hook_event_name":"PostToolUse","session_id":"s1","turn_id":"u1","tool_use_id":"t1","tool_name":"Bash"}`)
	for _, rt := range []adapter.Runtime{cli, {}, {Product: codex.ProductCLI, Version: "9.9.9"}} {
		p := NewPipeline(newNormalizer(t, Options{}))
		recs := p.Hook(adapter.ProviderCodex, rt, payload, observed)
		if len(recs) != 1 || recs[0].Event.EventType != "unknown" || recs[0].Event.Confidence != "inferred" {
			t.Fatalf("codex %+v: %+v", rt, recs)
		}
	}
	p := NewPipeline(newNormalizer(t, Options{}))
	if recs := p.Hook(adapter.ProviderCodex, desktop, payload, observed); len(recs) != 1 || recs[0].Event.EventType != "tool.completed" || recs[0].Event.Confidence != "inferred" {
		t.Fatalf("desktop: %+v", recs)
	}
	// 어댑터가 바뀌어 CLI 완료 후보를 내도 조립 단계에서 막는다.
	c := adapter.Candidate{Provider: adapter.ProviderCodex, Source: adapter.SourceHook, EventType: adapter.EventToolCompleted,
		SessionRef: "s1", ToolUseRef: "t1", Activity: adapter.ActivityCoding, Confidence: adapter.ConfidenceObserved, Support: adapter.SupportVerified}
	for _, et := range []adapter.EventType{adapter.EventToolCompleted, adapter.EventToolFailed} {
		c.EventType = et
		if g := guardCodexCompletion(c, cli); g.EventType != adapter.EventUnknown || g.ToolUseRef != "" || g.Confidence != adapter.ConfidenceInferred {
			t.Fatalf("guard %s: %+v", et, g)
		}
	}
	if g := guardCodexCompletion(c, desktop); g.EventType != c.EventType {
		t.Fatal("desktop completion must pass")
	}
}

func TestPipelineFailOpenCounters(t *testing.T) {
	p := NewPipeline(newNormalizer(t, Options{}))
	inputs := [][]byte{
		[]byte(`not json`),
		[]byte(`{"hook_event_name":"PreToolUse"}`),                        // 세션 없음
		[]byte(`{"hook_event_name":"PreToolUse","session_id":"s1"}`),      // tool_use_id 없음
		[]byte(`{"hook_event_name":"Notification","session_id":"s1"}`),    // unknown 통과
		[]byte(`{"hook_event_name":"SessionStart","session_id":"s 1"}`),   // 식별자 형식 아님
		[]byte(`{"hook_event_name":"SessionStart","session_id":"s1"}`),    // 통과
		bytes.Repeat([]byte{'x'}, adapter.MaxHookPayloadBytes+1),          // 상한 초과
		[]byte(`{"hook_event_name":"SessionStart","session_id":["s1"]}`),  // 타입 오류
		[]byte(`{"hook_event_name":"SubagentStart","session_id":"s1"}`),   // agent_id 없음
		[]byte(`{"hook_event_name":"SessionStart","session_id":"s1"}   `), // 통과(키 없음)
	}
	var recs []Record
	for _, in := range inputs {
		recs = append(recs, p.Hook(adapter.ProviderClaude, claudeRuntime, in, observed)...)
	}
	recs = append(recs, p.Hook(adapter.Provider("other"), claudeRuntime, inputs[5], observed)...)
	res := tail.Result{Records: [][]byte{
		[]byte(`{"type":"api-request-blob","sessionId":"s1","message":{"content":[]}}`),
		[]byte(`{"type":"assistant","message":{"content":[]}}`),
		[]byte(`{"type":"assistant","sessionId":"s1","message":"x"}`),
	}}
	recs = append(recs, p.Transcript(res, claudeRuntime, observed)...)
	got := p.Counters()
	want := Counters{Accepted: 3, DroppedInvalid: 10, IgnoredLines: 1}
	if got != want || len(recs) != 3 {
		t.Fatalf("counters %+v recs %d", got, len(recs))
	}
	types := []string{}
	for _, r := range recs {
		types = append(types, r.Event.EventType)
	}
	sort.Strings(types)
	if types[2] != "unknown" {
		t.Fatalf("types %v", types)
	}
}
