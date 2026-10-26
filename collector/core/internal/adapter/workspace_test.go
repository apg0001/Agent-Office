package adapter_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
)

func TestWorkspaceNameKeepsOnlySanitizedBasename(t *testing.T) {
	long := strings.Repeat("가", 70)
	cases := []struct{ in, want string }{
		{"/home/u/src/Agent-Office", "Agent-Office"},
		{"/home/u/src/Agent-Office/", "Agent-Office"},
		{`C:\Users\u\src\blog`, "blog"},
		{`C:\Users\u\src\blog\`, "blog"},
		{`\\server\share\team repo`, "team repo"},
		{"  /x/ spaced name  ", "spaced name"},
		{"/x/a\x00b\x1fc\u200bd\u202ee\u2028f", "abcdef"},
		{"/x/" + long, strings.Repeat("가", adapter.MaxWorkspaceNameRunes)},
		{"/x/\xff\xfeok", "ok"},
		{"", ""},
		{"/", ""},
		{`C:\`, ""},
		{"/x/.", ""},
		{"/x/..", ""},
		{"/x/\u200b", ""},
		{"relative-only", "relative-only"},
	}
	for _, c := range cases {
		got := adapter.WorkspaceName(c.in)
		if got != c.want {
			t.Errorf("WorkspaceName(%q) = %q, want %q", c.in, got, c.want)
		}
		if got != "" && (!adapter.ValidWorkspaceName(got) || strings.ContainsAny(got, `/\`) || utf8.RuneCountInString(got) > 64) {
			t.Errorf("WorkspaceName(%q) = %q is not valid", c.in, got)
		}
	}
	for _, bad := range []string{"", "a/b", `a\b`, " a", "a\n", ".", "..", "C:", strings.Repeat("x", 65)} {
		if adapter.ValidWorkspaceName(bad) {
			t.Errorf("ValidWorkspaceName(%q) = true", bad)
		}
	}
	if got := adapter.WorkspaceName("/home/u/", "/home/u"); got != "" {
		t.Errorf("excluded home: %q", got)
	}
	if got := adapter.WorkspaceName(`C:\Users\U`, `c:\users\u\`); got != "" {
		t.Errorf("excluded home (case): %q", got)
	}
	if got := adapter.WorkspaceName("/home/u/proj", "/home/u"); got != "proj" {
		t.Errorf("subfolder of home: %q", got)
	}
}

// 동의가 꺼져 있으면(기본) 어떤 Hook 후보에도 cwd에서 나온 값이 없다. 켜져 있으면 마지막 이름만 있다.
func TestParseHookWorkspaceNameConsent(t *testing.T) {
	cwd := fmt.Sprintf(`,"cwd":"/home/%s/parent-%s/Agent-Office"`, sentinel, sentinel)
	runtimes := []adapter.Runtime{claude.VerifiedTargets[0].Runtime, codex.VerifiedTargets[0].Runtime, {}}
	on := adapter.HookOptions{WorkspaceName: true}
	for _, event := range hookEvents {
		payload := sensitivePayload(event, "")
		// sensitivePayload의 cwd는 객체라 이름이 나오면 안 된다. 문자열 cwd를 덧붙인 payload도 확인한다.
		withCwd := []byte(strings.Replace(string(payload), `"cwd":{`, `"cwd_obj":{`, 1))
		withCwd = append(withCwd[:len(withCwd)-1], []byte(cwd+"}")...)
		for _, rt := range runtimes {
			for _, parse := range []func([]byte, adapter.Runtime, adapter.HookOptions) (adapter.Candidate, error){claude.ParseHookWith, codex.ParseHookWith} {
				c, err := parse(withCwd, rt, adapter.HookOptions{})
				assertNoSentinel(t, "off "+event, c, err)
				if c.WorkspaceName != "" || strings.Contains(fmt.Sprintf("%#v", c), "Agent-Office") {
					t.Fatalf("off %s: workspace name set", event)
				}
				c, err = parse(payload, rt, on)
				assertNoSentinel(t, "on object cwd "+event, c, err)
				if c.WorkspaceName != "" {
					t.Fatalf("on %s: non-string cwd produced a name", event)
				}
				c, err = parse(withCwd, rt, on)
				assertNoSentinel(t, "on "+event, c, err)
				if err == nil && c.EventType != adapter.EventUnknown && c.WorkspaceName != "Agent-Office" {
					t.Fatalf("on %s: workspace %q", event, c.WorkspaceName)
				}
				if c.EventType == adapter.EventUnknown && c.WorkspaceName != "" {
					t.Fatalf("on %s: unknown event carries a name", event)
				}
			}
		}
	}
}
