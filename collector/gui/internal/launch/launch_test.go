package launch

import (
	"os"
	"slices"
	"testing"
	"time"
)

func TestBrowserCommand(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "open", "windows": "rundll32.exe", "linux": "xdg-open"} {
		argv, err := BrowserCommand(goos, "https://office.example/")
		if err != nil || argv[0] != want || argv[len(argv)-1] != "https://office.example/" {
			t.Errorf("%s: %v %v", goos, argv, err)
		}
	}
	for _, bad := range []string{"", "file:///etc/passwd", "javascript:alert(1)", "https://", "-o https://x", "https://u:p@office.example/"} {
		if _, err := BrowserCommand("linux", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if argv, _ := BrowserCommand("windows", "https://office.example/a?b=1&c=2"); !slices.Contains(argv, "https://office.example/a?b=1&c=2") {
		t.Errorf("query: %v", argv)
	}
}

// TestMain 도우미: 자기 자신을 하위 프로세스로 띄워 Wait·Detached를 확인한다.
func TestMain(m *testing.M) {
	switch os.Getenv("AO_LAUNCH_HELPER") {
	case "exit":
		os.Exit(0)
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestWaitAndDetached(t *testing.T) {
	t.Setenv("AO_LAUNCH_HELPER", "exit")
	if err := Wait(30*time.Second, os.Args[0], "-test.run=^$"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := Detached(os.Args[0], "-test.run=^$"); err != nil {
		t.Fatalf("detached: %v", err)
	}
	t.Setenv("AO_LAUNCH_HELPER", "hang")
	start := time.Now()
	if err := Wait(500*time.Millisecond, os.Args[0], "-test.run=^$"); err == nil || time.Since(start) > 20*time.Second {
		t.Fatalf("timeout not enforced: %v", err)
	}
	if err := Detached("/nonexistent/agent-office-collector-gui"); err == nil {
		t.Fatal("missing exe")
	}
}
