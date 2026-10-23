package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/normalize"
	"github.com/apg0001/Agent-Office/collector/core/internal/queue"
)

const (
	testDevice = "11111111-2222-4333-8444-555555555555"
	testToken  = "aod_test-token-SENTINELt0k"
)

var ctx = context.Background()

// fakeServer는 서버 수집 계약을 흉내 낸다. event_id별 payload를 저장하고 같은 내용이면 duplicate다.
type fakeServer struct {
	t  *testing.T
	mu sync.Mutex
	// stored는 영속화된 event_id → payload다.
	stored   map[string]string
	requests int
	bodies   [][]byte
	sizes    []int
	// handle이 nil이 아니면 기본 처리 대신 쓴다.
	handle func(w http.ResponseWriter, r *http.Request, events []json.RawMessage) bool
}

func newFakeServer(t *testing.T) (*fakeServer, *httptest.Server) {
	fs := &fakeServer{t: t, stored: map[string]string{}}
	srv := httptest.NewServer(fs)
	t.Cleanup(srv.Close)
	return fs, srv
}

type batchReq struct {
	SchemaVersion string            `json:"schema_version"`
	Events        []json.RawMessage `json:"events"`
}

func (fs *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.requests++
	if r.URL.Path != batchPath || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+testToken ||
		r.Header.Get("Content-Type") != "application/json" || r.ContentLength <= 0 {
		fs.t.Errorf("bad request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	body, _ := io.ReadAll(r.Body)
	fs.bodies = append(fs.bodies, body)
	if len(body) > MaxBodyBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	var req batchReq
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.SchemaVersion != "1.0" || len(req.Events) == 0 || len(req.Events) > MaxBatchEvents {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	fs.sizes = append(fs.sizes, len(req.Events))
	if fs.handle != nil && fs.handle(w, r, req.Events) {
		return
	}
	writeJSON(w, fs.persist(req.Events))
}

func (fs *fakeServer) persist(events []json.RawMessage) map[string]any {
	resp := map[string]any{"schema_version": "1.0", "accepted_event_ids": []string{}, "duplicate_event_ids": []string{}, "rejected_events": []any{}}
	for _, raw := range events {
		var ev normalize.Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			fs.t.Errorf("event decode: %v", err)
			continue
		}
		key := "accepted_event_ids"
		if prev, ok := fs.stored[ev.EventID]; ok {
			if prev != string(raw) {
				fs.t.Errorf("event_id %s resent with different content", ev.EventID)
			}
			key = "duplicate_event_ids"
		}
		fs.stored[ev.EventID] = string(raw)
		resp[key] = append(resp[key].([]string), ev.EventID)
	}
	return resp
}

func (fs *fakeServer) snap() (stored map[string]string, sizes []int, bodies [][]byte, requests int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	stored = make(map[string]string, len(fs.stored))
	for k, v := range fs.stored {
		stored[k] = v
	}
	return stored, append([]int(nil), fs.sizes...), append([][]byte(nil), fs.bodies...), fs.requests
}

func (fs *fakeServer) setHandle(h func(w http.ResponseWriter, r *http.Request, events []json.RawMessage) bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.handle = h
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func eventIDOf(raw json.RawMessage) string {
	var ev struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(raw, &ev)
	return ev.EventID
}

// fakeClock은 대기 시간을 기록하고 바로 돌아온다. limit번째 대기에서 ctx를 취소한다.
type fakeClock struct {
	mu     sync.Mutex
	sleeps []time.Duration
	limit  int
	cancel context.CancelFunc
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration, _ <-chan struct{}) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	n := len(c.sleeps)
	c.mu.Unlock()
	if c.limit > 0 && n >= c.limit && c.cancel != nil {
		c.cancel()
		return ctx.Err()
	}
	return ctx.Err()
}

func openQueue(t *testing.T, opts queue.Options) (*queue.Queue, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queue.db")
	q, err := queue.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q, path
}

var seqCounter int64

// fill은 세션마다 sequence가 다른 유효 이벤트 n개를 넣는다. 일부는 무작위 ID 성격(DeterministicID=false)이다.
func fill(t *testing.T, q *queue.Queue, n int) []string {
	t.Helper()
	var recs []normalize.Record
	var ids []string
	session := normalize.SessionID(testDevice, "claude", fmt.Sprintf("s-%d", seqCounter))
	lead := normalize.LeadAgentID(testDevice, "claude", "s")
	for i := 0; i < n; i++ {
		seqCounter++
		id := fmt.Sprintf("%08x-0000-4000-8000-%012x", seqCounter>>16, seqCounter)
		recs = append(recs, normalize.Record{Event: normalize.Event{
			SchemaVersion: "1.0", EventID: id, DeviceID: testDevice, SessionID: session, AgentID: lead,
			Provider: "claude", Source: "hook", EventType: "tool.started", Activity: "coding",
			Confidence: "observed", OccurredAt: "2026-10-23T01:02:03Z", Sequence: int64(i),
		}, DeterministicID: i%2 == 0})
		ids = append(ids, id)
	}
	if res, err := q.Enqueue(ctx, recs, nil); err != nil || res.Enqueued != n {
		t.Fatalf("enqueue %+v %v", res, err)
	}
	return ids
}

func newSender(t *testing.T, url string, q Store, cfg Config) *Sender {
	t.Helper()
	cfg.BaseURL = url + "/"
	if cfg.Token == nil {
		cfg.Token = func() string { return testToken }
	}
	if cfg.Clock == nil {
		cfg.Clock = &fakeClock{}
	}
	if cfg.Rand == nil {
		cfg.Rand = func() float64 { return 0.5 }
	}
	s, err := New(cfg, q)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSendAcksAcceptedAndDuplicate(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	ids := fill(t, q, 3)
	// 서버가 이미 한 건을 영속화한 상태(이전 전송의 ACK 유실).
	items, _ := q.Peek(ctx, 1)
	fs.mu.Lock()
	fs.stored[ids[0]] = string(items[0].Payload)
	fs.mu.Unlock()
	s := newSender(t, srv.URL, q, Config{})
	res, err := s.SendOnce(ctx)
	if err != nil || res.Sent != 3 || res.Accepted != 2 || res.Duplicate != 1 || q.Depth() != 0 {
		t.Fatalf("res %+v %v depth=%d", res, err, q.Depth())
	}
	if res, err := s.SendOnce(ctx); err != nil || res.Sent != 0 {
		t.Fatalf("empty send %+v %v", res, err)
	}
}

// 서버가 저장한 뒤 응답 전에 연결이 끊기고 Collector가 재시작해도 같은 event_id·내용으로 다시 보내고,
// duplicate ACK로 큐에서 지운다.
func TestCrashBetweenSendAndAckResendsSameIDs(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, path := openQueue(t, queue.Options{})
	ids := fill(t, q, 4)
	fs.setHandle(func(w http.ResponseWriter, _ *http.Request, events []json.RawMessage) bool {
		fs.persist(events) // 커밋 후
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close() // 응답 전에 끊김
		return true
	})
	s := newSender(t, srv.URL, q, Config{})
	_, err := s.SendOnce(ctx)
	var ne *NetworkError
	if !errors.As(err, &ne) || q.Depth() != 4 {
		t.Fatalf("err %v depth %d", err, q.Depth())
	}
	if strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatal("error exposes url")
	}

	// 프로세스 재시작.
	_ = q.Close()
	q2, err := queue.Open(path, queue.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer q2.Close()
	fs.mu.Lock()
	fs.handle = nil
	firstBody := string(fs.bodies[0])
	fs.mu.Unlock()
	res, err := newSender(t, srv.URL, q2, Config{}).SendOnce(ctx)
	if err != nil || res.Duplicate != 4 || res.Accepted != 0 || q2.Depth() != 0 {
		t.Fatalf("resend %+v %v", res, err)
	}
	if _, _, bodies, _ := fs.snap(); string(bodies[1]) != firstBody {
		t.Fatal("resent body differs")
	}
	for _, id := range ids {
		if !strings.Contains(firstBody, id) {
			t.Fatal("missing id")
		}
	}
}

func TestBatchAndBodyLimits(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 1200)
	s := newSender(t, srv.URL, q, Config{})
	for q.Depth() > 0 {
		if _, err := s.SendOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stored, sizes, _, _ := fs.snap()
	if fmt.Sprint(sizes) != "[500 500 200]" || len(stored) != 1200 {
		t.Fatalf("sizes %v stored %d", sizes, len(stored))
	}

	fs2, srv2 := newFakeServer(t)
	q2, _ := openQueue(t, queue.Options{})
	fill(t, q2, 50)
	s2 := newSender(t, srv2.URL, q2, Config{MaxBody: 4096})
	for q2.Depth() > 0 {
		if _, err := s2.SendOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stored2, _, bodies2, _ := fs2.snap()
	for _, b := range bodies2 {
		if len(b) > 4096 {
			t.Fatalf("body %d > limit", len(b))
		}
	}
	if len(bodies2) < 5 || len(stored2) != 50 {
		t.Fatalf("bodies %d stored %d", len(bodies2), len(stored2))
	}
}

func TestServerErrorsBackOffWithJitterAndRetryAfter(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 2)
	statuses := []int{500, 502, 503, 429, 500}
	fs.setHandle(func(w http.ResponseWriter, _ *http.Request, events []json.RawMessage) bool {
		i := fs.requests - 1
		if i >= len(statuses) {
			return false
		}
		if statuses[i] == 429 {
			w.Header().Set("Retry-After", "7")
		}
		w.WriteHeader(statuses[i])
		_, _ = w.Write([]byte(`{"detail":"SENTINELbody"}`))
		return true
	})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	clock := &fakeClock{limit: 7, cancel: cancel}
	s := newSender(t, srv.URL, q, Config{Clock: clock, Rand: func() float64 { return 0.999 }, BackoffBase: time.Second, BackoffMax: 10 * time.Second, IdleInterval: time.Minute})
	if err := s.Run(runCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("run %v", err)
	}
	if stored, _, _, _ := fs.snap(); q.Depth() != 0 || len(stored) != 2 {
		t.Fatalf("depth %d stored %d", q.Depth(), len(stored))
	}
	got := clock.sleeps
	// 1·2·4·(429: Retry-After 7s 하한)·10(상한) 뒤 성공, 이어서 유휴 대기.
	ceil := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second}
	if len(got) < 6 {
		t.Fatalf("sleeps %v", got)
	}
	for i, c := range ceil {
		if got[i] > c || got[i] < c*99/100 {
			t.Fatalf("sleep %d = %v want ~%v (%v)", i, got[i], c, got)
		}
	}
	if got[3] < 7*time.Second {
		t.Fatalf("retry-after not honored: %v", got[3])
	}
	if got[5] != time.Minute {
		t.Fatalf("idle %v", got[5])
	}
}

func TestBackoffFullJitterBounds(t *testing.T) {
	base, maxD := 500*time.Millisecond, 30*time.Second
	for n := 1; n <= 20; n++ {
		lo := Backoff(n, base, maxD, 0, 0)
		hi := Backoff(n, base, maxD, 0.999999, 0)
		if lo != 0 || hi > maxD || hi > base<<(n-1) && base<<(n-1) > 0 && base<<(n-1) < maxD {
			t.Fatalf("n=%d lo=%v hi=%v", n, lo, hi)
		}
	}
	if d := Backoff(1, base, maxD, 0, 9*time.Second); d != 9*time.Second {
		t.Fatalf("retry-after floor %v", d)
	}
	if d := retryAfter("120", time.Now()); d != 2*time.Minute {
		t.Fatalf("seconds %v", d)
	}
	if d := retryAfter("999999999", time.Now()); d != MaxRetryAfter {
		t.Fatalf("cap %v", d)
	}
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if d := retryAfter(now.Add(30*time.Second).Format(http.TimeFormat), now); d != 30*time.Second {
		t.Fatalf("http-date %v", d)
	}
	for _, v := range []string{"", "-1", "soon", now.Add(-time.Hour).Format(http.TimeFormat)} {
		if d := retryAfter(v, now); d != 0 {
			t.Fatalf("%q → %v", v, d)
		}
	}
}

func TestRevokedDeviceStopsWithoutDeleting(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		fs, srv := newFakeServer(t)
		q, _ := openQueue(t, queue.Options{})
		fill(t, q, 3)
		fs.setHandle(func(w http.ResponseWriter, _ *http.Request, _ []json.RawMessage) bool {
			w.WriteHeader(status)
			return true
		})
		clock := &fakeClock{}
		s := newSender(t, srv.URL, q, Config{Clock: clock})
		if err := s.Run(ctx); !errors.Is(err, ErrDeviceRevoked) {
			t.Fatalf("%d: %v", status, err)
		}
		_, _, _, requests := fs.snap()
		if q.Depth() != 3 || len(clock.sleeps) != 0 || requests != 1 {
			t.Fatalf("%d: depth %d sleeps %d requests %d", status, q.Depth(), len(clock.sleeps), requests)
		}
	}
	// 토큰이 없으면 요청하지 않는다.
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 1)
	s := newSender(t, srv.URL, q, Config{Token: func() string { return "" }})
	if _, err := s.SendOnce(ctx); !errors.Is(err, ErrDeviceRevoked) || func() int { _, _, _, n := fs.snap(); return n }() != 0 {
		t.Fatalf("%v", err)
	}
}

