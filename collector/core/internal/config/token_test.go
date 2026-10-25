package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/secret"
)

func fileHasToken(t *testing.T, p string) bool {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(b), sentinelToken)
}

func TestValidateTokenStore(t *testing.T) {
	c := validConfig()
	for _, tc := range []struct {
		name  string
		store string
		token Secret
		ok    bool
	}{
		{"legacy file", "", sentinelToken, true},
		{"file", TokenStoreFile, sentinelToken, true},
		{"file without token", TokenStoreFile, "", false},
		{"keyring", TokenStoreKeyring, "", true},
		{"keyring with token in file", TokenStoreKeyring, sentinelToken, false},
		{"unknown store", "dpapi", "", false},
	} {
		c.TokenStore, c.DeviceToken = tc.store, tc.token
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func TestSaveWithTokenKeyring(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	st := secret.NewMemory()
	c := validConfig()
	c.DeviceToken = ""
	saved, err := SaveWithToken(p, c, sentinelToken, st)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.InKeyring() || saved.DeviceToken != "" {
		t.Fatalf("saved: %+v", saved)
	}
	if fileHasToken(t, p) {
		t.Fatal("token written to config.json despite keyring")
	}
	loaded, err := Load(p)
	if err != nil || !loaded.Same(saved) {
		t.Fatalf("load: %v", err)
	}
	if tok, err := loaded.Token(st); err != nil || tok != sentinelToken {
		t.Fatalf("token: %v", err)
	}
	if _, err := loaded.Token(nil); !errors.Is(err, ErrTokenUnavailable) {
		t.Fatalf("nil store: %v", err)
	}
	st.FailGet = true
	if _, err := loaded.Token(st); !errors.Is(err, ErrTokenUnavailable) {
		t.Fatalf("locked store: %v", err)
	}
	st.FailGet = false
	if err := DeleteToken(loaded, st); err != nil || st.Has(loaded.DeviceID) {
		t.Fatalf("delete: %v", err)
	}
	if err := DeleteToken(loaded, st); err != nil {
		t.Fatalf("delete again must be ok: %v", err)
	}
}

func TestSaveWithTokenFallsBackToFile(t *testing.T) {
	for name, st := range map[string]secret.Store{
		"nil":         nil,
		"unavailable": &secret.Memory{Unavailable: true},
		"locked get":  func() secret.Store { m := secret.NewMemory(); m.FailGet = true; return m }(),
		"corrupt":     func() secret.Store { m := secret.NewMemory(); m.Corrupt = true; return m }(),
	} {
		p := filepath.Join(t.TempDir(), "config.json")
		saved, err := SaveWithToken(p, validConfig(), sentinelToken, st)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if saved.InKeyring() || saved.TokenStore != TokenStoreFile || !fileHasToken(t, p) {
			t.Fatalf("%s: expected file fallback: %+v", name, saved)
		}
		if m, ok := st.(*secret.Memory); ok && m.Len() != 0 {
			t.Fatalf("%s: failed verification left a keyring item", name)
		}
		if tok, err := saved.Token(st); err != nil || tok != sentinelToken {
			t.Fatalf("%s: token", name)
		}
	}
}

func TestSaveWithTokenRollsBackKeyringWhenFileWriteFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.Mkdir(p, 0o700); err != nil { // 디렉터리라 쓸 수 없다
		t.Fatal(err)
	}
	st := secret.NewMemory()
	if _, err := SaveWithToken(p, validConfig(), sentinelToken, st); err == nil {
		t.Fatal("expected error")
	}
	if st.Len() != 0 {
		t.Fatal("keyring item left behind")
	}
}

func TestMigrateToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	legacy := validConfig() // token_store 없음(#41 이전 형식)
	if err := Save(p, legacy); err != nil {
		t.Fatal(err)
	}
	loaded, _ := Load(p)

	// 저장소 없음·실패: 그대로 파일 보관.
	if c, ok := MigrateToken(p, loaded, nil); ok || c.InKeyring() {
		t.Fatal("nil store migrated")
	}
	bad := secret.NewMemory()
	bad.Corrupt = true
	if c, ok := MigrateToken(p, loaded, bad); ok || c.InKeyring() || !fileHasToken(t, p) || bad.Len() != 0 {
		t.Fatal("corrupt store migrated or left item")
	}

	st := secret.NewMemory()
	c, ok := MigrateToken(p, loaded, st)
	if !ok || !c.InKeyring() {
		t.Fatalf("migrate: %+v", c)
	}
	if fileHasToken(t, p) {
		t.Fatal("token still in config.json after migration")
	}
	again, _ := Load(p)
	if !again.Same(c) || again.DeviceName != legacy.DeviceName || !again.RegisteredAt.Equal(legacy.RegisteredAt) {
		t.Fatalf("other fields changed: %+v", again)
	}
	if tok, err := again.Token(st); err != nil || tok != sentinelToken {
		t.Fatal("token not readable after migration")
	}
	// 두 번째 실행은 아무것도 하지 않는다.
	calls := st.Calls
	if _, ok := MigrateToken(p, again, st); ok || st.Calls != calls {
		t.Fatal("second migration touched the store")
	}
}

func TestSameDetectsStoreChange(t *testing.T) {
	a := validConfig()
	b := a
	b.TokenStore = TokenStoreFile
	if !a.Same(b) {
		t.Fatal("legacy and file must be the same")
	}
	b.TokenStore, b.DeviceToken = TokenStoreKeyring, ""
	if a.Same(b) {
		t.Fatal("keyring vs file must differ")
	}
}
