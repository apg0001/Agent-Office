package instance

import (
	"path/filepath"
	"testing"
)

func TestAcquireIsExclusiveAndReleasable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", "collector.lock")
	if Held(p) {
		t.Fatal("held before creation")
	}
	l, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p); err != ErrLocked {
		t.Fatalf("second acquire: %v", err)
	}
	if !Held(p) {
		t.Fatal("Held=false while locked")
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if Held(p) {
		t.Fatal("Held=true after release")
	}
	l2, err := Acquire(p)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	_ = l2.Release()
}
