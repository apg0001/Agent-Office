package spool

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

func cand() adapter.Candidate {
	return adapter.Candidate{
		Provider: adapter.ProviderClaude, Source: adapter.SourceHook, EventType: adapter.EventToolStarted,
		SessionRef: "00000000-0000-4000-8000-000000000001", ToolUseRef: "toolu_1",
		Activity: adapter.ActivityReading, Confidence: adapter.ConfidenceObserved, Support: adapter.SupportVerified,
	}
}

func TestWriteListRoundTripOrderAndLimit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	base := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for i := 2; i >= 0; i-- {
		if err := Write(dir, FromCandidate(cand(), base.Add(time.Duration(i)*time.Second)), 3); err != nil {
			t.Fatal(err)
		}
	}
	if err := Write(dir, FromCandidate(cand(), base), 3); err != ErrFull {
		t.Fatalf("expected full, got %v", err)
	}
	got, err := List(dir, 10, base)
	if err != nil || len(got) != 3 {
		t.Fatalf("list %d %v", len(got), err)
	}
	for i, e := range got {
		if e.Err != nil || !e.Record.ObservedAt.Equal(base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("entry %d: %+v", i, e)
		}
		c, err := e.Record.Candidate()
		if err != nil || c != cand() {
			t.Fatalf("candidate %d: %+v %v", i, c, err)
		}
	}
	Remove(dir, got[0].Name)
	if got, _ := List(dir, 10, base); len(got) != 2 {
		t.Fatalf("after remove %d", len(got))
	}
}

func TestInvalidFilesAndStaleTemps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "0001-bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, tmpPrefix+"x.json")
	if err := os.WriteFile(tmp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(tmp, old, old)
	got, err := List(dir, 10, time.Now())
	if err != nil || len(got) != 1 || got[0].Err == nil {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("stale temp not removed")
	}
	r := FromCandidate(cand(), time.Now())
	r.SessionRef = "bad ref with spaces"
	if _, err := r.Candidate(); err != ErrInvalid {
		t.Fatalf("bad ref accepted: %v", err)
	}
}
