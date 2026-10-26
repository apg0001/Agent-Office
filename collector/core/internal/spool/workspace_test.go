package spool

import (
	"testing"
	"time"
)

// 작업 폴더 이름(#181)은 정리된 값만 spool에 쓰고, 읽을 때 다시 검증해 경로 같은 값은 버린다.
func TestWorkspaceNameRoundTripAndRevalidation(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	c := cand()
	c.WorkspaceName = "Agent-Office"
	r := FromCandidate(c, now)
	if r.WorkspaceName != "Agent-Office" {
		t.Fatalf("record %q", r.WorkspaceName)
	}
	back, err := r.Candidate()
	if err != nil || back.WorkspaceName != "Agent-Office" {
		t.Fatalf("candidate %q %v", back.WorkspaceName, err)
	}
	for _, bad := range []string{"/home/u/Agent-Office", `C:\u\x`, "a\x00b", "..", " x"} {
		c.WorkspaceName = bad
		if got := FromCandidate(c, now).WorkspaceName; got != "" {
			t.Fatalf("write kept %q", got)
		}
		r.WorkspaceName = bad
		back, err := r.Candidate()
		if err != nil || back.WorkspaceName != "" {
			t.Fatalf("read kept %q (%v)", back.WorkspaceName, err)
		}
	}
	if FromCandidate(cand(), now).WorkspaceName != "" {
		t.Fatal("empty name must stay empty")
	}
}
