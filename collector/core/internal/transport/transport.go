// Package transport는 로컬 큐의 이벤트를 서버 `POST /api/v1/events/batch`로 보내는 배치 전송기다(#33).
//
// 규칙(docs/api/event-ingestion-v1.md):
//   - 한 요청은 최대 500건, 본문 1MiB 이하. 큐 첫 기록 때의 JSON을 그대로 보내 event_id가 바뀌지 않는다.
//   - 응답의 accepted·duplicate만 큐에서 지운다. HTTP 오류·연결 끊김·형식이 틀린 응답은 아무것도 지우지 않는다.
//   - rejected는 retryable 값을 따른다(알 수 없는 코드도 retryable만 본다).
//   - 5xx·429·네트워크 오류는 full jitter 지수 백오프, 429·503의 Retry-After를 하한으로 지킨다.
//   - 401·403은 장치 폐기·토큰 만료로 보고 ErrDeviceRevoked로 멈춘다(프로세스는 죽지 않음).
//
// 오류 값·로그에는 HTTP 상태 코드와 개수만 담고 요청·응답 본문, 토큰, URL을 넣지 않는다.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/queue"
)

// 서버 계약 한도와 기본값.
const (
	MaxBatchEvents        = 500
	MaxBodyBytes          = 1 << 20
	DefaultRequestTimeout = 30 * time.Second
	DefaultBackoffBase    = time.Second
	DefaultBackoffMax     = 5 * time.Minute
	// MaxRetryAfter는 서버 Retry-After를 따르는 최대 대기다. 넘는 값은 이 값으로 자른다.
	MaxRetryAfter = time.Hour
	// DefaultIdleInterval은 큐가 비었을 때 다시 확인하는 주기다(Notify로 더 빨리 깨울 수 있음).
	DefaultIdleInterval = 2 * time.Second
	// maxResponseBytes는 읽을 응답 본문 상한이다.
	maxResponseBytes = 1 << 20
)

const batchPath = "/api/v1/events/batch"
const heartbeatPath = "/api/v1/devices/heartbeat"

var (
	// ErrDeviceRevoked는 401·403 응답이다. 재등록 전까지 전송을 멈춘다. 큐는 그대로 남는다.
	ErrDeviceRevoked = errors.New("transport: device credential rejected")
	// ErrBadResponse는 200 응답 본문이 계약 형식이 아닐 때다. 아무것도 ACK하지 않는다.
	ErrBadResponse = errors.New("transport: malformed batch response")
)

// HTTPError는 재시도할 HTTP 실패다. 본문은 담지 않는다.
type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("transport: http status %d", e.Status) }

// NetworkError는 연결·타임아웃 실패다. 원인 문자열(URL 포함 가능)은 노출하지 않는다.
type NetworkError struct{ timeout bool }

func (e *NetworkError) Error() string {
	if e.timeout {
		return "transport: request timed out"
	}
	return "transport: request failed"
}

// Store는 전송기가 쓰는 큐 연산이다. *queue.Queue가 구현한다.
type Store interface {
	Peek(ctx context.Context, limit int) ([]queue.Item, error)
	Ack(ctx context.Context, eventIDs []string) (int, error)
	Reject(ctx context.Context, rejs []queue.Rejection) (queue.RejectResult, error)
	Depth() int
}

// Clock은 시간 주입 지점이다.
type Clock interface {
	// Sleep은 d만큼 기다리거나 wake·ctx가 먼저 오면 돌아온다.
	Sleep(ctx context.Context, d time.Duration, wake <-chan struct{}) error
}

type realClock struct{}

func (realClock) Sleep(ctx context.Context, d time.Duration, wake <-chan struct{}) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
		return nil
	case <-t.C:
		return nil
	}
}

// Config는 전송기 설정이다. 0 값은 기본값이다.
type Config struct {
	// BaseURL은 서버 origin이다(예: https://office.example). 끝의 `/`는 무시한다.
	BaseURL string
	// Token은 장치 토큰을 돌려준다(회전 반영). 빈 값이면 ErrDeviceRevoked다.
	Token func() string
	// Client가 nil이면 리다이렉트를 따르지 않는 기본 클라이언트를 쓴다.
	Client *http.Client
	Clock  Clock
	// Rand는 [0,1) 난수다(full jitter). nil이면 math/rand/v2.
	Rand             func() float64
	MaxBatch         int
	MaxBody          int
	RequestTimeout   time.Duration
	BackoffBase      time.Duration
	BackoffMax       time.Duration
	IdleInterval     time.Duration
	CollectorVersion string
}

// Sender는 배치 전송기다. SendOnce·Run은 한 고루틴에서 호출한다. Notify는 아무 고루틴에서나 된다.
type Sender struct {
	cfg   Config
	store Store
	wake  chan struct{}

	mu       sync.Mutex
	batchCap int // 422·413 뒤 줄인 배치 크기. 성공하면 다시 늘린다.
	failures int
}

