// Package tail은 append-only JSONL transcript에서 새로 추가된 완결 줄만 읽는다(#30).
//
// 규칙은 #20 참조 구현(tests/poc/collector/tail_reference.py)을 그대로 따르고, 로테이션 때
// 열린 핸들로 이전 파일의 남은 완결 줄을 먼저 읽는 점만 다르다. 파일은 읽기 전용으로만 열며
// 줄 내용을 로그·오류·체크포인트에 남기지 않는다. 상태는 고정 enum으로만 알린다(fail-open).
package tail

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
	"unicode/utf8"
)

const (
	// PrefixBytes는 체크포인트 앞부분 지문 길이다. 같은 파일이 다시 쓰였는지만 판정한다.
	PrefixBytes = 256
	// DefaultMaxLineBytes는 줄 하나의 상한이다(adapter.MaxTranscriptLineBytes와 같은 1MiB).
	DefaultMaxLineBytes = 1 << 20
	// DefaultMaxBytesPerPoll은 Poll 한 번에 읽는 최대 바이트다. 큰 파일의 첫 읽기도 메모리를 묶어 둔다.
	DefaultMaxBytesPerPoll = 16 << 20
	readChunkBytes         = 64 << 10
)

var (
	utf8BOM       = []byte{0xEF, 0xBB, 0xBF}
	errNoIdentity = errors.New("tail: file identity unavailable")
)

// Status는 Poll 결과 상태다. 경로나 OS 오류 문자열은 담지 않는다.
type Status string

const (
	StatusOK               Status = "ok"
	StatusMissing          Status = "missing"
	StatusPermissionDenied Status = "permission_denied"
	// StatusUnsupported는 일반 파일이 아니거나 파일 식별자를 얻을 수 없는 경우다.
	StatusUnsupported Status = "unsupported"
	// StatusError는 그 밖의 읽기 오류다. 체크포인트는 유지된다.
	StatusError Status = "error"
)

// ResetReason은 처음부터 다시 읽은 이유다.
type ResetReason string

const (
	ResetNone      ResetReason = ""
	ResetRotated   ResetReason = "rotated"
	ResetTruncated ResetReason = "truncated"
	ResetRewritten ResetReason = "rewritten"
)

// Options는 상한 설정이다. 0이면 기본값을 쓴다.
type Options struct {
	MaxLineBytes    int
	MaxBytesPerPoll int64
}

func (o Options) withDefaults() Options {
	if o.MaxLineBytes <= 0 {
		o.MaxLineBytes = DefaultMaxLineBytes
	}
	if o.MaxBytesPerPoll <= 0 {
		o.MaxBytesPerPoll = DefaultMaxBytesPerPoll
	}
	// 한 줄을 끝까지 볼 수 있어야 초과 줄 판정이 진행된다.
	if floor := int64(o.MaxLineBytes) + int64(len(utf8BOM)) + 1; o.MaxBytesPerPoll < floor {
		o.MaxBytesPerPoll = floor
	}
	return o
}

// Result는 Poll 한 번의 결과다.
type Result struct {
	// Checkpoint는 이 결과의 Records를 처리한 뒤 영속화할 다음 위치다.
	Checkpoint Checkpoint
	// Records는 JSON 객체로 검증된 줄 원문 바이트다(BOM·CR 제거). 어댑터
	// claude.ParseTranscriptLine에 그대로 넘긴다. 호출자는 내용을 로그로 남기지 않는다.
	Records [][]byte
	Status  Status
	Reset   ResetReason
	// DrainedRecords는 Records 앞쪽 중 로테이션·삭제 전 이전 파일에서 읽은 개수다.
	DrainedRecords      int
	InvalidLines        int
	OversizedLines      int
	PendingPartialBytes int64
	// More면 읽기 상한 때문에 아직 읽지 않은 바이트가 남아 있다. 곧바로 다시 Poll하면 된다.
	More bool
	// Denied는 StatusPermissionDenied일 때 단계와 오류 종류다(예: "open_eperm"). 경로는 담지 않는다(#184).
	Denied string
}

// Tailer는 경로 하나를 tail한다. 같은 파일을 읽는 동안 읽기 전용 핸들을 열어 두고,
// 경로가 다른 파일로 바뀌면 그 핸들로 이전 파일을 끝까지 읽은 뒤 전환한다.
// 동시 사용에 안전하지 않다. 호출자가 한 고루틴에서 Poll한다.
type Tailer struct {
	path string
	opts Options
	cp   Checkpoint

	f   *os.File
	fid FileID
}

