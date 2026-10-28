package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/normalize"
	"github.com/apg0001/Agent-Office/collector/core/internal/queue"
	"github.com/apg0001/Agent-Office/collector/core/internal/spool"
)

// 작업 폴더 상위 경로 sentinel. 동의 여부와 관계없이 어떤 spool·큐·이벤트 바이트에도 나오면 안 된다(#181).
const sentinelParent = "SENTINEL_PARENT_DIR_DO_NOT_SEND"

func workspaceHooks(cwd string) []string {
	cwdJSON, _ := json.Marshal(cwd)
	return []string{
		`{"hook_event_name":"SessionStart","session_id":"00000000-0000-4000-8000-000000000001","source":"startup","cwd":` + string(cwdJSON) + `}`,
		`{"hook_event_name":"UserPromptSubmit","session_id":"00000000-0000-4000-8000-000000000001","prompt_id":"00000000-0000-4000-8000-000000000101","prompt":"` + sentinelPrompt + `","cwd":` + string(cwdJSON) + `}`,
	}
}

// pipelineBytes는 spool 파일을 실행 루프와 같은 경로(spool → Candidate → Pipeline → 큐)로 처리하고
// spool·큐 DB 파일·전송 payload 바이트를 모두 이어 붙여 돌려준다.
func pipelineBytes(t *testing.T, te *testEnv) (string, []normalize.Event) {
	t.Helper()
	var all bytes.Buffer
	for _, f := range spoolFiles(t, te.dirs) {
		all.Write(f)
	}
	entries, err := spool.List(te.dirs.SpoolDir(), 100, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	n, err := normalize.New(normalize.Options{DeviceID: testDeviceID})
	if err != nil {
		t.Fatal(err)
	}
	p := normalize.NewPipeline(n)
	var recs []normalize.Record
	for _, e := range entries {
		if e.Err != nil {
			t.Fatal(e.Err)
		}
		c, err := e.Record.Candidate()
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, p.Candidate(normalize.Input{Candidate: c, ObservedAt: e.Record.ObservedAt})...)
	}
	dir := t.TempDir()
	q, err := queue.Open(filepath.Join(dir, "queue.db"), queue.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := q.Enqueue(ctx, recs, nil); err != nil {
		t.Fatal(err)
	}
	items, err := q.Peek(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		all.Write(it.Payload)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	events := make([]normalize.Event, len(recs))
	for i, r := range recs {
		events[i] = r.Event
		data, _ := json.Marshal(r.Event)
		all.Write(data)
	}
	return all.String(), events
}

func TestWorkspaceNameIsNotReadWhenTurnedOff(t *testing.T) {
	te := newTestEnv(t, "u")
	te.register("https://office.example")
	if code := te.run("", "settings", "workspace-name", "off"); code != ExitOK {
		t.Fatal(code)
	}
	for _, h := range workspaceHooks("/srv/" + sentinelParent + "/Agent-Office") {
		if code := te.run(h, "hook", "claude", "--tool-version", "2.1.308", "--mode", "print"); code != ExitOK {
			t.Fatal(code)
		}
	}
	out, events := pipelineBytes(t, te)
	if len(events) != 2 {
		t.Fatalf("events %d", len(events))
	}
	for _, bad := range []string{sentinelParent, "Agent-Office", "workspace_name", "/srv/", sentinelPrompt} {
		if strings.Contains(out, bad) {
			t.Fatalf("turned off: %q leaked", bad)
		}
	}
}

func TestWorkspaceNameSendsOnlyBasenameByDefault(t *testing.T) {
	te := newTestEnv(t, "u")
	te.register("https://office.example")
	for _, h := range workspaceHooks("/srv/" + sentinelParent + "/Agent-Office/") {
		if code := te.run(h, "hook", "claude", "--tool-version", "2.1.308", "--mode", "print"); code != ExitOK {
			t.Fatal(code)
		}
	}
	// Codex(Windows 경로)도 마지막 이름만 남는다.
	codexHook := `{"hook_event_name":"UserPromptSubmit","session_id":"00000000-0000-4000-8000-000000000002","turn_id":"t-1","cwd":"C:\\Users\\` + sentinelParent + `\\blog"}`
	if code := te.run(codexHook, "hook", "codex"); code != ExitOK {
		t.Fatal(code)
	}
	out, events := pipelineBytes(t, te)
	if len(events) != 3 {
		t.Fatalf("events %d", len(events))
	}
	for _, bad := range []string{sentinelParent, "/srv/", "Users", sentinelPrompt} {
		if strings.Contains(out, bad) {
			t.Fatalf("default on: %q leaked", bad)
		}
	}
	names := map[string]int{}
	for _, e := range events {
		names[e.WorkspaceName]++
	}
	if names["Agent-Office"] != 2 || names["blog"] != 1 {
		t.Fatalf("workspace names %v", names)
	}
}

func TestWorkspaceNameSkipsHomeDirectory(t *testing.T) {
	te := newTestEnv(t, "u")
	te.register("https://office.example")
	if code := te.run("", "settings", "workspace-name", "on"); code != ExitOK {
		t.Fatal(code)
	}
	for _, h := range workspaceHooks(te.home) {
		if code := te.run(h, "hook", "claude"); code != ExitOK {
			t.Fatal(code)
		}
	}
	out, events := pipelineBytes(t, te)
	if len(events) != 2 || strings.Contains(out, "workspace_name") {
		t.Fatalf("home directory name sent: %d events", len(events))
	}
}

func TestSettingsWorkspaceNameCommand(t *testing.T) {
	te := newTestEnv(t, "u")
	if code := te.run("", "settings", "workspace-name", "on"); code != ExitError {
		t.Fatalf("unregistered on: %d", code)
	}
	te.register("https://office.example")
	statusJSON := func() bool {
		t.Helper()
		if code := te.run("", "settings", "workspace-name", "status", "--json"); code != ExitOK {
			t.Fatal(code)
		}
		var v struct {
			Sharing *bool `json:"workspace_name_sharing"`
		}
		if err := json.Unmarshal(te.stdout.Bytes(), &v); err != nil || v.Sharing == nil {
			t.Fatalf("status json %q", te.stdout)
		}
		if code := te.run("", "status", "--json"); code != ExitOK {
			t.Fatal(code)
		}
		var rep Report
		if err := json.Unmarshal(te.stdout.Bytes(), &rep); err != nil || rep.WorkspaceNameSharing != *v.Sharing {
			t.Fatalf("status report mismatch")
		}
		if !strings.Contains(te.stdout.String(), `"workspace_name_sharing"`) {
			t.Fatal("status --json must always include workspace_name_sharing")
		}
		return *v.Sharing
	}
	if !statusJSON() {
		t.Fatal("default must be on (#221)")
	}
	if code := te.run("", "settings", "workspace-name", "off"); code != ExitOK || statusJSON() {
		t.Fatal("off failed")
	}
	cfg, err := config.Load(te.dirs.ConfigFile())
	if err != nil || cfg.ShareWorkspaceName == nil || *cfg.ShareWorkspaceName || cfg.DeviceToken != sentinelToken {
		t.Fatalf("config after off must keep an explicit false: %v", err)
	}
	if code := te.run("", "settings", "workspace-name", "on"); code != ExitOK || !statusJSON() {
		t.Fatal("on failed")
	}
	for _, bad := range [][]string{
		{"settings"}, {"settings", "workspace-name"}, {"settings", "workspace-name", "maybe"},
		{"settings", "other", "on"}, {"settings", "workspace-name", "on", "--json"}, {"settings", "workspace-name", "on", "extra"},
	} {
		if code := te.run("", bad...); code != ExitUsage {
			t.Fatalf("%v: exit %d", bad, code)
		}
	}
	if strings.Contains(te.stdout.String()+te.stderr.String(), sentinelToken) {
		t.Fatal("token printed")
	}
}