func TestMalformedResponsesAckNothing(t *testing.T) {
	cases := map[string]func(ids []string) string{
		"not json": func([]string) string { return `<html>` },
		"wrong version": func(ids []string) string {
			return `{"schema_version":"2.0","accepted_event_ids":[],"duplicate_event_ids":[],"rejected_events":[]}`
		},
		"missing list": func(ids []string) string { return `{"schema_version":"1.0","accepted_event_ids":["` + ids[0] + `"]}` },
		"unknown id": func(ids []string) string {
			return `{"schema_version":"1.0","accepted_event_ids":["` + ids[0] + `","99999999-9999-4999-8999-999999999999"],"duplicate_event_ids":[],"rejected_events":[]}`
		},
		"id twice": func(ids []string) string {
			return `{"schema_version":"1.0","accepted_event_ids":["` + ids[0] + `"],"duplicate_event_ids":["` + ids[0] + `"],"rejected_events":[]}`
		},
		"extra field": func(ids []string) string {
			return `{"schema_version":"1.0","accepted_event_ids":["` + ids[0] + `"],"duplicate_event_ids":[],"rejected_events":[],"message":"x"}`
		},
		"no retryable": func(ids []string) string {
			return `{"schema_version":"1.0","accepted_event_ids":[],"duplicate_event_ids":[],"rejected_events":[{"event_id":"` + ids[0] + `","code":"internal_error"}]}`
		},
	}
	for name, body := range cases {
		fs, srv := newFakeServer(t)
		q, _ := openQueue(t, queue.Options{})
		ids := fill(t, q, 2)
		fs.setHandle(func(w http.ResponseWriter, _ *http.Request, _ []json.RawMessage) bool {
			_, _ = w.Write([]byte(body(ids)))
			return true
		})
		_, err := newSender(t, srv.URL, q, Config{}).SendOnce(ctx)
		if !errors.Is(err, ErrBadResponse) || q.Depth() != 2 {
			t.Fatalf("%s: %v depth %d", name, err, q.Depth())
		}
	}
}