// New는 저장된 체크포인트(없으면 영값)에서 시작하는 Tailer를 만든다. 파일은 Poll 때 연다.
func New(path string, cp Checkpoint, opts Options) *Tailer {
	cp.Version = CheckpointVersion
	return &Tailer{path: path, opts: opts.withDefaults(), cp: cp}
}

// Checkpoint는 현재 체크포인트다.
func (t *Tailer) Checkpoint() Checkpoint { return t.cp }

// Close는 열린 핸들을 닫는다. 체크포인트는 유지되며 다시 Poll할 수 있다.
func (t *Tailer) Close() error {
	if t.f == nil {
		return nil
	}
	err := t.f.Close()
	t.f = nil
	return err
}

// PollOnce는 핸들을 유지하지 않는 1회 읽기다(참조 구현 poll과 같은 동작, 로테이션 drain 없음).
func PollOnce(path string, cp Checkpoint, opts Options) Result {
	t := New(path, cp, opts)
	defer t.Close()
	return t.Poll()
}

// Poll은 새로 append된 완결 줄을 읽는다. 오류를 반환하지 않고 Status로만 알린다.
func (t *Tailer) Poll() (res Result) {
	defer func() {
		if recover() != nil {
			res = Result{Checkpoint: t.cp, Status: StatusError}
		}
	}()
	res = Result{Checkpoint: t.cp, Status: StatusOK}

	f, err := openReadOnly(t.path)
	if err != nil {
		// 경로가 사라져도 열린 핸들의 이전 파일에서 남은 완결 줄은 읽는다.
		t.drain(&res)
		res.Status = classify(err)
		res.Denied = deniedKind("open", err)
		res.Checkpoint = t.cp
		return res
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		res.Status = classify(err)
		res.Denied = deniedKind("stat", err)
		return res
	}
	id, err := fileIdentity(f, info)
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		res.Status = StatusUnsupported
		return res
	}

	reset := ResetNone
	if t.cp.File != nil && *t.cp.File != id {
		reset = ResetRotated
		t.drain(&res)
	} else if info.Size() < t.cp.Offset {
		reset = ResetTruncated
	}

	// 같은 파일이면 기존 핸들을 유지하고, 다른 파일이면 새 핸들로 바꾼다.
	if t.f != nil && t.fid == id {
		f.Close()
	} else {
		t.Close()
		t.f, t.fid = f, id
	}

	cp := t.cp
	if reset == ResetNone && cp.PrefixDigest != "" {
		digest, err := prefixDigest(t.f, cp.Offset)
		if err != nil {
			res.Status = classify(err)
			return res
		}
		if digest != cp.PrefixDigest {
			reset = ResetRewritten
		}
	}
	if reset != ResetNone {
		cp = Checkpoint{Version: CheckpointVersion}
	}
	cp.File = &FileID{Device: id.Device, Inode: id.Inode}

	scanned, next, err := scan(t.f, cp, info.Size(), t.opts)
	if err != nil {
		res.Status = classify(err)
		res.Checkpoint = t.cp
		return res
	}
	res.Reset = reset
	res.merge(scanned)
	t.cp = next
	res.Checkpoint = next
	return res
}

// drain은 열린 핸들이 가리키는 이전 파일의 남은 완결 줄을 읽어 res 앞쪽에 붙인다.
func (t *Tailer) drain(res *Result) {
	if t.f == nil || t.cp.File == nil || *t.cp.File != t.fid {
		return
	}
	info, err := t.f.Stat()
	if err != nil || info.Size() < t.cp.Offset {
		return
	}
	scanned, next, err := scan(t.f, t.cp, info.Size(), t.opts)
	if err != nil {
		return
	}
	res.merge(scanned)
	res.DrainedRecords += len(scanned.Records)
	t.cp = next
}

func (r *Result) merge(o Result) {
	r.Records = append(r.Records, o.Records...)
	r.InvalidLines += o.InvalidLines
	r.OversizedLines += o.OversizedLines
	r.PendingPartialBytes = o.PendingPartialBytes
	r.More = r.More || o.More
}

func classify(err error) Status {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return StatusMissing
	case errors.Is(err, fs.ErrPermission):
		return StatusPermissionDenied
	default:
		return StatusError
	}
}

