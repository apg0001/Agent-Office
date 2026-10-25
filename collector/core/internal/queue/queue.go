// Package queue는 Collector 로컬 SQLite 큐다(#33).
//
// 정규화된 이벤트 v1(normalize.Record)과 그 이벤트를 만든 상태(중복 제거용 결정적 event_id,
// 세션 sequence, tail 체크포인트)를 한 트랜잭션으로 기록한다. 서버가 영속 ACK(accepted·duplicate)를
// 준 행만 지운다. 저장하는 값은 v1 화이트리스트 13개 필드 JSON, UUID, 숫자, 거부 코드 enum,
// 체크포인트(바이트 위치·해시)뿐이며 원본 프롬프트·도구 입출력·경로·원본 세션 참조는 저장하지 않는다.
//
// 드라이버는 cgo가 필요 없는 modernc.org/sqlite다(Windows·macOS 교차 빌드).
package queue

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/normalize"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"

	_ "modernc.org/sqlite" // database/sql 드라이버 "sqlite" 등록.
)

// SchemaVersion은 이 빌드가 만드는 DB 스키마 버전(PRAGMA user_version)이다.
const SchemaVersion = 1

// 기본값.
const (
	// DefaultMaxEvents는 큐 행 상한이다. 넘으면 가장 오래된 행부터 버리고 개수를 센다.
	DefaultMaxEvents = 100_000
	// DefaultMaxAttempts는 retryable 거부를 받은 이벤트를 dead-letter로 보내기 전 최대 시도 수다.
	DefaultMaxAttempts = 20
	// DefaultBusyTimeout은 SQLite busy_timeout이다.
	DefaultBusyTimeout = 5 * time.Second
)

// Dead-letter 사유 중 서버 거부 코드가 아닌 것.
const (
	// ReasonInvalidRequest는 1건 배치가 요청 검증(422)에서 거부된 경우다.
	ReasonInvalidRequest = "invalid_request"
	// ReasonMaxAttempts는 retryable 거부가 MaxAttempts번 반복된 경우다.
	ReasonMaxAttempts = "max_attempts"
	// ReasonUnknownCode는 v1 enum 밖 거부 코드다. 서버 문자열 원문은 저장하지 않는다.
	ReasonUnknownCode = "unknown_code"
	// ReasonCorrupt는 저장된 행을 읽을 수 없는 경우다.
	ReasonCorrupt = "corrupt_row"
)

// 거부 코드 v1 enum(collector-event-batch-response-v1.schema.json).
var rejectCodes = map[string]bool{
	"unauthorized_device": true, "unauthorized_scope": true, "unsupported_schema_version": true,
	"retention_window_exceeded": true, "event_id_conflict": true, "parent_agent_unknown": true,
	"parent_agent_conflict": true, "occurred_at_out_of_range": true, "internal_error": true,
}

var (
	// ErrSchemaTooNew는 DB가 이 빌드보다 새 스키마일 때 반환한다(다운그레이드). DB는 건드리지 않는다.
	ErrSchemaTooNew = errors.New("queue: database schema is newer than this build")
	// ErrClosed는 닫힌 큐를 쓸 때 반환한다.
	ErrClosed = errors.New("queue: closed")
	// ErrInvalidRecord는 event_id·device_id·session_id가 UUID가 아니거나 sequence가 범위 밖일 때다.
	ErrInvalidRecord = errors.New("queue: invalid record")
)

// Options는 큐 설정이다. 0 값은 기본값이다.
type Options struct {
	MaxEvents   int
	MaxAttempts int
	BusyTimeout time.Duration
	// Now는 기록 시각용 시계다(테스트 주입). nil이면 time.Now.
	Now func() time.Time
}

// Queue는 SQLite 큐다. 동시 사용에 안전하다(쓰기는 내부에서 직렬화).
type Queue struct {
	db   *sql.DB
	opts Options

	mu     sync.Mutex
	closed bool
	depth  int
	// seq는 (device|session) → 다음 sequence 캐시다. Next는 캐시만 올리고, 실제 영속화는
	// Enqueue 트랜잭션이 이벤트 행과 함께 한다(이벤트 없이 소비된 값은 재시작 후 다시 쓰일 수 있음).
	seq map[string]int64
}

