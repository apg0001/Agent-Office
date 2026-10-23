package codex

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

var fixtureDir = filepath.Join("..", "..", "..", "..", "..", "tests", "fixtures", "collector", "codex", "hooks")

var (
	cliRuntime     = VerifiedTargets[0].Runtime
	desktopRuntime = VerifiedTargets[1].Runtime
)

func readLines(t *testing.T, name string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		if l := bytes.TrimSpace(s.Bytes()); len(l) > 0 {
			lines = append(lines, append([]byte(nil), l...))
		}
	}
	return lines
}

func parseAll(t *testing.T, name string, rt adapter.Runtime) []adapter.Candidate {
	t.Helper()
	var out []adapter.Candidate
	for _, l := range readLines(t, name) {
		c, err := ParseHook(l, rt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, c)
	}
	return out
}

func TestCLIExecFixture(t *testing.T) {
	want := []adapter.EventType{
		adapter.EventSessionStarted, adapter.EventTurnStarted, adapter.EventToolStarted,
		adapter.EventTurnCompleted, adapter.EventSessionEnded,
	}
	cs := parseAll(t, "cli-exec.jsonl", cliRuntime)
	if len(cs) != len(want) {
		t.Fatalf("got %d", len(cs))
	}
	for i, c := range cs {
		if c.EventType != want[i] || c.Provider != adapter.ProviderCodex || c.Source != adapter.SourceHook ||
			c.Support != adapter.SupportVerified || c.Confidence != adapter.ConfidenceObserved {
			t.Fatalf("%d: %+v", i, c)
		}
	}
	if cs[1].TurnRef != "00000000-0000-4000-8000-000000000211" || cs[1].DedupRef() != cs[1].TurnRef {
		t.Fatalf("turn ref: %+v", cs[1])
	}
	if cs[2].ToolUseRef != "call_synthetic_0001" || cs[2].ToolCategory != adapter.ToolShell || cs[2].Activity != adapter.ActivityUnknown {
		t.Fatalf("tool: %+v", cs[2])
	}
}

func TestDesktopFixtureIsPartial(t *testing.T) {
	want := []adapter.EventType{
		adapter.EventTurnStarted, adapter.EventToolStarted, adapter.EventToolCompleted,
		adapter.EventApprovalRequested, adapter.EventAgentStarted, adapter.EventAgentCompleted,
		adapter.EventTurnCompleted, adapter.EventSessionEnded,
	}
	cs := parseAll(t, "desktop.jsonl", desktopRuntime)
	for i, c := range cs {
		if c.EventType != want[i] || c.Support != adapter.SupportPartial || c.Confidence != adapter.ConfidenceInferred {
			t.Fatalf("%d: %+v", i, c)
		}
	}
	if cs[4].AgentRef != "00000000-0000-4000-8000-000000000221" || cs[4].AgentType != adapter.AgentTypeUnknown {
		t.Fatalf("agent: %+v", cs[4])
	}
	if cs[1].DedupRef() != cs[2].DedupRef() || cs[2].DedupKind() != "tool.finished" {
		t.Fatal("pre/post must share tool_use_id")
	}
	// Desktop에서 SessionStart는 미관측이므로 지원으로 표시하지 않는다.
	c, err := ParseHook([]byte(`{"hook_event_name":"SessionStart","session_id":"s"}`), desktopRuntime)
	if err != nil || c.EventType != adapter.EventSessionStarted || c.Support != adapter.SupportUnverified {
		t.Fatalf("desktop SessionStart: %+v %v", c, err)
	}
}

