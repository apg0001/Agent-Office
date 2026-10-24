package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
)

// TestQueueStoresPlanStatusesOnly는 TodoWrite·update_plan 문장(content·activeForm·step·explanation)이
// 큐 DB(WAL 포함)에 남지 않고 상태 enum만 저장되는지 확인한다(#38).
func TestQueueStoresPlanStatusesOnly(t *testing.T) {
	dir := t.TempDir()
	q, err := Open(filepath.Join(dir, "queue.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := newPipeline(t, q)
	todo := fmt.Sprintf(`,"tool_use_id":"tu-1","tool_name":"TodoWrite","tool_input":{"todos":[{"content":"%[1]s /home/%[1]s","activeForm":"%[1]s","status":"in_progress"},{"content":"%[1]s","status":"%[1]s"}]}`, sentinel)
	recs := process(t, p, [][]byte{hook("PreToolUse", "s-1", todo), hook("PostToolUse", "s-1", todo)})
	plan := fmt.Sprintf(`{"hook_event_name":"PreToolUse","session_id":"s-2","tool_use_id":"c-1","tool_name":"update_plan","tool_input":{"explanation":"%[1]s","plan":[{"step":"%[1]s /home/x","status":"completed"}]}}`, sentinel)
	recs = append(recs, p.Hook(adapter.ProviderCodex, codex.VerifiedTargets[0].Runtime, []byte(plan), observed)...)
	plans := 0
	for _, r := range recs {
		if r.Event.EventType == "plan.updated" {
			plans++
		}
	}
	if plans != 2 {
		t.Fatalf("plan events %d of %d", plans, len(recs))
	}
	if _, err := q.Enqueue(ctx, recs, nil); err != nil {
		t.Fatal(err)
	}
	items, err := q.Peek(ctx, 100)
	if err != nil || len(items) != len(recs) {
		t.Fatalf("peek %d %v", len(items), err)
	}
	stored := 0
	for _, it := range items {
		if strings.Contains(string(it.Payload), `"plan_steps":["in_progress","unknown"]`) || strings.Contains(string(it.Payload), `"plan_steps":["completed"]`) {
			stored++
		}
	}
	if stored != 2 {
		t.Fatalf("stored plan payloads %d", stored)
	}
	check := func(label string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), sentinel) || strings.Contains(string(data), "/home/") {
				t.Fatalf("%s: raw plan text in %s", label, e.Name())
			}
		}
	}
	check("before close")
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	check("after close")
}