// deniedKind는 권한 오류의 단계·종류다. EACCES는 파일 권한, EPERM은 macOS 개인정보 보호(TCC)나
// 플래그처럼 권한 비트 밖의 거부인 경우가 많다(#184). 권한 오류가 아니면 "".
func deniedKind(stage string, err error) string {
	if !errors.Is(err, fs.ErrPermission) {
		return ""
	}
	switch {
	case errors.Is(err, syscall.EPERM):
		return stage + "_eperm"
	case errors.Is(err, syscall.EACCES):
		return stage + "_eacces"
	default:
		return stage + "_other"
	}
}

// prefixDigest는 앞부분 min(length, PrefixBytes) 바이트의 SHA-256 hex다. length가 0이면 "".
func prefixDigest(r io.ReaderAt, length int64) (string, error) {
	if length <= 0 {
		return "", nil
	}
	n := min(length, PrefixBytes)
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, 0); err != nil {
		if !errors.Is(err, io.EOF) {
			return "", err
		}
		// 파일이 그 사이 줄었으면 지문이 달라지게 둔다.
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// scan은 cp.Offset부터 size까지(읽기 상한 적용) 완결 줄을 읽는다.
// 개행 없는 마지막 조각은 소비하지 않고, 상한을 넘은 줄은 다음 개행까지 버린다.
func scan(r io.ReaderAt, cp Checkpoint, size int64, opts Options) (Result, Checkpoint, error) {
	var res Result
	start := cp.Offset
	if size <= start {
		return res, cp, nil
	}
	end := size
	if end-start > opts.MaxBytesPerPoll {
		end = start + opts.MaxBytesPerPoll
		res.More = true
	}

	maxLine := opts.MaxLineBytes
	section := io.NewSectionReader(r, start, end-start)
	buf := make([]byte, readChunkBytes)
	var line []byte
	pos, consumed, lineStart := start, start, start
	skipping := cp.SkippingOversizedLine
	overflow := false

	for {
		n, err := section.Read(buf)
		chunk := buf[:n]
		for len(chunk) > 0 {
			nl := bytes.IndexByte(chunk, '\n')
			seg := chunk
			if nl >= 0 {
				seg = chunk[:nl]
			}
			if !skipping && !overflow {
				line = append(line, seg...)
				if contentLen(line, lineStart) > maxLine {
					overflow = true
					line = line[:0]
				}
			}
			if nl < 0 {
				pos += int64(len(chunk))
				break
			}
			pos += int64(nl + 1)
			chunk = chunk[nl+1:]
			switch {
			case skipping:
				skipping = false
			case overflow:
				res.OversizedLines++
				overflow = false
			default:
				res.emit(line, lineStart)
			}
			line = line[:0]
			consumed, lineStart = pos, pos
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, cp, err
		}
	}

	switch {
	case skipping:
		consumed = pos
	case overflow:
		// 개행 없이 상한을 넘은 줄은 버리고 다음 개행까지 건너뛴다.
		res.OversizedLines++
		consumed = pos
		skipping = true
	default:
		res.PendingPartialBytes = pos - consumed
	}

	digest, err := prefixDigest(r, consumed)
	if err != nil {
		return Result{}, cp, err
	}
	cp.Offset = consumed
	cp.PrefixDigest = digest
	cp.SkippingOversizedLine = skipping
	return res, cp, nil
}

// contentLen은 파일 시작 BOM을 뺀 줄 길이다(참조 구현은 BOM 제거 후 상한을 비교).
func contentLen(line []byte, lineStart int64) int {
	if lineStart == 0 && bytes.HasPrefix(line, utf8BOM) {
		return len(line) - len(utf8BOM)
	}
	return len(line)
}

const asciiSpace = " \t\n\v\f\r"

// emit은 완결 줄 하나를 검증해 Records에 복사하거나 InvalidLines를 센다.
func (r *Result) emit(line []byte, lineStart int64) {
	if lineStart == 0 {
		line = bytes.TrimPrefix(line, utf8BOM)
	}
	line = bytes.TrimRight(line, "\r")
	trimmed := bytes.Trim(line, asciiSpace)
	if len(trimmed) == 0 {
		return
	}
	if trimmed[0] != '{' || !utf8.Valid(line) || !json.Valid(line) {
		r.InvalidLines++
		return
	}
	r.Records = append(r.Records, bytes.Clone(line))
}
