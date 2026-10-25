package tail

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mkfile(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, []byte("{\"a\": 1}\n"))
}

func TestDiscoverClaudeLayoutWithBoundedDepth(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "proj-a/11111111-1111-4111-8111-111111111111.jsonl")
	mkfile(t, root, "proj-a/11111111-1111-4111-8111-111111111111/subagents/agent-a1.jsonl")
	mkfile(t, root, "proj-b/22222222-2222-4222-8222-222222222222.jsonl")
	mkfile(t, root, "proj-b/notes.txt")
	mkfile(t, root, "proj-b/s/subagents/deeper/too-deep.jsonl")
	mkfile(t, root, "proj-b/s/other/x.jsonl")

	d, err := Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]FileKind{
		"proj-a/11111111-1111-4111-8111-111111111111.jsonl":                    KindMain,
		"proj-a/11111111-1111-4111-8111-111111111111/subagents/agent-a1.jsonl": KindSubagent,
		"proj-b/22222222-2222-4222-8222-222222222222.jsonl":                    KindMain,
		"proj-b/s/other/x.jsonl":                                               KindOther,
	}
	if len(d.Files) != len(want) {
		t.Fatalf("files = %+v", d.Files)
	}
	for i, f := range d.Files {
		if want[f.Rel] != f.Kind {
			t.Fatalf("%s kind = %q", f.Rel, f.Kind)
		}
		if i > 0 && d.Files[i-1].Rel >= f.Rel {
			t.Fatal("not sorted")
		}
		if len(f.ID) != 32 || strings.Contains(f.ID, "proj") {
			t.Fatalf("id = %q", f.ID)
		}
		if filepath.IsAbs(f.Rel) || strings.Contains(f.Rel, root) {
			t.Fatalf("rel is absolute: %q", f.Rel)
		}
		if _, err := os.Stat(f.Path(root)); err != nil {
			t.Fatal(err)
		}
	}

	again, _ := Discover(root, DiscoverOptions{})
	for i := range again.Files {
		if again.Files[i] != d.Files[i] {
			t.Fatal("identifiers are not stable")
		}
	}

	shallow, _ := Discover(root, DiscoverOptions{MaxDepth: 2})
	if len(shallow.Files) != 2 {
		t.Fatalf("depth 2 files = %+v", shallow.Files)
	}
	limited, _ := Discover(root, DiscoverOptions{MaxFiles: 1})
	if len(limited.Files) != 1 || !limited.Truncated {
		t.Fatalf("limited = %+v", limited)
	}
}

func TestDiscoverDoesNotFollowSymlinks(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	mkfile(t, root, "proj/inside.jsonl")
	mkfile(t, outside, "secret.jsonl")
	mkfile(t, outside, "nested/secret.jsonl")

	if err := os.Symlink(outside, filepath.Join(root, "proj", "linkdir")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows 심볼릭 링크 생성 권한 없음")
		}
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.jsonl"), filepath.Join(root, "proj", "link.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "toplink")); err != nil {
		t.Fatal(err)
	}

	d, err := Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.Files[0].Rel != "proj/inside.jsonl" {
		t.Fatalf("files = %+v", d.Files)
	}
	if d.SkippedSymlinks != 3 {
		t.Fatalf("skipped symlinks = %d", d.SkippedSymlinks)
	}
}

func TestDiscoverMissingRootHidesPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "private-project-name")
	_, err := Discover(missing, DiscoverOptions{})
	if err != ErrRootUnavailable {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "private-project-name") || strings.Contains(err.Error(), string(filepath.Separator)) {
		t.Fatalf("error leaks path: %v", err)
	}
}

func TestDiscoverSkipsUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("POSIX 권한으로 읽기 거부를 재현할 수 없는 환경(Windows 또는 root)")
	}
	root := t.TempDir()
	mkfile(t, root, "ok/a.jsonl")
	mkfile(t, root, "locked/b.jsonl")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil { // 테스트가 만든 합성 디렉터리만 바꾼다.
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o700)

	d, err := Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.SkippedDirs != 1 {
		t.Fatalf("discovery = %+v", d)
	}
}
