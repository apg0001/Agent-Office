package single

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 도우미 프로세스가 같은 잠금을 잡으려 하면 ErrHeld(종료 코드 3)다. 잠금은 다른 프로세스 사이에서 동작해야 한다.
func TestMain(m *testing.M) {
	if p := os.Getenv("AO_SINGLE_HELPER"); p != "" {
		l, err := Acquire(p)
		switch {
		case errors.Is(err, ErrHeld):
			os.Exit(3)
		case err != nil:
			os.Exit(4)
		}
		l.Release()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, path string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "AO_SINGLE_HELPER="+path)
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

func TestAcquireAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui", "tray.lock")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if code := helper(t, path); code != 3 {
		t.Fatalf("second process got lock (exit %d)", code)
	}
	l.Release()
	l.Release()
	if code := helper(t, path); code != 0 {
		t.Fatalf("after release: exit %d", code)
	}
}
