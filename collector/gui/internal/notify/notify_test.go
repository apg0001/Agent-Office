package notify

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	argv, _, err := Command("darwin", `제목"; do shell script "x`, "본문", nil)
	if err != nil || argv[0] != "/usr/bin/osascript" || argv[len(argv)-2] != `제목"; do shell script "x` {
		t.Fatalf("darwin %q %v", argv, err)
	}
	// 입력은 AppleScript 문자열에 끼워 넣지 않고 argv로만 넘긴다.
	for _, a := range argv[:len(argv)-2] {
		if strings.Contains(a, "제목") {
			t.Fatal("title embedded in script")
		}
	}
	argv, env, err := Command("windows", "t", "b\nnext", nil)
	if err != nil || argv[0] != "powershell.exe" || !slices.Contains(env, "AO_NOTIFY_BODY=b next") || strings.Contains(argv[len(argv)-1], "b next") {
		t.Fatalf("windows %q %q %v", argv, env, err)
	}
	found := func(string) (string, error) { return "/usr/bin/notify-send", nil }
	argv, _, err = Command("linux", "-t", "b", found)
	if err != nil || !slices.Equal(argv[len(argv)-3:], []string{"--", "-t", "b"}) {
		t.Fatalf("linux %q %v", argv, err)
	}
	missing := func(string) (string, error) { return "", errors.New("no") }
	if _, _, err := Command("linux", "t", "b", missing); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing %v", err)
	}
	if got := clean(strings.Repeat("가", 100), 80); len([]rune(got)) != 80 {
		t.Fatal("not truncated")
	}
}