func TestRejectedEventsFollowRetryable(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{MaxAttempts: 3})
	ids := fill(t, q, 3)
	fs.setHandle(func(w http.ResponseWriter, _ *http.Request, events []json.RawMessage) bool {
		rej := []map[string]any{}
		acc := []string{}
		for _, e := range events {
			switch id := eventIDOf(e); id {
			case ids[0]:
				rej = append(rej, map[string]any{"event_id": id, "code": "retention_window_exceeded", "retryable": false})
			case ids[1]:
				// 알 수 없는 코드도 retryable만 따른다.
				rej = append(rej, map[string]any{"event_id": id, "code": "future_code", "retryable": true})
			default:
				acc = append(acc, id)
			}
		}
		writeJSON(w, map[string]any{"schema_version": "1.0", "accepted_event_ids": acc, "duplicate_event_ids": []string{}, "rejected_events": rej})
		return true
	})
	s := newSender(t, srv.URL, q, Config{})
	res, err := s.SendOnce(ctx)
	if err != nil || res.Accepted != 1 || res.Rejected != 2 || res.DeadLettered != 1 || q.Depth() != 1 {
		t.Fatalf("%+v %v depth %d", res, err, q.Depth())
	}
	for i := 0; i < 2; i++ {
		if _, err := s.SendOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := q.Stats(ctx)
	if q.Depth() != 0 || st.DeadLetterByReason["retention_window_exceeded"] != 1 || st.DeadLetterByReason[queue.ReasonMaxAttempts] != 1 {
		t.Fatalf("stats %+v", st)
	}
}

