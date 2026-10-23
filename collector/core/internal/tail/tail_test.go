package tail

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// 참조 구현 테스트(tests/test_collector_reference.py의 tail 절)를 그대로 옮긴다.
// 데이터는 모두 합성 값이다.

func writeFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, p string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func records(t *testing.T, res Result, want ...string) {
	t.Helper()
	got := make([]string, len(res.Records))
	for i, r := range res.Records {
		got[i] = string(r)
	}
	if len(want) == 0 {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("records = %q, want %q", got, want)
	}
}

func size(t *testing.T, p string) int64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func TestPartialLineIsConsumedOnlyAfterNewline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n{\"b\": "))

	first := PollOnce(p, Checkpoint{}, Options{})
	appendFile(t, p, []byte("2}\n"))
	second := PollOnce(p, first.Checkpoint, Options{})

	records(t, first, `{"a": 1}`)
	if first.PendingPartialBytes != int64(len(`{"b": `)) {
		t.Fatalf("pending = %d", first.PendingPartialBytes)
	}
	records(t, second, `{"b": 2}`)
	if second.Checkpoint.Offset != size(t, p) {
		t.Fatalf("offset = %d", second.Checkpoint.Offset)
	}
}

func TestBOMAndCRLFAreTolerated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, append(append([]byte{}, utf8BOM...), "{\"a\": 1}\r\n\r\n{\"b\": 2}\r\n"...))

	res := PollOnce(p, Checkpoint{}, Options{})

	records(t, res, `{"a": 1}`, `{"b": 2}`)
	if res.InvalidLines != 0 {
		t.Fatalf("invalid = %d", res.InvalidLines)
	}
}

func TestInvalidLinesAreSkippedWithoutStalling(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\nnot-json\n[1, 2]\n\xff\xfe\n{\"b\": 2}\n"))

	res := PollOnce(p, Checkpoint{}, Options{})

	records(t, res, `{"a": 1}`, `{"b": 2}`)
	if res.InvalidLines != 3 {
		t.Fatalf("invalid = %d", res.InvalidLines)
	}
	records(t, PollOnce(p, res.Checkpoint, Options{}))
}

func TestRestartResumesFromPersistedCheckpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	saved, err := MarshalCheckpoint(PollOnce(p, Checkpoint{}, Options{}).Checkpoint)
	if err != nil {
		t.Fatal(err)
	}

	appendFile(t, p, []byte("{\"b\": 2}\n"))
	cp, err := UnmarshalCheckpoint(saved)
	if err != nil {
		t.Fatal(err)
	}
	resumed := PollOnce(p, cp, Options{})

	records(t, resumed, `{"b": 2}`)
	if resumed.Reset != ResetNone {
		t.Fatalf("reset = %q", resumed.Reset)
	}
}

func TestTruncationRestartsFromBeginning(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n{\"b\": 2}\n"))
	cp := PollOnce(p, Checkpoint{}, Options{}).Checkpoint

	writeFile(t, p, []byte("{\"c\": 3}\n"))
	res := PollOnce(p, cp, Options{})

	if res.Reset != ResetTruncated {
		t.Fatalf("reset = %q", res.Reset)
	}
	records(t, res, `{"c": 3}`)
}

func TestRewriteWithSameSizeIsDetected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	cp := PollOnce(p, Checkpoint{}, Options{}).Checkpoint

	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("{\"z\": 9}\n")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	appendFile(t, p, []byte("{\"b\": 2}\n"))
	res := PollOnce(p, cp, Options{})

	if res.Reset != ResetRewritten {
		t.Fatalf("reset = %q", res.Reset)
	}
	records(t, res, `{"z": 9}`, `{"b": 2}`)
}

func TestRotationReadsNewFileFromStart(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	cp := PollOnce(p, Checkpoint{}, Options{}).Checkpoint

	if err := os.Rename(p, filepath.Join(dir, "t.jsonl.1")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, []byte("{\"b\": 2}\n"))
	res := PollOnce(p, cp, Options{})

	if res.Reset != ResetRotated {
		t.Fatalf("reset = %q", res.Reset)
	}
	records(t, res, `{"b": 2}`)
}

func TestMissingFileKeepsCheckpoint(t *testing.T) {
	cp := Checkpoint{Version: CheckpointVersion, File: &FileID{Device: 1, Inode: 2}, Offset: 10}

	res := PollOnce(filepath.Join(t.TempDir(), "absent.jsonl"), cp, Options{})

	if res.Status != StatusMissing {
		t.Fatalf("status = %q", res.Status)
	}
	if !reflect.DeepEqual(res.Checkpoint, cp) {
		t.Fatalf("checkpoint = %+v", res.Checkpoint)
	}
	records(t, res)
}

