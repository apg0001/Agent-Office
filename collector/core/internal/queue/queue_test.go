package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/normalize"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"
)

const testDevice = "11111111-2222-4333-8444-555555555555"

// sentinel은 식별자 형식도 통과하므로 원본 ID 자리에도 넣을 수 있다.
const sentinel = "SENTINELk3v"

var (
	ctx      = context.Background()
	observed = time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC)
	runtime  = claude.VerifiedTargets[0].Runtime
)

func openTemp(t *testing.T, opts Options) (*Queue, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "queue.db")
	q, err := Open(path, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q, path
}

func reopen(t *testing.T, q *Queue, path string, opts Options) *Queue {
	t.Helper()
	if err := q.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	q2, err := Open(path, opts)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = q2.Close() })
	return q2
}

func newPipeline(t *testing.T, q *Queue) *normalize.Pipeline {
	t.Helper()
	seen, err := q.SeenEventIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, err := normalize.New(normalize.Options{DeviceID: testDevice, Sequences: q, SeenEventIDs: seen})
	if err != nil {
		t.Fatal(err)
	}
	return normalize.NewPipeline(n)
}

func hook(event, session, extra string) []byte {
	return []byte(fmt.Sprintf(`{"hook_event_name":%q,"session_id":%q%s}`, event, session, extra))
}

// sessionHooks는 키 있는 이벤트(turn·tool)와 키 없는 이벤트(session.started)를 섞는다.
func sessionHooks(session string) [][]byte {
	return [][]byte{
		hook("SessionStart", session, `,"source":"startup"`),
		hook("UserPromptSubmit", session, `,"prompt_id":"p-1","prompt":"secret"`),
		hook("PreToolUse", session, `,"tool_use_id":"tu-1","tool_name":"Bash","tool_input":{"command":"ls"}`),
		hook("PostToolUse", session, `,"tool_use_id":"tu-1","tool_name":"Bash","tool_response":"out"`),
	}
}

func process(t *testing.T, p *normalize.Pipeline, payloads [][]byte) []normalize.Record {
	t.Helper()
	var out []normalize.Record
	for _, b := range payloads {
		out = append(out, p.Hook(adapter.ProviderClaude, runtime, b, observed)...)
	}
	return out
}

func eventIDs(items []Item) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.EventID
	}
	return ids
}

func TestOpenCreatesSchemaWithPragmasAndPrivateFile(t *testing.T) {
	q, path := openTemp(t, Options{})
	var v int
	var mode, sync string
	if err := q.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("user_version %d %v", v, err)
	}
	if err := q.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q %v", mode, err)
	}
	if err := q.db.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil || sync != "2" {
		t.Fatalf("synchronous %q %v", sync, err)
	}
	var busy int
	if err := q.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil || busy != int(DefaultBusyTimeout.Milliseconds()) {
		t.Fatalf("busy_timeout %d %v", busy, err)
	}
	if fi, err := os.Stat(path); err != nil || (os.PathSeparator == '/' && fi.Mode().Perm() != 0o600) {
		t.Fatalf("file mode %v %v", fi.Mode(), err)
	}
	// 같은 버전으로 다시 열어도 마이그레이션이 반복되지 않는다.
	reopen(t, q, path, Options{})
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	q, path := openTemp(t, Options{})
	if _, err := q.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	if _, err := Open(path, Options{}); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("got %v", err)
	}
}

func TestEnqueueFixesEventIDAndAckDeletes(t *testing.T) {
	q, path := openTemp(t, Options{})
	recs := process(t, newPipeline(t, q), sessionHooks("sess-a"))
	if len(recs) != 4 {
		t.Fatalf("records %d", len(recs))
	}
	random := 0
	for _, r := range recs {
		if !r.DeterministicID {
			random++
		}
	}
	if random == 0 {
		t.Fatal("expected a random event_id record")
	}
	res, err := q.Enqueue(ctx, recs, nil)
	if err != nil || res.Enqueued != 4 || q.Depth() != 4 {
		t.Fatalf("enqueue %+v %v depth=%d", res, err, q.Depth())
	}
	items, err := q.Peek(ctx, 10)
	if err != nil || len(items) != 4 {
		t.Fatalf("peek %d %v", len(items), err)
	}
	for i, it := range items {
		want, _ := json.Marshal(recs[i].Event)
		if string(it.Payload) != string(want) || it.EventID != recs[i].Event.EventID {
			t.Fatalf("payload or event id changed at %d", i)
		}
	}
	// 재시작해도 같은 event_id(무작위 포함)가 같은 순서로 남는다.
	q = reopen(t, q, path, Options{})
	again, err := q.Peek(ctx, 10)
	if err != nil || strings.Join(eventIDs(again), ",") != strings.Join(eventIDs(items), ",") {
		t.Fatalf("ids changed after restart: %v", err)
	}
	n, err := q.Ack(ctx, []string{items[0].EventID, items[2].EventID, "99999999-9999-4999-8999-999999999999"})
	if err != nil || n != 2 || q.Depth() != 2 {
		t.Fatalf("ack %d %v depth=%d", n, err, q.Depth())
	}
	left, _ := q.Peek(ctx, 10)
	if len(left) != 2 || left[0].EventID != items[1].EventID || left[1].EventID != items[3].EventID {
		t.Fatalf("left %v", eventIDs(left))
	}
}

