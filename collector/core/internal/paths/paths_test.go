package paths

import "testing"

func envOf(m map[string]string) Env { return func(k string) string { return m[k] } }

func TestResolvePerOS(t *testing.T) {
	cases := []struct {
		name, goos, home string
		env              map[string]string
		want             Dirs
	}{
		{
			name: "linux default", goos: "linux", home: "/home/u", env: map[string]string{},
			want: Dirs{Config: "/home/u/.config/agent-office/collector", State: "/home/u/.local/state/agent-office/collector", ClaudeProjects: "/home/u/.claude/projects", CodexHome: "/home/u/.codex"},
		},
		{
			name: "linux xdg", goos: "linux", home: "/home/u",
			env:  map[string]string{"XDG_CONFIG_HOME": "/x/c", "XDG_STATE_HOME": "/x/s", "CLAUDE_CONFIG_DIR": "/x/claude", "CODEX_HOME": "/x/codex"},
			want: Dirs{Config: "/x/c/agent-office/collector", State: "/x/s/agent-office/collector", ClaudeProjects: "/x/claude/projects", CodexHome: "/x/codex"},
		},
		{
			name: "linux relative xdg ignored", goos: "linux", home: "/home/u",
			env:  map[string]string{"XDG_CONFIG_HOME": "rel", "XDG_STATE_HOME": "rel2", "CLAUDE_CONFIG_DIR": "rel3", "CODEX_HOME": "rel4"},
			want: Dirs{Config: "/home/u/.config/agent-office/collector", State: "/home/u/.local/state/agent-office/collector", ClaudeProjects: "/home/u/.claude/projects", CodexHome: "/home/u/.codex"},
		},
		{
			name: "darwin", goos: "darwin", home: "/Users/u", env: map[string]string{},
			want: Dirs{Config: "/Users/u/Library/Application Support/AgentOffice/Collector", State: "/Users/u/Library/Application Support/AgentOffice/Collector/state", ClaudeProjects: "/Users/u/.claude/projects", CodexHome: "/Users/u/.codex"},
		},
		{
			name: "windows", goos: "windows", home: `C:\Users\u`,
			env:  map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`, "LOCALAPPDATA": `C:\Users\u\AppData\Local`},
			want: Dirs{Config: `C:\Users\u\AppData\Roaming\AgentOffice\Collector`, State: `C:\Users\u\AppData\Local\AgentOffice\Collector`, ClaudeProjects: `C:\Users\u\.claude\projects`, CodexHome: `C:\Users\u\.codex`},
		},
		{
			name: "override", goos: "linux", home: "/home/u", env: map[string]string{EnvHome: "/opt/ao"},
			want: Dirs{Config: "/opt/ao/config", State: "/opt/ao/state", ClaudeProjects: "/home/u/.claude/projects", CodexHome: "/home/u/.codex"},
		},
	}
	for _, c := range cases {
		got, err := Resolve(c.goos, envOf(c.env), c.home)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Config != c.want.Config || got.State != c.want.State || got.ClaudeProjects != c.want.ClaudeProjects || got.CodexHome != c.want.CodexHome {
			t.Fatalf("%s: got %+v", c.name, got)
		}
	}
	if _, err := Resolve("windows", envOf(map[string]string{}), `C:\Users\u`); err != ErrUnavailable {
		t.Fatalf("windows without APPDATA: %v", err)
	}
	if _, err := Resolve("linux", envOf(nil), ""); err != ErrUnavailable {
		t.Fatalf("no home: %v", err)
	}
}

func TestFileNames(t *testing.T) {
	d, _ := Resolve("windows", envOf(map[string]string{"APPDATA": `C:\a`, "LOCALAPPDATA": `C:\l`}), `C:\u`)
	if d.ConfigFile() != `C:\a\AgentOffice\Collector\config.json` || d.QueueFile() != `C:\l\AgentOffice\Collector\queue.db` {
		t.Fatalf("%s %s", d.ConfigFile(), d.QueueFile())
	}
}