// Open은 path에 DB를 열거나 만든다. 디렉터리는 0700, 파일은 0600으로 만든다.
func Open(path string, opts Options) (*Queue, error) {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = DefaultMaxEvents
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = DefaultMaxAttempts
	}
	if opts.BusyTimeout <= 0 {
		opts.BusyTimeout = DefaultBusyTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("queue: create directory: %w", errorsOnly(err))
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("queue: create file: %w", errorsOnly(err))
	}
	_ = f.Close()

	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", opts.BusyTimeout.Milliseconds()))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_txlock", "immediate")
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("queue: open: %w", errorsOnly(err))
	}
	// 단일 연결로 프로세스 안 쓰기를 직렬화한다(SQLITE_BUSY 회피).
	db.SetMaxOpenConns(1)
	qu := &Queue{db: db, opts: opts, seq: map[string]int64{}}
	if err := qu.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&qu.depth); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("queue: count: %w", err)
	}
	return qu, nil
}

// errorsOnly는 경로가 들어 있을 수 있는 *os.PathError에서 경로를 뺀다.
func errorsOnly(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return err
}

var migrations = []string{
	// v1
	`CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id TEXT NOT NULL UNIQUE,
		payload BLOB NOT NULL,
		enqueued_at INTEGER NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE seen_event_ids (
		event_id TEXT PRIMARY KEY,
		seen_at INTEGER NOT NULL
	) WITHOUT ROWID;
	CREATE INDEX seen_event_ids_seen_at ON seen_event_ids(seen_at);
	CREATE TABLE sequences (
		device_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		next INTEGER NOT NULL,
		PRIMARY KEY (device_id, session_id)
	) WITHOUT ROWID;
	CREATE TABLE checkpoints (
		key_digest TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		updated_at INTEGER NOT NULL
	) WITHOUT ROWID;
	CREATE TABLE counters (
		name TEXT PRIMARY KEY,
		value INTEGER NOT NULL
	) WITHOUT ROWID;`,
}

func (q *Queue) migrate() error {
	var v int
	if err := q.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("queue: read schema version: %w", err)
	}
	if v > len(migrations) {
		return ErrSchemaTooNew
	}
	for i := v; i < len(migrations); i++ {
		tx, err := q.db.Begin()
		if err != nil {
			return fmt.Errorf("queue: migrate: %w", err)
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("queue: migrate v%d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("queue: migrate v%d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("queue: migrate v%d: %w", i+1, err)
		}
	}
	return nil
}

// Close는 DB를 닫는다.
func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	q.closed = true
	return q.db.Close()
}

func (q *Queue) lock() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return ErrClosed
	}
	return nil
}

// Next는 normalize.SequenceStore 구현이다. 캐시에 없으면 DB의 다음 값에서 시작한다.
func (q *Queue) Next(deviceID, sessionID string) (int64, error) {
	if err := q.lock(); err != nil {
		return 0, err
	}
	defer q.mu.Unlock()
	key := deviceID + "|" + sessionID
	v, ok := q.seq[key]
	if !ok {
		err := q.db.QueryRow(`SELECT next FROM sequences WHERE device_id = ? AND session_id = ?`, deviceID, sessionID).Scan(&v)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("queue: read sequence: %w", err)
		}
	}
	if v > normalize.MaxSequence {
		return 0, normalize.ErrSequenceExhausted
	}
	q.seq[key] = v + 1
	return v, nil
}