func TestRestartPreservesSequenceAndDedup(t *testing.T) {
	q, path := openTemp(t, Options{})
	first := process(t, newPipeline(t, q), sessionHooks("sess-r"))
	if _, err := q.Enqueue(ctx, first, nil); err != nil {
		t.Fatal(err)
	}
	// 전송·ACK로 큐가 비어도 중복 제거 키와 sequence는 남는다.
	items, _ := q.Peek(ctx, 10)
	if _, err := q.Ack(ctx, eventIDs(items)); err != nil {
		t.Fatal(err)
	}

	q = reopen(t, q, path, Options{})
	p := newPipeline(t, q)
	replay := process(t, p, sessionHooks("sess-r"))
	// 키 있는 3건(turn.started, tool.started, tool.completed)은 재방출되지 않고, 키 없는
	// session.started만 새 이벤트가 된다.
	if len(replay) != 1 || replay[0].Event.EventType != "session.started" {
		t.Fatalf("replayed %d", len(replay))
	}
	if p.Counters().DuplicateSuppressed != 3 {
		t.Fatalf("counters %+v", p.Counters())
	}
	last := first[len(first)-1].Event.Sequence
	if replay[0].Event.Sequence != last+1 {
		t.Fatalf("sequence %d after %d", replay[0].Event.Sequence, last)
	}
	if _, err := q.Enqueue(ctx, replay, nil); err != nil {
		t.Fatal(err)
	}
	q = reopen(t, q, path, Options{})
	next, err := q.Next(testDevice, first[0].Event.SessionID)
	if err != nil || next != last+2 {
		t.Fatalf("next %d %v", next, err)
	}
}

func TestSequenceNotPersistedWithoutEnqueue(t *testing.T) {
	q, path := openTemp(t, Options{})
	sess := normalize.SessionID(testDevice, "claude", "s")
	for i := int64(0); i < 3; i++ {
		if v, err := q.Next(testDevice, sess); err != nil || v != i {
			t.Fatalf("next %d %v", v, err)
		}
	}
	q = reopen(t, q, path, Options{})
	if v, _ := q.Next(testDevice, sess); v != 0 {
		t.Fatalf("unenqueued sequence persisted: %d", v)
	}
}

func TestEnqueueDeduplicatesAtomically(t *testing.T) {
	q, _ := openTemp(t, Options{})
	recs := process(t, newPipeline(t, q), sessionHooks("sess-d"))
	if _, err := q.Enqueue(ctx, recs, nil); err != nil {
		t.Fatal(err)
	}
	// 다른 Normalizer(키 미로딩)가 같은 키를 다시 만들어도 큐가 거른다.
	n, _ := normalize.New(normalize.Options{DeviceID: testDevice, Sequences: q})
	again := process(t, normalize.NewPipeline(n), sessionHooks("sess-d"))
	res, err := q.Enqueue(ctx, again, nil)
	if err != nil || res.Enqueued != 1 || res.Duplicates != 3 {
		t.Fatalf("second enqueue %+v %v", res, err)
	}
	// 같은 레코드(같은 무작위 event_id)를 다시 넣어도 한 번만 들어간다.
	res, err = q.Enqueue(ctx, again, nil)
	if err != nil || res.Enqueued != 0 || res.Duplicates != 4 || q.Depth() != 5 {
		t.Fatalf("third enqueue %+v %v depth=%d", res, err, q.Depth())
	}

	// 잘못된 레코드가 섞이면 아무것도 커밋하지 않는다.
	bad := process(t, newPipeline(t, q), sessionHooks("sess-e"))
	bad = append(bad, normalize.Record{Event: normalize.Event{EventID: "not-a-uuid"}})
	if _, err := q.Enqueue(ctx, bad, &CheckpointUpdate{Key: "k", Checkpoint: tail.Checkpoint{Offset: 9}}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("got %v", err)
	}
	if q.Depth() != 5 {
		t.Fatalf("depth %d", q.Depth())
	}
	if _, ok, _ := q.Checkpoint(ctx, "k"); ok {
		t.Fatal("checkpoint committed without records")
	}
	seen, _ := q.SeenEventIDs(ctx)
	if len(seen) != 3 {
		t.Fatalf("seen %d", len(seen))
	}
}

