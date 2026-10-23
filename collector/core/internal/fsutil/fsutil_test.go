package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicCreatesPrivateFileAndReplaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	p := filepath.Join(dir, "a.json")
	if err := WriteFileAtomic(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "two" {
		t.Fatalf("content %q err %v", b, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm %v", fi.Mode().Perm())
		}
		di, _ := os.Stat(dir)
		if di.Mode().Perm() != 0o700 {
			t.Fatalf("dir perm %v", di.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

func TestWriteFileAtomicRefusesSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("심볼릭 링크 생성 권한이 필요해 Windows에서는 실행하지 않음")
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(link, []byte("x"), 0o600); err != ErrNotRegular {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatal("symlink target modified")
	}
}

func TestWriteFileAtomicErrorHasNoPath(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := WriteFileAtomic(filepath.Join(blocker, "sentinel-secret-dir", "x"), []byte("x"), 0o600)
	if err == nil || strings.Contains(err.Error(), "sentinel-secret-dir") || strings.Contains(err.Error(), dir) {
		t.Fatalf("err %v", err)
	}
}