func TestPermissionDeniedFailsOpen(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("POSIX 권한으로 읽기 거부를 재현할 수 없는 환경(Windows 또는 root)")
	}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	if err := os.Chmod(p, 0); err != nil { // 테스트가 만든 합성 파일만 바꾼다.
		t.Fatal(err)
	}
	defer os.Chmod(p, 0o600)

	res := PollOnce(p, Checkpoint{}, Options{})

	if res.Status != StatusPermissionDenied {
		t.Fatalf("status = %q", res.Status)
	}
	records(t, res)
	if res.Checkpoint.Offset != 0 {
		t.Fatalf("offset = %d", res.Checkpoint.Offset)
	}
}

func TestOversizedLineIsDroppedAndTailContinues(t *testing.T) {
	opts := Options{MaxLineBytes: 16}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, append([]byte("{\"a\": 1}\n"), bytes.Repeat([]byte("x"), 40)...))

	first := PollOnce(p, Checkpoint{}, opts)
	appendFile(t, p, []byte("yyyy\n{\"b\": 2}\n"))
	second := PollOnce(p, first.Checkpoint, opts)

	records(t, first, `{"a": 1}`)
	if first.OversizedLines != 1 {
		t.Fatalf("first oversized = %d", first.OversizedLines)
	}
	records(t, second, `{"b": 2}`)
	if second.OversizedLines != 0 {
		t.Fatalf("second oversized = %d", second.OversizedLines)
	}
}

// --- 참조 구현 밖의 Go 동작 ---------------------------------------------

func TestCompleteOversizedLineIsCountedOnce(t *testing.T) {
	opts := Options{MaxLineBytes: 16}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n{\"long\": \"xxxxxxxxxxxxxxxxxxxx\"}\n{\"b\": 2}\n"))

	res := PollOnce(p, Checkpoint{}, opts)

	records(t, res, `{"a": 1}`, `{"b": 2}`)
	if res.OversizedLines != 1 || res.Checkpoint.SkippingOversizedLine {
		t.Fatalf("oversized = %d skipping = %v", res.OversizedLines, res.Checkpoint.SkippingOversizedLine)
	}
}

func TestReadBudgetSplitsLargeBacklog(t *testing.T) {
	opts := Options{MaxLineBytes: 16, MaxBytesPerPoll: 1}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	var data []byte
	for i := 0; i < 20; i++ {
		data = append(data, "{\"n\": 1}\n"...)
	}
	writeFile(t, p, data)

	tl := New(p, Checkpoint{}, opts)
	defer tl.Close()
	total, polls := 0, 0
	for {
		res := tl.Poll()
		total += len(res.Records)
		polls++
		if !res.More {
			break
		}
	}
	if total != 20 || polls < 2 {
		t.Fatalf("total = %d polls = %d", total, polls)
	}
}

func TestCheckpointJSONIsVersioned(t *testing.T) {
	data, err := MarshalCheckpoint(Checkpoint{File: &FileID{Device: 7, Inode: 9}, Offset: 3, PrefixDigest: "ab"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"file":{"device":7,"inode":9},"offset":3,"prefix_digest":"ab"}`
	if string(data) != want {
		t.Fatalf("json = %s", data)
	}
	if _, err := UnmarshalCheckpoint([]byte(`{"version":2,"offset":0}`)); err != ErrCheckpointVersion {
		t.Fatalf("err = %v", err)
	}
	if _, err := UnmarshalCheckpoint([]byte(`{"version":1,"offset":-1}`)); err != ErrCheckpointMalformed {
		t.Fatalf("err = %v", err)
	}
	if _, err := UnmarshalCheckpoint([]byte(`secret-content`)); err != ErrCheckpointMalformed {
		t.Fatalf("err = %v", err)
	}
}

func TestDirectoryIsUnsupported(t *testing.T) {
	res := PollOnce(t.TempDir(), Checkpoint{}, Options{})
	if res.Status != StatusUnsupported && res.Status != StatusPermissionDenied {
		t.Fatalf("status = %q", res.Status)
	}
	records(t, res)
}

func TestKeptHandleReadsAppendsAcrossPolls(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, []byte("{\"a\": 1}\n"))
	tl := New(p, Checkpoint{}, Options{})
	defer tl.Close()

	records(t, tl.Poll(), `{"a": 1}`)
	records(t, tl.Poll())
	appendFile(t, p, []byte("{\"b\": 2}\n"))
	records(t, tl.Poll(), `{"b": 2}`)
	if tl.Checkpoint().Offset != size(t, p) {
		t.Fatalf("offset = %d", tl.Checkpoint().Offset)
	}
}