func TestBoundedQueueDropsOldest(t *testing.T) {
	q, path := openTemp(t, Options{MaxEvents: 3})
	p := newPipeline(t, q)
	var all []normalize.Record
	for i := 0; i < 2; i++ {
		recs := process(t, p, sessionHooks(fmt.Sprintf("sess-%d", i)))
		all = append(all, recs...)
		if _, err := q.Enqueue(ctx, recs, nil); err != nil {
			t.Fatal(err)
		}
	}
	items, _ := q.Peek(ctx, 10)
	if len(items) != 3 || q.Depth() != 3 {
		t.Fatalf("depth %d", len(items))
	}
	for i, it := range items {
		if it.EventID != all[len(all)-3+i].Event.EventID {
			t.Fatal("did not keep newest")
		}
	}
	q = reopen(t, q, path, Options{MaxEvents: 3})
	st, err := q.Stats(ctx)
	if err != nil || st.DroppedOldest != 5 || st.Depth != 3 {
		t.Fatalf("stats %+v %v", st, err)
	}
}

func TestRejectDeadLettersWithoutContent(t *testing.T) {
	q, _ := openTemp(t, Options{MaxAttempts: 2})
	recs := process(t, newPipeline(t, q), sessionHooks("sess-x"))
	if _, err := q.Enqueue(ctx, recs, nil); err != nil {
		t.Fatal(err)
	}
	id := func(i int) string { return recs[i].Event.EventID }
	res, err := q.Reject(ctx, []Rejection{
		{EventID: id(0), Code: "retention_window_exceeded"},
		{EventID: id(1), Code: sentinel + " free text"},
		{EventID: id(2), Code: "parent_agent_unknown", Retryable: true},
		{EventID: "99999999-9999-4999-8999-999999999999", Code: "internal_error"},
	})
	if err != nil || res.DeadLettered != 2 || res.Retrying != 1 || q.Depth() != 2 {
		t.Fatalf("reject %+v %v depth=%d", res, err, q.Depth())
	}
	items, _ := q.Peek(ctx, 10)
	if items[0].EventID != id(2) || items[0].Attempts != 1 {
		t.Fatalf("retrying item %+v", items[0].Attempts)
	}
	res, _ = q.Reject(ctx, []Rejection{{EventID: id(2), Code: "parent_agent_unknown", Retryable: true}})
	if res.DeadLettered != 1 {
		t.Fatalf("max attempts %+v", res)
	}
	st, _ := q.Stats(ctx)
	want := map[string]int64{"retention_window_exceeded": 1, ReasonUnknownCode: 1, ReasonMaxAttempts: 1}
	if st.DeadLetter != 3 || len(st.DeadLetterByReason) != 3 {
		t.Fatalf("stats %+v", st)
	}
	for k, v := range want {
		if st.DeadLetterByReason[k] != v {
			t.Fatalf("stats %+v", st)
		}
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), sentinel) {
		t.Fatal("server code text stored")
	}
}