func TestPostToolUseIsNotCompletedOutsideDesktop(t *testing.T) {
	post := []byte(`{"hook_event_name":"PostToolUse","session_id":"s","turn_id":"u","tool_name":"Bash","tool_use_id":"call_1"}`)
	for _, rt := range []adapter.Runtime{
		cliRuntime,
		{Product: ProductCLI, Version: "9.9.9", OS: "linux", Mode: "exec"},
		{},
	} {
		c, err := ParseHook(post, rt)
		if err != nil {
			t.Fatal(err)
		}
		if c.EventType != adapter.EventUnknown || c.ToolUseRef != "" || c.DedupRef() != "" || c.Support != adapter.SupportUnverified {
			t.Fatalf("%+v: %+v", rt, c)
		}
	}
	other := desktopRuntime
	other.Version = "26.2000.0.0"
	c, _ := ParseHook(post, other)
	if c.EventType != adapter.EventToolCompleted || c.Support != adapter.SupportUnverified || c.Confidence != adapter.ConfidenceInferred {
		t.Fatalf("unverified desktop: %+v", c)
	}
}

func TestCodexErrorsAndUnknown(t *testing.T) {
	cases := map[string]struct {
		payload string
		err     error
		event   adapter.EventType
	}{
		"malformed":      {`{`, adapter.ErrMalformed, ""},
		"no-session":     {`{"hook_event_name":"Stop"}`, adapter.ErrMissingSession, ""},
		"tool-no-id":     {`{"hook_event_name":"PreToolUse","session_id":"s"}`, adapter.ErrMissingRef, ""},
		"agent-no-id":    {`{"hook_event_name":"SubagentStop","session_id":"s"}`, adapter.ErrMissingRef, ""},
		"unknown-event":  {`{"hook_event_name":"PreCompact","session_id":"s"}`, nil, adapter.EventUnknown},
		"interrupt":      {`{"hook_event_name":"Interrupt","session_id":"s"}`, nil, adapter.EventUnknown},
		"non-string-evt": {`{"hook_event_name":5,"session_id":"s"}`, nil, adapter.EventUnknown},
	}
	for name, tc := range cases {
		c, err := ParseHook([]byte(tc.payload), cliRuntime)
		if !errors.Is(err, tc.err) || c.EventType != tc.event {
			t.Errorf("%s: %+v %v", name, c, err)
		}
	}
	if _, err := ParseHook(make([]byte, adapter.MaxHookPayloadBytes+1), cliRuntime); !errors.Is(err, adapter.ErrTooLarge) {
		t.Errorf("oversized: %v", err)
	}
}

func TestCodexToolCategory(t *testing.T) {
	cases := map[string]adapter.Activity{
		"Bash": adapter.ActivityUnknown, "apply_patch": adapter.ActivityCoding,
		"update_plan": adapter.ActivityUnknown, "mcp__x__y": adapter.ActivityUnknown,
		"spawn_agent": adapter.ActivityUnknown, "Read": adapter.ActivityUnknown,
	}
	for name, want := range cases {
		c, err := ParseHook([]byte(fmt.Sprintf(`{"hook_event_name":"PreToolUse","session_id":"s","tool_use_id":"c","tool_name":%q}`, name)), cliRuntime)
		if err != nil || c.Activity != want {
			t.Errorf("%s: %s %v", name, c.Activity, err)
		}
	}
}

func TestCodexFixturesHoldNoRawContent(t *testing.T) {
	forbidden := regexp.MustCompile(`(/home/|/Users/|[A-Za-z]:\\\\|/tmp/|sk-|ghp_|@[a-z]+\.[a-z]+)`)
	sensitive := []string{`"prompt"`, `"tool_input"`, `"tool_response"`, `"last_assistant_message"`, `"cwd"`, `"transcript_path"`, `"agent_transcript_path"`, `"model"`}
	for _, name := range []string{"cli-exec.jsonl", "desktop.jsonl"} {
		for _, line := range readLines(t, name) {
			s := string(line)
			if forbidden.MatchString(s) {
				t.Fatalf("%s: forbidden pattern", name)
			}
			obj, err := adapter.DecodeObject(line, adapter.MaxHookPayloadBytes)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range sensitive {
				raw, ok := obj[strings.Trim(key, `"`)]
				if ok && !bytes.Contains(raw, []byte("<synthetic>")) {
					t.Fatalf("%s: %s must be <synthetic>", name, key)
				}
			}
		}
	}
}