// New는 전송기를 만든다.
func New(cfg Config, store Store) (*Sender, error) {
	if cfg.BaseURL == "" || cfg.Token == nil || store == nil {
		return nil, errors.New("transport: base url, token source and store are required")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Client == nil {
		// 리다이렉트를 따라가지 않는다(토큰이 다른 곳으로 가거나 POST가 GET으로 바뀌지 않게).
		cfg.Client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Float64
	}
	if cfg.MaxBatch <= 0 || cfg.MaxBatch > MaxBatchEvents {
		cfg.MaxBatch = MaxBatchEvents
	}
	if cfg.MaxBody <= 0 || cfg.MaxBody > MaxBodyBytes {
		cfg.MaxBody = MaxBodyBytes
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = DefaultBackoffBase
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = DefaultBackoffMax
	}
	if cfg.IdleInterval <= 0 {
		cfg.IdleInterval = DefaultIdleInterval
	}
	return &Sender{cfg: cfg, store: store, wake: make(chan struct{}, 1), batchCap: cfg.MaxBatch}, nil
}

// Notify는 새 이벤트가 들어왔음을 알려 유휴 대기를 깨운다(백오프 대기는 깨우지 않는다).
func (s *Sender) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Result는 SendOnce 한 번의 개수다.
type Result struct {
	Sent, Accepted, Duplicate, Rejected, DeadLettered int
}

// SendOnce는 큐 앞부분을 한 배치로 보낸다. 큐가 비었으면 Sent=0, nil이다.
func (s *Sender) SendOnce(ctx context.Context) (Result, error) {
	var res Result
	items, err := s.store.Peek(ctx, s.cap())
	if err != nil || len(items) == 0 {
		return res, err
	}
	items, body := s.buildBody(items)
	if len(items) == 0 {
		return res, nil
	}
	res.Sent = len(items)

	status, header, respBody, err := s.post(ctx, batchPath, body)
	if err != nil {
		return res, err
	}
	switch {
	case status == http.StatusOK:
		return s.applyResponse(ctx, res, items, respBody)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return res, ErrDeviceRevoked
	case status == http.StatusUnprocessableEntity || status == http.StatusRequestEntityTooLarge:
		// 요청 전체 거부. 배치를 반으로 줄여 원인 이벤트를 좁히고, 1건이면 dead-letter한다.
		if len(items) == 1 {
			rr, err := s.store.Reject(ctx, []queue.Rejection{{EventID: items[0].EventID, Code: queue.ReasonInvalidRequest}})
			res.Rejected, res.DeadLettered = 1, rr.DeadLettered
			return res, err
		}
		s.setCap(max(1, len(items)/2))
		return res, &HTTPError{Status: status}
	default:
		return res, &HTTPError{Status: status, RetryAfter: retryAfter(header.Get("Retry-After"), time.Now())}
	}
}

func (s *Sender) cap() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.batchCap
}

func (s *Sender) setCap(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCap = n
}

