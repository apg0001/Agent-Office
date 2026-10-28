package guictl

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/single"
)

// 도우미 프로세스: 두 번째 GUI 역할. 잠금을 못 잡으면 요청을 남기고 3으로 끝난다.
func TestMain(m *testing.M) {
	if dir := os.Getenv("AO_GUICTL_DIR"); dir != "" {
		p, err := Acquire(dir)
		if errors.Is(err, single.ErrHeld) {
			if Send(dir, Verb(os.Getenv("AO_GUICTL_VERB"))) != nil {
				os.Exit(4)
			}
			os.Exit(3)
		}
		if err != nil {
			os.Exit(5)
		}
		p.Release()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func second(t *testing.T, dir string, v Verb) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "AO_GUICTL_DIR="+dir, "AO_GUICTL_VERB="+string(v))
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}

func TestPrimaryReceivesShowAndQuit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "CollectorGUI")
	// 이전 실행이 남긴 quit은 새 첫 GUI를 끝내지 않는다.
	if err := Send(dir, Quit); err != nil {
		t.Fatal(err)
	}
	p, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	var (
		mu  sync.Mutex
		got []Verb
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Serve(ctx, func(v Verb) { mu.Lock(); got = append(got, v); mu.Unlock() })
		close(done)
	}()
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(got) }
	time.Sleep(2 * PollInterval)
	if count() != 0 {
		t.Fatalf("stale request delivered: %v", got)
	}
	for i, v := range []Verb{Show, Quit} {
		if code := second(t, dir, v); code != 3 {
			t.Fatalf("second instance exit %d", code)
		}
		deadline := time.Now().Add(5 * time.Second)
		for count() != i+1 {
			if time.Now().After(deadline) {
				t.Fatalf("request %s not delivered", v)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if got[0] != Show || got[1] != Quit {
		t.Fatalf("got %v", got)
	}
}

func TestNoPrimaryAndInvalid(t *testing.T) {
	dir := t.TempDir()
	// 첫 GUI가 없으면 두 번째 실행이 잠금을 잡고 그냥 끝난다(요청 파일 없음).
	if code := second(t, dir, Quit); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, requestName)); !os.IsNotExist(err) {
		t.Fatal("request file left without a primary")
	}
	if err := Send(dir, "rm -rf"); !errors.Is(err, ErrInvalidVerb) {
		t.Fatal("invalid verb accepted")
	}
}

// Pending은 요청을 지우지 않고 보여 준다. Serve는 그 뒤에도 같은 요청을 받는다(#177).
func TestPendingPeeksWithoutConsuming(t *testing.T) {
	dir := t.TempDir()
	p, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	if v := p.Pending(); v != "" {
		t.Fatalf("pending before send: %q", v)
	}
	if err := Send(dir, Quit); err != nil {
		t.Fatal(err)
	}
	if v := p.Pending(); v != Quit {
		t.Fatalf("pending = %q, want quit", v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := make(chan Verb, 1)
	go p.Serve(ctx, func(v Verb) { got <- v; cancel() })
	select {
	case v := <-got:
		if v != Quit {
			t.Fatalf("served %q", v)
		}
	case <-ctx.Done():
		t.Fatal("Serve did not receive the request Pending peeked")
	}
	if v := p.Pending(); v != "" {
		t.Fatalf("pending after serve: %q", v)
	}
	var nilP *Primary
	if v := nilP.Pending(); v != "" {
		t.Fatalf("nil primary: %q", v)
	}
}
