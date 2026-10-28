package cli

import (
	"context"
	"encoding/json"
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
	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/transport"
)

var fixtureDir = filepath.Join("..", "..", "..", "..", "tests", "fixtures", "collector", "claude", "dual-source")

// fakeOffice는 장치 등록·heartbeat·배치 수집 API(docs/api/devices-v1.md, event-ingestion-v1.md)의 최소 구현이다.
type fakeOffice struct {
	mu         sync.Mutex
	events     []normalize.Event
	bodies     []string
	heartbeats int
	authFail   int
	revoked    int
}

func (f *fakeOffice) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = append(f.bodies, string(body))
	if r.URL.Path != "/api/v1/devices/register" && r.URL.Path != "/api/v1/system/health" &&
		r.Header.Get("Authorization") != "Bearer "+sentinelToken {
		f.authFail++
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/api/v1/system/health":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	case "/api/v1/devices/register":
		var req map[string]string
		if json.Unmarshal(body, &req) != nil || req["registration_code"] != "ABCD-EFGH" || req["name"] != "E2E PC" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"device_id":"` + testDeviceID + `","device_token":"` + sentinelToken + `"}`))
	case "/api/v1/devices/heartbeat":
		f.heartbeats++
		w.WriteHeader(http.StatusNoContent)
	case "/api/v1/devices/revoke":
		f.revoked++
		w.WriteHeader(http.StatusNoContent)
	case "/api/v1/events/batch":
		var req struct {
			SchemaVersion string            `json:"schema_version"`
			Events        []normalize.Event `json:"events"`
		}
		if json.Unmarshal(body, &req) != nil || req.SchemaVersion != "1.0" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		ids := []string{}
		for _, e := range req.Events {
			f.events = append(f.events, e)
			ids = append(ids, e.EventID)
		}
		resp, _ := json.Marshal(map[string]any{"schema_version": "1.0", "accepted_event_ids": ids, "duplicate_event_ids": []string{}, "rejected_events": []any{}})
		_, _ = w.Write(resp)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeOffice) snapshot() ([]normalize.Event, int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]normalize.Event(nil), f.events...), f.heartbeats, strings.Join(f.bodies, "\n")
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func userLine(session, prompt string) string {
	return `{"type":"user","sessionId":"` + session + `","promptId":"` + prompt + `","uuid":"u-` + prompt[len(prompt)-4:] + `","message":{"role":"user","content":"` + sentinelPrompt + `"}}` + "\n"
}

func appendFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}

func sessionsOf(events []normalize.Event) map[string]int {
	out := map[string]int{}
	for _, e := range events {
		out[e.SessionID]++
	}
	return out
}

// register → run(기존 transcript는 건너뜀) → 새 transcript·Hook 수집·전송 → 일시중지 동안 쌓인 줄 미수집
// → 재개 후 새 줄 수집 → 종료. 전송 본문·로컬 파일에 원문 sentinel과 토큰이 없는지도 확인한다.
func TestEndToEndRegisterRunCollectSendShutdown(t *testing.T) {
	office := &fakeOffice{}
	srv := httptest.NewServer(office)
	defer srv.Close()

	te := newTestEnv(t, "u")
	projects := te.dirs.ClaudeProjects
	const (
		oldSession    = "00000000-0000-4000-8000-000000000099"
		pausedSession = "00000000-0000-4000-8000-000000000077"
		lateSession   = "00000000-0000-4000-8000-000000000088"
		fixtureSess   = "00000000-0000-4000-8000-000000000001"
	)
	// 설치 전부터 있던 transcript: 최초 실행에서 끝으로 건너뛰어야 한다.
	appendFile(t, filepath.Join(projects, "old", oldSession+".jsonl"), userLine(oldSession, "00000000-0000-4000-8000-000000009901"))

	if code := te.run("", "register", "--server", srv.URL, "--code", "ABCD-EFGH", "--name", "E2E PC"); code != ExitOK {
		t.Fatalf("register: %d %s", code, te.stderr)
	}
	if strings.Contains(te.stdout.String()+te.stderr.String(), sentinelToken) {
		t.Fatal("register printed the device token")
	}
	if code := te.run("", "register", "--server", srv.URL, "--code", "ABCD-EFGH", "--name", "E2E PC"); code != ExitError {
		t.Fatalf("second register without --force: %d", code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEnv := te.env
	runEnv.Context = ctx
	runEnv.Stdout, runEnv.Stderr = io.Discard, io.Discard
	runEnv.RunOptions = func(o *runner.Options) {
		o.PollInterval = 30 * time.Millisecond
		o.DiscoverInterval = 30 * time.Millisecond
		o.HeartbeatInterval = 100 * time.Millisecond
		o.StatusInterval = 30 * time.Millisecond
		o.TuneSender = func(c *transport.Config) {
			c.IdleInterval = 30 * time.Millisecond
			c.BackoffBase = 10 * time.Millisecond
			c.BackoffMax = 50 * time.Millisecond
		}
	}
	done := make(chan int, 1)
	go func() { done <- Main([]string{"run"}, runEnv) }()

	status := func() runner.Snapshot {
		s, _ := runner.ReadSnapshot(te.dirs.StatusFile())
		return s
	}
	waitFor(t, "baseline skip of existing transcript", func() bool {
		_, err := os.Stat(te.dirs.BaselineFile())
		return err == nil && status().Counters["baseline_skipped_files"] == 1
	})

	// 설치 뒤 새로 생긴 transcript(합성 fixture)와 Hook 한 건.
	fixture, err := os.ReadFile(filepath.Join(fixtureDir, "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	appendFile(t, filepath.Join(projects, "proj", fixtureSess+".jsonl"), string(fixture))
	hookLine := `{"hook_event_name":"SessionStart","session_id":"` + lateSession + `","source":"startup","prompt":"` + sentinelPrompt + `"}`
	if code := te.run(hookLine, "hook", "claude"); code != ExitOK {
		t.Fatal(code)
	}

	fixtureID := normalize.SessionID(testDeviceID, "claude", fixtureSess)
	lateID := normalize.SessionID(testDeviceID, "claude", lateSession)
	waitFor(t, "transcript and hook events at server", func() bool {
		ev, hb, _ := office.snapshot()
		s := sessionsOf(ev)
		return s[fixtureID] > 0 && s[lateID] > 0 && hb > 0
	})
	waitFor(t, "spool drained", func() bool { return len(spoolFiles(t, te.dirs)) == 0 })
	waitFor(t, "queue drained", func() bool { s := status(); return s.QueueDepth == 0 && s.Counters["acked"] > 0 })

	// 일시중지: 그동안 쓰인 줄은 재개 뒤에도 보내지 않는다.
	if code := te.run("", "pause"); code != ExitOK {
		t.Fatal(code)
	}
	waitFor(t, "paused state", func() bool { return status().State == runner.StatePaused })
	before, _, _ := office.snapshot()
	appendFile(t, filepath.Join(projects, "proj", fixtureSess+".jsonl"), userLine(pausedSession, "00000000-0000-4000-8000-000000007701"))
	if code := te.run(`{"hook_event_name":"SessionStart","session_id":"`+pausedSession+`"}`, "hook", "claude"); code != ExitOK {
		t.Fatal(code)
	}
	if n := len(spoolFiles(t, te.dirs)); n != 0 {
		t.Fatalf("hook spooled %d files while paused", n)
	}
	time.Sleep(300 * time.Millisecond)
	if after, _, _ := office.snapshot(); len(after) != len(before) {
		t.Fatalf("events sent while paused: %d -> %d", len(before), len(after))
	}
	if code := te.run("", "resume"); code != ExitOK {
		t.Fatal(code)
	}
	waitFor(t, "resume backlog skip", func() bool {
		s := status()
		return s.State == runner.StateRunning && s.Counters["resume_backlog_skips"] == 1
	})
	appendFile(t, filepath.Join(projects, "proj", fixtureSess+".jsonl"), userLine(lateSession, "00000000-0000-4000-8000-000000008801"))
	waitFor(t, "post-resume line", func() bool {
		ev, _, _ := office.snapshot()
		for _, e := range ev {
			if e.SessionID == lateID && e.Source == "jsonl" {
				return true
			}
		}
		return false
	})
	// 서버가 받은 뒤 ACK 처리가 끝나기 전에 종료하면 큐에 한 건이 남는다(재전송되므로 정상 동작).
	// 최종 큐 깊이 0을 확인하려면 ACK 반영을 기다린 뒤 종료한다(CI 간헐 실패 원인).
	// 상태 파일의 큐 깊이는 마지막 enqueue 직전 값일 수 있으므로, 받아들인 이벤트가 모두 ACK될 때까지 본다.
	// 한 순간만 보면 직후에 들어온 이벤트(늦게 읽힌 줄 등)가 큐에 남은 채 종료될 수 있다(#213: accepted 8, acked 7).
	// 같은 조건이 세 번 연속(약 0.6초) 유지되고 개수도 변하지 않을 때 비었다고 본다.
	stable, last := 0, int64(-1)
	waitFor(t, "queue drained", func() bool {
		s := status()
		ok := s.QueueDepth == 0 && s.Counters["acked"] > 0 &&
			s.Counters["acked"] == s.Counters["normalize_accepted"]
		if ok && s.Counters["normalize_accepted"] == last {
			stable++
		} else if ok {
			stable, last = 1, s.Counters["normalize_accepted"]
		} else {
			stable, last = 0, -1
		}
		if stable >= 3 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
		return false
	})

	cancel()
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("run exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not shut down")
	}

	events, _, bodies := office.snapshot()
	sessions := sessionsOf(events)
	if sessions[normalize.SessionID(testDeviceID, "claude", oldSession)] != 0 {
		t.Fatal("pre-existing transcript was collected")
	}
	if sessions[normalize.SessionID(testDeviceID, "claude", pausedSession)] != 0 {
		t.Fatal("lines written while paused were collected")
	}
	seen := map[string]bool{}
	for _, e := range events {
		if seen[e.EventID] {
			t.Fatalf("event %s delivered twice", e.EventID)
		}
		seen[e.EventID] = true
		if e.DeviceID != testDeviceID {
			t.Fatalf("device id %s", e.DeviceID)
		}
	}
	for _, bad := range []string{sentinelPrompt, "<synthetic>", te.home, "toolu_synthetic"} {
		if strings.Contains(bodies, bad) {
			t.Fatalf("server received %q", bad)
		}
	}
	final := status()
	if final.State != runner.StateStopped || final.QueueDepth != 0 {
		t.Fatalf("final status %+v", final)
	}
	// 로컬 상태 파일(설정 제외)에도 원문이 없어야 한다.
	_ = filepath.Walk(te.dirs.State, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, bad := range []string{sentinelPrompt, sentinelToken, "<synthetic>"} {
			if strings.Contains(string(b), bad) {
				t.Fatalf("state file %s contains %q", filepath.Base(p), bad)
			}
		}
		return nil
	})

	// 실행 뒤 진단: 서버 도달(health)·큐 비어 있음, 비밀 없음.
	if code := te.run("", "diagnose", "--json"); code != ExitOK {
		t.Fatal(code)
	}
	var rep Report
	if err := json.Unmarshal(te.stdout.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Server != "reachable" || rep.Queue == nil || rep.Queue.Depth != 0 || rep.ProcessRunning {
		t.Fatalf("diagnose %+v", rep)
	}
	if strings.Contains(te.stdout.String(), srv.URL) || strings.Contains(te.stdout.String(), sentinelToken) {
		t.Fatal("diagnose leaked server url or token")
	}
}
