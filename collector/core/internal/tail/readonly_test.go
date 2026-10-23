package tail

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type snapshot struct {
	data  []byte
	mode  fs.FileMode
	mtime time.Time
}

func snapshotTree(t *testing.T, root string) map[string]snapshot {
	t.Helper()
	out := map[string]snapshot{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		s := snapshot{mode: info.Mode(), mtime: info.ModTime()}
		if info.Mode().IsRegular() {
			if s.data, err = os.ReadFile(p); err != nil {
				return err
			}
		}
		out[p] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 감시 루트 아래 파일의 내용·권한·mtime과 디렉터리 항목이 tail 전후로 같아야 한다.
func TestTailingNeverModifiesWatchedRoot(t *testing.T) {
	root := t.TempDir()
	mkfile(t, root, "proj/s1.jsonl")
	mkfile(t, root, "proj/s1/subagents/a1.jsonl")
	writeFile(t, filepath.Join(root, "proj", "s2.jsonl"),
		append(append([]byte{}, utf8BOM...), "{\"a\": 1}\r\nbad\n{\"partial\": "...))
	if err := os.Chmod(filepath.Join(root, "proj", "s2.jsonl"), 0o444); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, rel := range []string{"proj/s1.jsonl", "proj/s2.jsonl", "proj/s1/subagents/a1.jsonl"} {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), old, old); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshotTree(t, root)

	d, err := Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var tailers []*Tailer
	for _, f := range d.Files {
		tl := New(f.Path(root), Checkpoint{}, Options{MaxLineBytes: 8})
		tailers = append(tailers, tl)
		for i := 0; i < 3; i++ {
			_ = tl.Poll()
		}
		_ = PollOnce(f.Path(root), Checkpoint{Offset: 1 << 30}, Options{}) // truncated 경로
	}
	for _, tl := range tailers {
		if err := tl.Close(); err != nil {
			t.Fatal(err)
		}
	}

	after := snapshotTree(t, root)
	if len(after) != len(before) {
		t.Fatalf("entries changed: %d -> %d", len(before), len(after))
	}
	for p, b := range before {
		a, ok := after[p]
		if !ok {
			t.Fatalf("entry removed: %s", filepath.Base(p))
		}
		if a.mode != b.mode || !bytes.Equal(a.data, b.data) {
			t.Fatalf("entry modified: %s", filepath.Base(p))
		}
		if b.mode.IsRegular() && !a.mtime.Equal(b.mtime) {
			t.Fatalf("mtime changed: %s", filepath.Base(p))
		}
	}
}
