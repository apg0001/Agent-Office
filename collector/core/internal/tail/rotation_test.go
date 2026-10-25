package tail

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 참조 구현은 로테이션 때 이전 파일의 읽지 않은 꼬리를 버린다. Go Tailer는 열린 핸들로 먼저 읽는다.
func TestRotationDrainsOldFileBeforeSwitching(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	tl := New(p, Checkpoint{}, Options{})
	defer tl.Close()
	records(t, tl.Poll(), `{"a": 1}`)

	appendFile(t, p, []byte("{\"b\": 2}\n{\"partial\": "))
	if err := os.Rename(p, filepath.Join(dir, "t.jsonl.1")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, []byte("{\"c\": 3}\n"))
	res := tl.Poll()

	if res.Reset != ResetRotated || res.DrainedRecords != 1 {
		t.Fatalf("reset = %q drained = %d", res.Reset, res.DrainedRecords)
	}
	records(t, res, `{"b": 2}`, `{"c": 3}`)
	if tl.Checkpoint().Offset != size(t, p) {
		t.Fatalf("offset = %d", tl.Checkpoint().Offset)
	}
	records(t, tl.Poll())
}

// 이름이 바뀐 뒤 새 파일이 아직 없으면 상태는 missing이지만 이전 파일의 꼬리는 읽는다.
func TestRenamedAwayFileIsDrainedWhileMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	tl := New(p, Checkpoint{}, Options{})
	defer tl.Close()
	records(t, tl.Poll(), `{"a": 1}`)

	appendFile(t, p, []byte("{\"b\": 2}\n"))
	if err := os.Rename(p, filepath.Join(dir, "t.jsonl.1")); err != nil {
		t.Fatal(err)
	}
	res := tl.Poll()

	if res.Status != StatusMissing || res.DrainedRecords != 1 {
		t.Fatalf("status = %q drained = %d", res.Status, res.DrainedRecords)
	}
	records(t, res, `{"b": 2}`)

	writeFile(t, p, []byte("{\"c\": 3}\n"))
	next := tl.Poll()
	if next.Reset != ResetRotated {
		t.Fatalf("reset = %q", next.Reset)
	}
	records(t, next, `{"c": 3}`)
}

// 삭제 후 같은 이름으로 다시 만든 경우(교체)도 로테이션으로 처리한다.
func TestReplaceByDeleteAndRecreate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows는 열린 핸들이 있으면 같은 이름 재생성이 지연될 수 있어 실행 검증하지 않음")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	tl := New(p, Checkpoint{}, Options{})
	defer tl.Close()
	records(t, tl.Poll(), `{"a": 1}`)

	if err := os.Remove(p); err != nil { // 테스트가 만든 합성 파일만 지운다.
		t.Fatal(err)
	}
	writeFile(t, p, []byte("{\"x\": 1}\n{\"y\": 2}\n"))
	res := tl.Poll()

	if res.Reset != ResetRotated {
		t.Fatalf("reset = %q", res.Reset)
	}
	records(t, res, `{"x": 1}`, `{"y": 2}`)
}