// 요청 전체 검증 실패(422)는 배치를 반씩 줄여 원인 이벤트만 dead-letter한다.
func TestInvalidRequestBisectsToOffendingEvent(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	ids := fill(t, q, 8)
	bad := ids[5]
	fs.setHandle(func(w http.ResponseWriter, _ *http.Request, events []json.RawMessage) bool {
		for _, e := range events {
			if eventIDOf(e) == bad {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"detail":[{"loc":["body","events",0],"type":"x"}]}`))
				return true
			}
		}
		return false
	})
	s := newSender(t, srv.URL, q, Config{})
	for i := 0; i < 40 && q.Depth() > 0; i++ {
		_, err := s.SendOnce(ctx)
		var he *HTTPError
		if err != nil && !(errors.As(err, &he) && he.Status == 422) {
			t.Fatal(err)
		}
	}
	st, _ := q.Stats(ctx)
	stored, _, _, _ := fs.snap()
	if q.Depth() != 0 || len(stored) != 7 || st.DeadLetterByReason[queue.ReasonInvalidRequest] != 1 {
		t.Fatalf("depth %d stored %d stats %+v", q.Depth(), len(stored), st)
	}
	if _, ok := stored[bad]; ok {
		t.Fatal("bad event stored")
	}
}

func TestRequestTimeoutKeepsQueue(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 2)
	s := newSender(t, srv.URL, q, Config{RequestTimeout: 50 * time.Millisecond})
	_, err := s.SendOnce(ctx)
	var ne *NetworkError
	if !errors.As(err, &ne) || !ne.timeout || q.Depth() != 2 {
		t.Fatalf("%v depth %d", err, q.Depth())
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	target := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			target++
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 1)
	_, err := newSender(t, srv.URL, q, Config{}).SendOnce(ctx)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusTemporaryRedirect || target != 0 || q.Depth() != 1 {
		t.Fatalf("%v target %d", err, target)
	}
}

// 계약 fixture(packages/event-schema/fixtures)의 응답을 그대로 받아 처리한다.
func TestContractFixtures(t *testing.T) {
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "event-schema", "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	valid := read("valid/batch-response.json")
	var fixture struct {
		Accepted  []string `json:"accepted_event_ids"`
		Duplicate []string `json:"duplicate_event_ids"`
		Rejected  []struct {
			EventID string `json:"event_id"`
		} `json:"rejected_events"`
	}
	if err := json.Unmarshal(valid, &fixture); err != nil {
		t.Fatal(err)
	}
	ids := append(append(append([]string{}, fixture.Accepted...), fixture.Duplicate...), fixture.Rejected[0].EventID)

	enqueue := func(q *queue.Queue) {
		var recs []normalize.Record
		for i, id := range ids {
			recs = append(recs, normalize.Record{Event: normalize.Event{
				SchemaVersion: "1.0", EventID: id, DeviceID: testDevice,
				SessionID: "22222222-2222-4222-8222-222222222222", AgentID: "33333333-3333-4333-8333-333333333333",
				Provider: "claude", Source: "jsonl", EventType: "turn.started", Activity: "thinking",
				Confidence: "inferred", OccurredAt: "2026-10-23T01:02:03Z", Sequence: int64(i),
			}})
		}
		if _, err := q.Enqueue(ctx, recs, nil); err != nil {
			t.Fatal(err)
		}
	}
	serve := func(body []byte) (*queue.Queue, error, *[]byte) {
		var req []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req, _ = io.ReadAll(r.Body)
			_, _ = w.Write(body)
		}))
		t.Cleanup(srv.Close)
		q, _ := openQueue(t, queue.Options{})
		enqueue(q)
		_, err := newSender(t, srv.URL, q, Config{}).SendOnce(ctx)
		return q, err, &req
	}

	q, err, req := serve(valid)
	items, _ := q.Peek(ctx, 10)
	if err != nil || len(items) != 1 || items[0].EventID != fixture.Rejected[0].EventID || items[0].Attempts != 1 {
		t.Fatalf("valid fixture: %v %d", err, len(items))
	}
	// 요청 본문은 배치 요청 스키마의 최상위 형식(schema_version, events)만 가진다.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(*req, &top); err != nil || len(top) != 2 || string(top["schema_version"]) != `"1.0"` {
		t.Fatalf("request shape %v", err)
	}
	var reqFixture map[string]json.RawMessage
	_ = json.Unmarshal(read("valid/batch-request.json"), &reqFixture)
	for k := range reqFixture {
		if _, ok := top[k]; !ok {
			t.Fatalf("request missing %s", k)
		}
	}

	q, err, _ = serve(read("invalid/batch-response-freeform-message.json"))
	if !errors.Is(err, ErrBadResponse) || q.Depth() != len(ids) {
		t.Fatalf("invalid fixture: %v depth %d", err, q.Depth())
	}
}

// 전송기 오류·Result에는 본문·토큰·URL이 없다.
func TestErrorsCarryNoBodyOrToken(t *testing.T) {
	fs, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 1)
	fs.setHandle(func(w http.ResponseWriter, _ *http.Request, _ []json.RawMessage) bool {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("SENTINELbody " + testToken))
		return true
	})
	res, err := newSender(t, srv.URL, q, Config{}).SendOnce(ctx)
	out := fmt.Sprintf("%v %+v", err, res)
	if err == nil || strings.Contains(out, "SENTINEL") || strings.Contains(out, "aod_") || strings.Contains(out, "127.0.0.1") {
		t.Fatalf("leak: %s", out)
	}
}

func TestHeartbeat(t *testing.T) {
	var got map[string]any
	status := http.StatusNoContent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != heartbeatPath || r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	q, _ := openQueue(t, queue.Options{})
	fill(t, q, 3)
	s := newSender(t, srv.URL, q, Config{CollectorVersion: "0.1.0"})
	if err := s.Heartbeat(ctx); err != nil {
		t.Fatal(err)
	}
	if got["queue_depth"] != float64(3) || got["collector_version"] != "0.1.0" || len(got) != 2 {
		t.Fatalf("payload %v", got)
	}
	status = http.StatusUnauthorized
	if err := s.Heartbeat(ctx); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("%v", err)
	}
}

func TestNotifyWakesIdleRun(t *testing.T) {
	_, srv := newFakeServer(t)
	q, _ := openQueue(t, queue.Options{})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := newSender(t, srv.URL, q, Config{Clock: realClock{}, IdleInterval: time.Hour})
	done := make(chan error, 1)
	go func() { done <- s.Run(runCtx) }()
	fill(t, q, 2)
	s.Notify()
	deadline := time.After(10 * time.Second)
	for q.Depth() > 0 {
		select {
		case <-deadline:
			t.Fatal("not woken")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}