// SeenEventIDs는 영속화된 결정적 event_id 집합이다. normalize.Options.SeenEventIDs에 넘긴다.
func (q *Queue) SeenEventIDs(ctx context.Context) ([]string, error) {
	if err := q.lock(); err != nil {
		return nil, err
	}
	defer q.mu.Unlock()
	rows, err := q.db.QueryContext(ctx, `SELECT event_id FROM seen_event_ids`)
	if err != nil {
		return nil, fmt.Errorf("queue: read seen ids: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("queue: read seen ids: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PruneSeen은 before보다 오래된 결정적 event_id를 지운다. 서버 보존 기간(30일)보다 오래된
// 원본을 다시 읽으면 서버가 retention_window_exceeded로 거부하므로 그보다 길게 잡는다.
func (q *Queue) PruneSeen(ctx context.Context, before time.Time) (int64, error) {
	if err := q.lock(); err != nil {
		return 0, err
	}
	defer q.mu.Unlock()
	res, err := q.db.ExecContext(ctx, `DELETE FROM seen_event_ids WHERE seen_at < ?`, before.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("queue: prune seen ids: %w", err)
	}
	return res.RowsAffected()
}

// CheckpointUpdate는 같은 트랜잭션에 기록할 tail 체크포인트다. Key(파일 경로 등)는 SHA-256만 저장한다.
type CheckpointUpdate struct {
	Key        string
	Checkpoint tail.Checkpoint
}

// EnqueueResult는 Enqueue 결과 개수다.
type EnqueueResult struct {
	Enqueued int
	// Duplicates는 이미 본 결정적 event_id이거나 이미 큐에 있는 event_id라 건너뛴 수다.
	Duplicates int
	// DroppedOldest는 상한을 넘어 이번에 버린 가장 오래된 행 수다.
	DroppedOldest int
}

// Enqueue는 레코드들과 (선택) tail 체크포인트를 한 트랜잭션으로 커밋한다. 레코드마다 순서는
// 중복 제거(결정적 event_id 기록) → sequence 영속화 → 큐 행(event_id 고정) 삽입이다.
// 오류가 나면 아무것도 커밋하지 않는다. event_id는 최초 삽입 값이 고정되어 재전송에서 그대로 쓰인다.
func (q *Queue) Enqueue(ctx context.Context, recs []normalize.Record, cp *CheckpointUpdate) (EnqueueResult, error) {
	var res EnqueueResult
	for _, r := range recs {
		if !validRecord(r) {
			return res, ErrInvalidRecord
		}
	}
	if err := q.lock(); err != nil {
		return res, err
	}
	defer q.mu.Unlock()
	now := q.opts.Now().UnixMilli()
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("queue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, r := range recs {
		ev := r.Event
		if r.DeterministicID {
			n, err := execN(ctx, tx, `INSERT INTO seen_event_ids(event_id, seen_at) VALUES (?, ?) ON CONFLICT DO NOTHING`, ev.EventID, now)
			if err != nil {
				return EnqueueResult{}, fmt.Errorf("queue: record seen id: %w", err)
			}
			if n == 0 {
				res.Duplicates++
				continue
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sequences(device_id, session_id, next) VALUES (?, ?, ?)
			ON CONFLICT(device_id, session_id) DO UPDATE SET next = MAX(next, excluded.next)`,
			ev.DeviceID, ev.SessionID, ev.Sequence+1); err != nil {
			return EnqueueResult{}, fmt.Errorf("queue: persist sequence: %w", err)
		}
		payload, err := json.Marshal(ev)
		if err != nil {
			return EnqueueResult{}, fmt.Errorf("queue: encode: %w", err)
		}
		n, err := execN(ctx, tx, `INSERT INTO events(event_id, payload, enqueued_at) VALUES (?, ?, ?) ON CONFLICT(event_id) DO NOTHING`,
			ev.EventID, payload, now)
		if err != nil {
			return EnqueueResult{}, fmt.Errorf("queue: insert: %w", err)
		}
		if n == 0 {
			res.Duplicates++
			continue
		}
		res.Enqueued++
	}

	if cp != nil {
		data, err := tail.MarshalCheckpoint(cp.Checkpoint)
		if err != nil {
			return EnqueueResult{}, fmt.Errorf("queue: encode checkpoint: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoints(key_digest, data, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key_digest) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
			CheckpointDigest(cp.Key), data, now); err != nil {
			return EnqueueResult{}, fmt.Errorf("queue: persist checkpoint: %w", err)
		}
	}

	depth := q.depth + res.Enqueued
	if over := depth - q.opts.MaxEvents; over > 0 {
		n, err := execN(ctx, tx, `DELETE FROM events WHERE id IN (SELECT id FROM events ORDER BY id LIMIT ?)`, over)
		if err != nil {
			return EnqueueResult{}, fmt.Errorf("queue: drop oldest: %w", err)
		}
		if err := addCounter(ctx, tx, counterDroppedOldest, n); err != nil {
			return EnqueueResult{}, err
		}
		res.DroppedOldest = int(n)
		depth -= int(n)
	}
	if err := tx.Commit(); err != nil {
		return EnqueueResult{}, fmt.Errorf("queue: commit: %w", err)
	}
	q.depth = depth
	for _, r := range recs {
		key := r.Event.DeviceID + "|" + r.Event.SessionID
		if q.seq[key] <= r.Event.Sequence {
			q.seq[key] = r.Event.Sequence + 1
		}
	}
	return res, nil
}

func validRecord(r normalize.Record) bool {
	ev := r.Event
	return normalize.IsUUID(ev.EventID) && normalize.IsUUID(ev.DeviceID) && normalize.IsUUID(ev.SessionID) &&
		ev.Sequence >= 0 && ev.Sequence <= normalize.MaxSequence
}

// CheckpointDigest는 체크포인트 키(경로 등)의 SHA-256 hex다. 원문 키는 저장하지 않는다.
func CheckpointDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Checkpoint는 저장된 체크포인트를 읽는다. 없으면 ok=false다. 버전·형식 오류는 tail 오류를 그대로
// 반환하므로 호출자는 빈 체크포인트로 처음부터 다시 읽는다.
func (q *Queue) Checkpoint(ctx context.Context, key string) (cp tail.Checkpoint, ok bool, err error) {
	if err := q.lock(); err != nil {
		return cp, false, err
	}
	defer q.mu.Unlock()
	var data []byte
	err = q.db.QueryRowContext(ctx, `SELECT data FROM checkpoints WHERE key_digest = ?`, CheckpointDigest(key)).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return cp, false, nil
	}
	if err != nil {
		return cp, false, fmt.Errorf("queue: read checkpoint: %w", err)
	}
	cp, err = tail.UnmarshalCheckpoint(data)
	return cp, err == nil, err
}

// Item은 전송 대기 이벤트 하나다. Payload는 큐 첫 기록 때의 이벤트 v1 JSON 그대로다.
type Item struct {
	EventID  string
	Payload  json.RawMessage
	Attempts int
}

// Peek은 가장 오래된 순서로 최대 limit개를 반환한다. 지우지 않는다.
func (q *Queue) Peek(ctx context.Context, limit int) ([]Item, error) {
	if err := q.lock(); err != nil {
		return nil, err
	}
	defer q.mu.Unlock()
	rows, err := q.db.QueryContext(ctx, `SELECT event_id, payload, attempts FROM events ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("queue: peek: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var payload []byte
		if err := rows.Scan(&it.EventID, &payload, &it.Attempts); err != nil {
			return nil, fmt.Errorf("queue: peek: %w", err)
		}
		it.Payload = payload
		out = append(out, it)
	}
	return out, rows.Err()
}

// Ack은 서버가 영속 ACK(accepted·duplicate)한 이벤트를 지운다. 지운 수를 반환한다.
func (q *Queue) Ack(ctx context.Context, eventIDs []string) (int, error) {
	if len(eventIDs) == 0 {
		return 0, nil
	}
	if err := q.lock(); err != nil {
		return 0, err
	}
	defer q.mu.Unlock()
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("queue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var total int64
	for _, id := range eventIDs {
		n, err := execN(ctx, tx, `DELETE FROM events WHERE event_id = ?`, id)
		if err != nil {
			return 0, fmt.Errorf("queue: ack: %w", err)
		}
		total += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("queue: commit: %w", err)
	}
	q.depth -= int(total)
	return int(total), nil
}

// Rejection은 서버가 거부한 이벤트 하나다.
type Rejection struct {
	EventID   string
	Code      string
	Retryable bool
}

// RejectResult는 Reject 결과 개수다.
type RejectResult struct {
	DeadLettered int
	Retrying     int
}

// Reject는 거부를 반영한다. retryable이 아니면 행을 지우고 dead-letter 개수만 코드별로 올린다
// (이벤트 내용은 남기지 않음). retryable이면 시도 수를 올리고, MaxAttempts에 닿으면 dead-letter다.
// v1 enum 밖 코드는 원문 대신 unknown_code로 센다.
func (q *Queue) Reject(ctx context.Context, rejs []Rejection) (RejectResult, error) {
	var res RejectResult
	if len(rejs) == 0 {
		return res, nil
	}
	if err := q.lock(); err != nil {
		return res, err
	}
	defer q.mu.Unlock()
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("queue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	removed := 0
	for _, r := range rejs {
		reason := r.Code
		if !rejectCodes[reason] && reason != ReasonInvalidRequest && reason != ReasonCorrupt {
			reason = ReasonUnknownCode
		}
		dead := !r.Retryable
		if r.Retryable {
			var attempts int
			err := tx.QueryRowContext(ctx, `UPDATE events SET attempts = attempts + 1 WHERE event_id = ? RETURNING attempts`, r.EventID).Scan(&attempts)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return RejectResult{}, fmt.Errorf("queue: reject: %w", err)
			}
			if attempts >= q.opts.MaxAttempts {
				dead, reason = true, ReasonMaxAttempts
			} else {
				res.Retrying++
			}
		}
		if !dead {
			continue
		}
		n, err := execN(ctx, tx, `DELETE FROM events WHERE event_id = ?`, r.EventID)
		if err != nil {
			return RejectResult{}, fmt.Errorf("queue: dead-letter: %w", err)
		}
		if n == 0 {
			continue
		}
		if err := addCounter(ctx, tx, counterDeadLetterPrefix+reason, n); err != nil {
			return RejectResult{}, err
		}
		removed += int(n)
		res.DeadLettered += int(n)
	}
	if err := tx.Commit(); err != nil {
		return RejectResult{}, fmt.Errorf("queue: commit: %w", err)
	}
	q.depth -= removed
	return res, nil
}

// Depth는 전송 대기 행 수다.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.depth
}

const (
	counterDroppedOldest    = "dropped_oldest"
	counterDeadLetterPrefix = "dead_letter:"
)

// Stats는 영속 개수다. 이벤트 내용·식별자 원문은 없다.
type Stats struct {
	Depth         int
	DroppedOldest int64
	DeadLetter    int64
	// DeadLetterByReason은 거부 코드(v1 enum) 또는 Reason* 상수별 개수다.
	DeadLetterByReason map[string]int64
}

// Stats는 현재 개수를 읽는다.
func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	if err := q.lock(); err != nil {
		return Stats{}, err
	}
	defer q.mu.Unlock()
	st := Stats{Depth: q.depth, DeadLetterByReason: map[string]int64{}}
	rows, err := q.db.QueryContext(ctx, `SELECT name, value FROM counters`)
	if err != nil {
		return Stats{}, fmt.Errorf("queue: stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var v int64
		if err := rows.Scan(&name, &v); err != nil {
			return Stats{}, fmt.Errorf("queue: stats: %w", err)
		}
		switch {
		case name == counterDroppedOldest:
			st.DroppedOldest = v
		case len(name) > len(counterDeadLetterPrefix) && name[:len(counterDeadLetterPrefix)] == counterDeadLetterPrefix:
			st.DeadLetterByReason[name[len(counterDeadLetterPrefix):]] = v
			st.DeadLetter += v
		}
	}
	return st, rows.Err()
}

func addCounter(ctx context.Context, tx *sql.Tx, name string, n int64) error {
	if n == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO counters(name, value) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET value = value + excluded.value`, name, n)
	if err != nil {
		return fmt.Errorf("queue: counter: %w", err)
	}
	return nil
}

func execN(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
