package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

// 언어 중립 fixture(#20)를 Python 참조 구현과 공유한다.
var fixtureDir = filepath.Join("..", "..", "..", "..", "..", "tests", "fixtures", "collector", "claude", "dual-source")

var verifiedRuntime = VerifiedTargets[0].Runtime

type expectedResult struct {
	DeviceRef                  string            `json:"device_ref"`
	AcceptedDedupKeys          []string          `json:"accepted_dedup_keys"`
	KeylessEventTypesFromHooks []string          `json:"keyless_event_types_from_hooks"`
	SuppressedWhenHooksFirst   int               `json:"suppressed_when_hooks_first"`
	ToolFinishedEventType      map[string]string `json:"tool_finished_event_type"`
}

// pythonEventIDs는 Python uuid.uuid5(EVENT_ID_NAMESPACE, key)로 계산한 기대값이다.
var pythonEventIDs = map[string]string{
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|agent.completed|a0000000000000001":                 "073de7de-d48f-5c73-90a0-8c3d5557046a",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|agent.started|a0000000000000001":                   "db97aeda-cc72-553e-8f33-5a140c4a8713",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.finished|toolu_synthetic_0001":                "63caff8d-ea92-5ad2-b04d-fe9c160c1901",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.finished|toolu_synthetic_0002":                "94aacb87-b01c-530a-8ac8-97b73910473b",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.finished|toolu_synthetic_agent":               "6e0599dd-406a-5048-8542-2b8ba3f10a44",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.started|toolu_synthetic_0001":                 "4dfd1880-2479-5d5c-a596-2d4aece97e4d",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.started|toolu_synthetic_0002":                 "f6b9c634-b9ee-5aa4-9cb8-0cd86a0edfca",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|tool.started|toolu_synthetic_agent":                "4c253d43-e8a2-5074-8614-8c2675ad18a7",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|turn.started|00000000-0000-4000-8000-000000000101": "c28b85af-46c2-5da5-9d0e-a984aff0bb8f",
	"device-synthetic-1|claude|00000000-0000-4000-8000-000000000001|turn.started|00000000-0000-4000-8000-000000000102": "ffa6de77-e853-5234-b718-980cd24861dc",
}

func readLines(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			lines = append(lines, append([]byte(nil), line...))
		}
	}
	return lines
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

func hookCandidates(t *testing.T) []adapter.Candidate {
	t.Helper()
	var out []adapter.Candidate
	for _, line := range readLines(t, "hooks.jsonl") {
		c, err := ParseHook(line, verifiedRuntime)
		if err != nil {
			t.Fatalf("hook parse: %v", err)
		}
		out = append(out, c)
	}
	return out
}

func transcriptCandidates(t *testing.T) []adapter.Candidate {
	t.Helper()
	var out []adapter.Candidate
	for _, name := range []string{"transcript.jsonl", "subagent-transcript.jsonl"} {
		for _, line := range readLines(t, name) {
			cs, err := ParseTranscriptLine(line, verifiedRuntime)
			if err != nil {
				t.Fatalf("transcript parse: %v", err)
			}
			out = append(out, cs...)
		}
	}
	return out
}

func run(device string, cs []adapter.Candidate, d *adapter.Deduplicator) []string {
	var accepted []string
	for _, c := range cs {
		if d.Accept(c) {
			if key := adapter.DedupKey(device, c); key != "" {
				accepted = append(accepted, key)
			}
		}
	}
	return accepted
}

func TestDualSourceDedupIsOrderIndependent(t *testing.T) {
	exp := loadExpected(t)
	hooks, lines := hookCandidates(t), transcriptCandidates(t)
	for _, hooksFirst := range []bool{true, false} {
		d := adapter.NewDeduplicator(exp.DeviceRef)
		all := append(append([]adapter.Candidate{}, lines...), hooks...)
		if hooksFirst {
			all = append(append([]adapter.Candidate{}, hooks...), lines...)
		}
		accepted := run(exp.DeviceRef, all, d)
		sort.Strings(accepted)
		if !equalStrings(accepted, exp.AcceptedDedupKeys) {
			t.Fatalf("hooksFirst=%v accepted=%v", hooksFirst, accepted)
		}
		if hooksFirst && d.Suppressed() != exp.SuppressedWhenHooksFirst {
			t.Fatalf("suppressed=%d want %d", d.Suppressed(), exp.SuppressedWhenHooksFirst)
		}
	}
}