func TestCheckpointCommittedWithRecords(t *testing.T) {
	q, path := openTemp(t, Options{})
	key := "/home/" + sentinel + "/.claude/projects/x/transcript.jsonl"
	cp := tail.Checkpoint{File: &tail.FileID{Device: 1, Inode: 2}, Offset: 1234, PrefixDigest: strings.Repeat("a", 64)}
	recs := process(t, newPipeline(t, q), sessionHooks("sess-c"))
	if _, err := q.Enqueue(ctx, recs, &CheckpointUpdate{Key: key, Checkpoint: cp}); err != nil {
		t.Fatal(err)
	}
	q = reopen(t, q, path, Options{})
	got, ok, err := q.Checkpoint(ctx, key)
	if err != nil || !ok || got.Offset != 1234 || got.File == nil || got.File.Inode != 2 {
		t.Fatalf("checkpoint %+v %v %v", got, ok, err)
	}
	if _, ok, _ := q.Checkpoint(ctx, "other"); ok {
		t.Fatal("unexpected checkpoint")
	}
	// 체크포인트만 갱신(레코드 없음)도 된다.
	cp.Offset = 2000
	if _, err := q.Enqueue(ctx, nil, &CheckpointUpdate{Key: key, Checkpoint: cp}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := q.Checkpoint(ctx, key); got.Offset != 2000 {
		t.Fatalf("offset %d", got.Offset)
	}
}

// DB 파일(본 파일·WAL)에 원본 프롬프트·도구 입출력·경로·원본 세션 참조·체크포인트 키가 없다.
func TestQueueStoresNoRawContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.db")
	q, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := newPipeline(t, q)
	var payloads [][]byte
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "SubagentStart", "SubagentStop", "Stop", "SessionEnd"} {
		var b strings.Builder
		fmt.Fprintf(&b, `{"hook_event_name":%q`, ev)
		for _, id := range []string{"session_id", "prompt_id", "turn_id", "tool_use_id", "agent_id"} {
			fmt.Fprintf(&b, `,%q:"%s-%s"`, id, sentinel, id)
		}
		for _, f := range []string{"tool_name", "agent_type", "prompt", "tool_input", "tool_response", "error", "cwd", "transcript_path", "last_assistant_message"} {
			fmt.Fprintf(&b, `,%q:"%s /home/%s/secret.txt"`, f, sentinel, sentinel)
		}
		b.WriteString("}")
		payloads = append(payloads, []byte(b.String()))
	}
	recs := process(t, p, payloads)
	if len(recs) < 5 {
		t.Fatalf("records %d", len(recs))
	}
	cpKey := "/home/" + sentinel + "/transcript.jsonl"
	if _, err := q.Enqueue(ctx, recs, &CheckpointUpdate{Key: cpKey, Checkpoint: tail.Checkpoint{Offset: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reject(ctx, []Rejection{{EventID: recs[0].Event.EventID, Code: sentinel}}); err != nil {
		t.Fatal(err)
	}
	check := func(label string) {
		entries, _ := os.ReadDir(dir)
		if len(entries) == 0 {
			t.Fatal("no db files")
		}
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), sentinel) || strings.Contains(string(data), "/home/") {
				t.Fatalf("%s: raw value in %s", label, e.Name())
			}
		}
	}
	check("before close") // WAL에 남은 상태
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	check("after close")
	// 오류 문자열에도 경로가 남지 않는다.
	blocker := filepath.Join(dir, sentinel)
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(filepath.Join(blocker, "q.db"), Options{})
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("open error %v", err)
	}
}

func TestConcurrentEnqueueAndAck(t *testing.T) {
	q, _ := openTemp(t, Options{})
	var wg sync.WaitGroup
	const writers = 4
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			n, _ := normalize.New(normalize.Options{DeviceID: testDevice, Sequences: q})
			p := normalize.NewPipeline(n)
			for i := 0; i < 10; i++ {
				recs := process(t, p, sessionHooks(fmt.Sprintf("w%d-%d", w, i)))
				if _, err := q.Enqueue(ctx, recs, nil); err != nil {
					t.Error(err)
					return
				}
			}
		}(w)
	}
	acked := 0
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for finished := false; !finished; {
		select {
		case <-done:
			finished = true
		default:
		}
		items, err := q.Peek(ctx, 7)
		if err != nil {
			t.Fatal(err)
		}
		n, err := q.Ack(ctx, eventIDs(items))
		if err != nil {
			t.Fatal(err)
		}
		acked += n
	}
	for q.Depth() > 0 {
		items, _ := q.Peek(ctx, 50)
		n, _ := q.Ack(ctx, eventIDs(items))
		acked += n
	}
	if acked != writers*10*4 {
		t.Fatalf("acked %d", acked)
	}
	var count int
	if err := q.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rows %d %v", count, err)
	}
}

func TestPruneSeen(t *testing.T) {
	now := observed
	q, _ := openTemp(t, Options{Now: func() time.Time { return now }})
	recs := process(t, newPipeline(t, q), sessionHooks("sess-p"))
	if _, err := q.Enqueue(ctx, recs, nil); err != nil {
		t.Fatal(err)
	}
	if n, err := q.PruneSeen(ctx, now); err != nil || n != 0 {
		t.Fatalf("prune %d %v", n, err)
	}
	if n, err := q.PruneSeen(ctx, now.Add(time.Millisecond)); err != nil || n != 3 {
		t.Fatalf("prune %d %v", n, err)
	}
}

func TestClosedQueue(t *testing.T) {
	q, _ := openTemp(t, Options{})
	_ = q.Close()
	if _, err := q.Peek(ctx, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if _, err := q.Next(testDevice, testDevice); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
}

var _ normalize.SequenceStore = (*Queue)(nil)
