package secret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestSystemSelection(t *testing.T) {
	bus := map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus"}
	cases := []struct {
		name string
		env  map[string]string
		goos string
		want bool
	}{
		{"forced file", map[string]string{EnvStore: "file", "DBUS_SESSION_BUS_ADDRESS": "unix:path=/x"}, "linux", false},
		{"forced file upper", map[string]string{EnvStore: " FILE "}, "darwin", false},
		{"unknown value is file", map[string]string{EnvStore: "keychain-please"}, "windows", false},
		{"auto darwin", map[string]string{EnvStore: "auto"}, "darwin", true},
		{"default windows", nil, "windows", true},
		{"linux with bus", bus, "linux", true},
		{"linux autolaunch only", map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:"}, "freebsd", false},
		{"other os", bus, "plan9", false},
	}
	for _, c := range cases {
		got := System(envOf(c.env), c.goos) != nil
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestSessionBusLikely(t *testing.T) {
	none := func(string) bool { return false }
	if sessionBusLikely(envOf(nil), 1000, none) {
		t.Error("no address, no socket")
	}
	if sessionBusLikely(envOf(map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:"}), 1000, none) {
		t.Error("autolaunch must not count")
	}
	if !sessionBusLikely(envOf(nil), 1000, func(p string) bool { return p == "/run/user/1000/bus" }) {
		t.Error("socket")
	}
	if sessionBusLikely(envOf(nil), -1, func(string) bool { return true }) {
		t.Error("windows uid -1")
	}
}

func TestMemory(t *testing.T) {
	m := NewMemory()
	if _, err := m.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := m.Set("a", "v"); err != nil {
		t.Fatal(err)
	}
	if v, err := m.Get("a"); err != nil || v != "v" {
		t.Fatal(v, err)
	}
	if err := m.Delete("a"); err != nil || m.Has("a") {
		t.Fatal(err)
	}
	if err := m.Delete("a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	m.Unavailable = true
	if err := m.Set("a", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestKeyringTimeoutAndErrorMapping(t *testing.T) {
	k := &keyringStore{timeout: 20 * time.Millisecond}
	block := make(chan struct{})
	defer close(block)
	if _, err := k.call(func() (string, error) { <-block; return "late", nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("timeout: %v", err)
	}
	// OS 오류 문자열은 버리고 ErrUnavailable만 돌려준다.
	if _, err := k.call(func() (string, error) { return "", errors.New("dbus: /home/user/secret path") }); err != ErrUnavailable {
		t.Fatalf("mapped: %v", err)
	}
}

// EnvRealKeyring이 "1"이면 실제 OS 보안 저장소로 저장·읽기·삭제를 확인한다(문서의 검증 절차, CI 미실행).
const EnvRealKeyring = "AGENT_OFFICE_TEST_REAL_KEYRING"

func TestRealKeyringRoundTrip(t *testing.T) {
	if os.Getenv(EnvRealKeyring) != "1" {
		t.Skip(EnvRealKeyring + " 미설정: 실제 보안 저장소 검증은 수동·컨테이너(gnome-keyring) 절차에서만 실행한다")
	}
	s := System(os.Getenv, runtime.GOOS)
	if s == nil {
		t.Fatal("system store unavailable")
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	account := "test-" + hex.EncodeToString(b)
	const value = "aod_SENTINEL_REAL_KEYRING_DO_NOT_PRINT"
	if err := s.Set(account, value); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(account) })
	if v, err := s.Get(account); err != nil || v != value {
		t.Fatalf("get: %v", err)
	}
	if err := s.Delete(account); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
