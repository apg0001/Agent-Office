package cli

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
)

// GUI(#40)는 등록 코드를 인자 대신 표준 입력으로 넘긴다(프로세스 목록 노출 방지).
func TestRegisterCodeStdin(t *testing.T) {
	srv := httptest.NewServer(&fakeOffice{})
	defer srv.Close()
	te := newTestEnv(t, "u")

	if code := te.run("ABCD-EFGH\n", "register", "--server", srv.URL, "--code-stdin", "--name", "E2E PC"); code != ExitOK {
		t.Fatalf("register --code-stdin: %d %s", code, te.stderr)
	}
	if strings.Contains(te.stdout.String()+te.stderr.String(), sentinelToken) || strings.Contains(te.stdout.String(), "ABCD-EFGH") {
		t.Fatal("register printed the token or code")
	}
	if _, err := config.Load(te.dirs.ConfigFile()); err != nil {
		t.Fatalf("config not saved: %v", err)
	}
}

func TestRegisterCodeStdinRejects(t *testing.T) {
	te := newTestEnv(t, "u")
	cases := map[string]struct {
		stdin string
		args  []string
	}{
		"both flags": {"ABCD-EFGH\n", []string{"--code", "ABCD-EFGH", "--code-stdin"}},
		"empty":      {"", []string{"--code-stdin"}},
		"blank line": {"\nABCD-EFGH\n", []string{"--code-stdin"}},
		"too long":   {strings.Repeat("A", 200), []string{"--code-stdin"}},
	}
	for name, c := range cases {
		args := append([]string{"register", "--server", "https://office.example", "--name", "PC"}, c.args...)
		if code := te.run(c.stdin, args...); code != ExitUsage {
			t.Errorf("%s: exit %d, want %d", name, code, ExitUsage)
		}
	}
}

func TestReadCodeLineStopsAtNewline(t *testing.T) {
	got, err := readCodeLine(strings.NewReader("abcd-efgh\r\nrest"))
	if err != nil || got != "abcd-efgh" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = readCodeLine(strings.NewReader("abcd-efgh"))
	if err != nil || got != "abcd-efgh" {
		t.Fatalf("no newline: got %q %v", got, err)
	}
}