// buildBody는 본문 한도 안에 들어가는 앞부분만 담는다. 한 건만으로도 넘는 행은(정상 이벤트는 1KiB
// 미만이라 손상 외에는 없음) 보낼 수 없으므로 dead-letter 대상으로 1건짜리 배치로 둔다.
func (s *Sender) buildBody(items []queue.Item) ([]queue.Item, []byte) {
	const head = `{"schema_version":"1.0","events":[`
	const tail = `]}`
	var buf bytes.Buffer
	buf.WriteString(head)
	n := 0
	for i, it := range items {
		extra := len(it.Payload)
		if i > 0 {
			extra++
		}
		if buf.Len()+extra+len(tail) > s.cfg.MaxBody && n > 0 {
			break
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(it.Payload)
		n++
	}
	buf.WriteString(tail)
	return items[:n], buf.Bytes()
}

type batchResponse struct {
	SchemaVersion string   `json:"schema_version"`
	Accepted      []string `json:"accepted_event_ids"`
	Duplicate     []string `json:"duplicate_event_ids"`
	Rejected      []struct {
		EventID   string `json:"event_id"`
		Code      string `json:"code"`
		Retryable *bool  `json:"retryable"`
	} `json:"rejected_events"`
}

func (s *Sender) applyResponse(ctx context.Context, res Result, items []queue.Item, body []byte) (Result, error) {
	var br batchResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields() // 응답 스키마는 additionalProperties: false다.
	if err := dec.Decode(&br); err != nil || br.SchemaVersion != "1.0" || br.Accepted == nil || br.Duplicate == nil || br.Rejected == nil {
		return res, ErrBadResponse
	}
	sent := make(map[string]bool, len(items))
	for _, it := range items {
		sent[it.EventID] = true
	}
	// 보내지 않은 ID를 ACK하면 다른 이벤트를 잃을 수 있으므로 응답 전체를 신뢰하지 않는다.
	seen := map[string]bool{}
	check := func(id string) bool {
		if !sent[id] || seen[id] {
			return false
		}
		seen[id] = true
		return true
	}
	var ack []string
	for _, id := range append(append([]string{}, br.Accepted...), br.Duplicate...) {
		if !check(id) {
			return res, ErrBadResponse
		}
		ack = append(ack, id)
	}
	var rejs []queue.Rejection
	for _, r := range br.Rejected {
		if !check(r.EventID) || r.Retryable == nil {
			return res, ErrBadResponse
		}
		rejs = append(rejs, queue.Rejection{EventID: r.EventID, Code: r.Code, Retryable: *r.Retryable})
	}
	if _, err := s.store.Ack(ctx, ack); err != nil {
		return res, err
	}
	res.Accepted, res.Duplicate, res.Rejected = len(br.Accepted), len(br.Duplicate), len(rejs)
	rr, err := s.store.Reject(ctx, rejs)
	res.DeadLettered = rr.DeadLettered
	if err != nil {
		return res, err
	}
	if s.cap() < s.cfg.MaxBatch {
		s.setCap(min(s.cfg.MaxBatch, s.cap()*2))
	}
	return res, nil
}

func (s *Sender) post(ctx context.Context, path string, body []byte) (int, http.Header, []byte, error) {
	token := s.cfg.Token()
	if token == "" {
		return 0, nil, nil, ErrDeviceRevoked
	}
	rctx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, s.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, errors.New("transport: invalid base url")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.ContentLength = int64(len(body))
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		var ne interface{ Timeout() bool }
		return 0, nil, nil, &NetworkError{timeout: errors.As(err, &ne) && ne.Timeout()}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, &NetworkError{timeout: rctx.Err() != nil}
	}
	return resp.StatusCode, resp.Header, data, nil
}

// retryAfter는 초 단위 또는 HTTP-date 값을 해석한다. 해석할 수 없으면 0이다.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > int64(MaxRetryAfter/time.Second) {
			return MaxRetryAfter
		}
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	if d < 0 {
		return 0
	}
	return min(d, MaxRetryAfter)
}

// Backoff는 n번째(1부터) 연속 실패 뒤 대기 시간이다. full jitter: [0, min(max, base·2^(n-1))) 균등,
// 서버 Retry-After가 있으면 그 값이 하한이다.
func Backoff(n int, base, maxDelay time.Duration, rnd float64, retryAfter time.Duration) time.Duration {
	if n < 1 {
		n = 1
	}
	ceiling := float64(maxDelay)
	if exp := float64(base) * math.Pow(2, float64(n-1)); exp < ceiling {
		ceiling = exp
	}
	d := time.Duration(rnd * ceiling)
	if d < retryAfter {
		d = retryAfter
	}
	return d
}

// Run은 ctx가 끝나거나 장치 자격증명이 거부될 때까지 큐를 비운다. 반환값은 ctx.Err() 또는
// ErrDeviceRevoked다. 그 밖의 오류는 백오프로 흡수한다.
func (s *Sender) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := s.SendOnce(ctx)
		switch {
		case errors.Is(err, ErrDeviceRevoked):
			return ErrDeviceRevoked
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			s.failures++
			var ra time.Duration
			var he *HTTPError
			if errors.As(err, &he) {
				ra = he.RetryAfter
			}
			d := Backoff(s.failures, s.cfg.BackoffBase, s.cfg.BackoffMax, s.cfg.Rand(), ra)
			if err := s.cfg.Clock.Sleep(ctx, d, nil); err != nil {
				return err
			}
		case res.Sent == 0:
			s.failures = 0
			if err := s.cfg.Clock.Sleep(ctx, s.cfg.IdleInterval, s.wake); err != nil {
				return err
			}
		default:
			s.failures = 0
		}
	}
}

// Heartbeat는 `POST /api/v1/devices/heartbeat`에 버전과 큐 깊이를 보낸다(204 기대).
func (s *Sender) Heartbeat(ctx context.Context) error {
	payload := map[string]any{"queue_depth": min(s.store.Depth(), 10_000_000)}
	if s.cfg.CollectorVersion != "" {
		payload["collector_version"] = s.cfg.CollectorVersion
	}
	body, _ := json.Marshal(payload)
	status, header, _, err := s.post(ctx, heartbeatPath, body)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusNoContent || status == http.StatusOK:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrDeviceRevoked
	default:
		return &HTTPError{Status: status, RetryAfter: retryAfter(header.Get("Retry-After"), time.Now())}
	}
}