func TestHookAndTranscriptProduceIdenticalEventIDs(t *testing.T) {
	exp := loadExpected(t)
	byKey := map[string]map[string]bool{}
	sources := map[string]map[adapter.Source]bool{}
	for _, c := range append(hookCandidates(t), transcriptCandidates(t)...) {
		key := adapter.DedupKey(exp.DeviceRef, c)
		if key == "" {
			continue
		}
		id, deterministic, err := adapter.EventID(exp.DeviceRef, c)
		if err != nil || !deterministic {
			t.Fatalf("keyed candidate must have deterministic id: %v", err)
		}
		if byKey[key] == nil {
			byKey[key], sources[key] = map[string]bool{}, map[adapter.Source]bool{}
		}
		byKey[key][id] = true
		sources[key][c.Source] = true
	}
	if len(byKey) != len(exp.AcceptedDedupKeys) {
		t.Fatalf("keys=%d want %d", len(byKey), len(exp.AcceptedDedupKeys))
	}
	for _, key := range exp.AcceptedDedupKeys {
		ids := byKey[key]
		if len(ids) != 1 || !ids[pythonEventIDs[key]] {
			t.Fatalf("%s: ids=%v want %s", key, ids, pythonEventIDs[key])
		}
	}
	// agent.*를 제외한 키는 두 소스 모두에서 관측돼야 교차 중복 제거 근거가 된다.
	for key, src := range sources {
		isAgent := bytes.Contains([]byte(key), []byte("|agent."))
		if !isAgent && !(src[adapter.SourceHook] && src[adapter.SourceJSONL]) {
			t.Errorf("%s not seen in both sources: %v", key, src)
		}
	}
}

func TestRestartWithPersistedKeysReplaysNothing(t *testing.T) {
	exp := loadExpected(t)
	first := adapter.NewDeduplicator(exp.DeviceRef)
	run(exp.DeviceRef, hookCandidates(t), first)
	restarted := adapter.NewDeduplicator(exp.DeviceRef, first.Seen()...)
	if got := run(exp.DeviceRef, transcriptCandidates(t), restarted); len(got) != 0 {
		t.Fatalf("replayed %v", got)
	}
}

func TestToolFinishedEventTypes(t *testing.T) {
	exp := loadExpected(t)
	for name, cs := range map[string][]adapter.Candidate{"hook": hookCandidates(t), "jsonl": transcriptCandidates(t)} {
		got := map[string]string{}
		for _, c := range cs {
			if c.EventType == adapter.EventToolCompleted || c.EventType == adapter.EventToolFailed {
				got[c.ToolUseRef] = string(c.EventType)
			}
		}
		if len(got) != len(exp.ToolFinishedEventType) {
			t.Fatalf("%s: %v", name, got)
		}
		for ref, want := range exp.ToolFinishedEventType {
			if got[ref] != want {
				t.Fatalf("%s %s: %s want %s", name, ref, got[ref], want)
			}
		}
	}
}

func TestKeylessHookEventsAreNeverSuppressed(t *testing.T) {
	exp := loadExpected(t)
	var keyless []adapter.Candidate
	types := map[string]bool{}
	for _, c := range hookCandidates(t) {
		if adapter.DedupKey(exp.DeviceRef, c) == "" {
			keyless = append(keyless, c)
			types[string(c.EventType)] = true
		}
	}
	if len(types) != len(exp.KeylessEventTypesFromHooks) {
		t.Fatalf("keyless types %v", types)
	}
	for _, et := range exp.KeylessEventTypesFromHooks {
		if !types[et] {
			t.Fatalf("missing keyless %s", et)
		}
	}
	d := adapter.NewDeduplicator(exp.DeviceRef)
	for _, c := range append(keyless, keyless...) {
		if !d.Accept(c) {
			t.Fatal("keyless suppressed")
		}
	}
}

func TestSubagentToolsAreAttributedToTheSubagent(t *testing.T) {
	for _, c := range append(hookCandidates(t), transcriptCandidates(t)...) {
		switch c.ToolUseRef {
		case "toolu_synthetic_0002":
			if c.AgentRef != "a0000000000000001" {
				t.Fatalf("%s %s owner=%q", c.Source, c.EventType, c.AgentRef)
			}
		case "toolu_synthetic_0001", "toolu_synthetic_agent":
			if c.AgentRef != "" {
				t.Fatalf("%s %s owner=%q", c.Source, c.EventType, c.AgentRef)
			}
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
