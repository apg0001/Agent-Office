package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const sentinelToken = "aod_SENTINEL_TOKEN_DO_NOT_PRINT"

func validConfig() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		ServerURL:     "https://office.example",
		DeviceID:      "11111111-2222-4333-8444-555555555555",
		DeviceToken:   sentinelToken,
		DeviceName:    "PC",
		RegisteredAt:  time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC),
	}
}

func TestSaveLoadRoundTripAndPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg", "config.json")
	if err := Save(p, validConfig()); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Same(validConfig()) || got.DeviceName != "PC" {
		t.Fatalf("round trip: %+v", got)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("config perm %v", fi.Mode().Perm())
		}
	}
}

func TestSecretIsRedactedInFormatting(t *testing.T) {
	c := validConfig()
	for _, s := range []string{fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s", c, c, c, c.DeviceToken)} {
		if strings.Contains(s, sentinelToken) {
			t.Fatalf("token leaked: %s", s)
		}
	}
}

func TestLoadMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "none.json")); err != ErrNotRegistered {
		t.Fatalf("missing: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	for _, body := range []string{"{", `{"schema_version":99}`, `{"schema_version":1,"server_url":"https://x","device_id":"nope","device_token":"t"}`} {
		if err := os.WriteFile(bad, []byte(body+sentinelToken), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(bad)
		if err == nil || strings.Contains(err.Error(), sentinelToken) {
			t.Fatalf("invalid accepted or leaked: %v", err)
		}
	}
}

func TestNormalizeServerURL(t *testing.T) {
	ok := map[string]string{
		"https://office.example/":     "https://office.example",
		"https://office.example:8443": "https://office.example:8443",
		"http://127.0.0.1:8000":       "http://127.0.0.1:8000",
		"http://localhost:18000/":     "http://localhost:18000",
	}
	for in, want := range ok {
		got, err := NormalizeServerURL(in)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", in, got, err)
		}
	}
	if _, err := NormalizeServerURL("http://office.example"); err != ErrInsecureURL {
		t.Fatalf("plain http: %v", err)
	}
	for _, in := range []string{"", "office.example", "ftp://x", "https://u:p@x", "https://x/path", "https://x?q=1"} {
		if _, err := NormalizeServerURL(in); err == nil {
			t.Fatalf("%q accepted", in)
		}
	}
}

func TestPauseState(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pause.json")
	st, err := LoadPause(p)
	if err != nil || st.Paused {
		t.Fatalf("default: %+v %v", st, err)
	}
	now := time.Now()
	if err := SavePause(p, true, now); err != nil {
		t.Fatal(err)
	}
	if st, _ := LoadPause(p); !st.Paused {
		t.Fatal("pause not persisted")
	}
	if err := SavePause(p, false, now); err != nil {
		t.Fatal(err)
	}
	if st, _ := LoadPause(p); st.Paused {
		t.Fatal("resume not persisted")
	}
	if err := os.WriteFile(p, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err := LoadPause(p); err == nil || !st.Paused {
		t.Fatalf("corrupt pause must fail safe to paused: %+v %v", st, err)
	}
}
