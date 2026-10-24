package runner

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// SnapshotVersion은 status.json 형식 버전이다.
const SnapshotVersion = 1

// 실행 상태 값.
const (
	StateStarting      = "starting"
	StateRunning       = "running"
	StatePaused        = "paused"
	StateDeviceRevoked = "device_revoked"
	StateNotRegistered = "not_registered"
	StateConfigInvalid = "config_invalid"
	StateStopped       = "stopped"
	// StateTokenUnavailable은 OS 보안 저장소에서 장치 토큰을 읽지 못해 기다리는 상태다(#41, 잠김·D-Bus 없음).
	StateTokenUnavailable = "token_unavailable"
)

// 오류 코드. 고정 enum만 쓰고 경로·URL·서버 응답 본문·OS 오류 문자열은 남기지 않는다.
const (
	ErrCodeDeviceRevoked        = "device_revoked"
	ErrCodeHTTP4xx              = "http_4xx"
	ErrCodeHTTP429              = "http_429"
	ErrCodeHTTP5xx              = "http_5xx"
	ErrCodeHTTPRedirect         = "http_redirect"
	ErrCodeNetwork              = "network"
	ErrCodeTimeout              = "timeout"
	ErrCodeQueue                = "queue_error"
	ErrCodeWatchRootUnavailable = "watch_root_unavailable"
	ErrCodeTailPermission       = "tail_permission_denied"
	ErrCodeTailUnsupported      = "tail_unsupported"
	ErrCodeTailError            = "tail_error"
	ErrCodeSpoolInvalid         = "spool_invalid"
	ErrCodeSpoolError           = "spool_error"
	ErrCodePauseStateInvalid    = "pause_state_invalid"
	ErrCodeStatusWrite          = "status_write_failed"
)

// ErrorStat은 오류 코드별 개수와 마지막 시각이다.
type ErrorStat struct {
	Code   string    `json:"code"`
	Count  int64     `json:"count"`
	LastAt time.Time `json:"last_at"`
}

// Snapshot은 실행 중인 `run`이 상태 디렉터리에 주기적으로 쓰는 진단 정보다(status.json).
// 개수·enum·시각만 담는다. 경로, 서버 주소, 토큰, 세션 ID, 이벤트 내용은 담지 않는다.
type Snapshot struct {
	SchemaVersion     int              `json:"schema_version"`
	CollectorVersion  string           `json:"collector_version"`
	State             string           `json:"state"`
	StartedAt         time.Time        `json:"started_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	WatchRoot         string           `json:"watch_root"`
	WatchedFiles      int              `json:"watched_files"`
	QueueDepth        int              `json:"queue_depth"`
	Counters          map[string]int64 `json:"counters"`
	Errors            []ErrorStat      `json:"errors"`
	LastSendOKAt      *time.Time       `json:"last_send_ok_at,omitempty"`
	LastHeartbeatOKAt *time.Time       `json:"last_heartbeat_ok_at,omitempty"`
}

// ReadSnapshot은 status.json을 읽는다.
func ReadSnapshot(path string) (Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return Snapshot{}, errors.New("runner: status unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return Snapshot{}, errors.New("runner: status unavailable")
	}
	var s Snapshot
	if json.Unmarshal(data, &s) != nil || s.SchemaVersion != SnapshotVersion {
		return Snapshot{}, errors.New("runner: status malformed")
	}
	return s, nil
}

// metrics는 고루틴 사이에서 공유하는 개수다.
type metrics struct {
	mu        sync.Mutex
	now       func() time.Time
	counters  map[string]int64
	errors    map[string]*ErrorStat
	lastSend  time.Time
	lastHB    time.Time
	watchRoot string
	state     string
	logf      func(string, ...any)
}

func newMetrics(now func() time.Time, logf func(string, ...any)) *metrics {
	return &metrics{now: now, logf: logf, counters: map[string]int64{}, errors: map[string]*ErrorStat{}, watchRoot: "unknown", state: StateStarting}
}

func (m *metrics) add(name string, n int64) {
	if n == 0 {
		return
	}
	m.mu.Lock()
	m.counters[name] += n
	m.mu.Unlock()
}

func (m *metrics) fail(code string) {
	m.mu.Lock()
	e := m.errors[code]
	if e == nil {
		e = &ErrorStat{Code: code}
		m.errors[code] = e
	}
	e.Count++
	e.LastAt = m.now().UTC()
	first := e.Count == 1
	m.mu.Unlock()
	if first {
		m.logf("오류 코드 %s 발생(이후 같은 코드는 status/diagnose 개수로만 집계)", code)
	}
}

func (m *metrics) sendOK() {
	m.mu.Lock()
	m.lastSend = m.now().UTC()
	m.mu.Unlock()
}

func (m *metrics) heartbeatOK() {
	m.mu.Lock()
	m.lastHB = m.now().UTC()
	m.mu.Unlock()
}

func (m *metrics) setState(s string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := m.state != s
	m.state = s
	return changed
}

func (m *metrics) setWatchRoot(s string) {
	m.mu.Lock()
	m.watchRoot = s
	m.mu.Unlock()
}

func (m *metrics) snapshot(version string, started time.Time) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{
		SchemaVersion: SnapshotVersion, CollectorVersion: version, State: m.state,
		StartedAt: started.UTC(), UpdatedAt: m.now().UTC(), WatchRoot: m.watchRoot,
		Counters: make(map[string]int64, len(m.counters)), Errors: []ErrorStat{},
	}
	for k, v := range m.counters {
		s.Counters[k] = v
	}
	for _, e := range m.errors {
		s.Errors = append(s.Errors, *e)
	}
	sort.Slice(s.Errors, func(i, j int) bool { return s.Errors[i].Code < s.Errors[j].Code })
	if !m.lastSend.IsZero() {
		t := m.lastSend
		s.LastSendOKAt = &t
	}
	if !m.lastHB.IsZero() {
		t := m.lastHB
		s.LastHeartbeatOKAt = &t
	}
	return s
}

// observingTransport는 HTTP 결과를 오류 코드로만 집계한다. URL·본문·헤더는 보지 않는다.
type observingTransport struct {
	base http.RoundTripper
	m    *metrics
}

func (t observingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if req.Context().Err() == nil || errors.Is(req.Context().Err(), errDeadline) {
			var te interface{ Timeout() bool }
			if errors.As(err, &te) && te.Timeout() {
				t.m.fail(ErrCodeTimeout)
			} else {
				t.m.fail(ErrCodeNetwork)
			}
		}
		return resp, err
	}
	heartbeat := strings.HasSuffix(req.URL.Path, "/devices/heartbeat")
	switch s := resp.StatusCode; {
	case s >= 200 && s < 300:
		if heartbeat {
			t.m.heartbeatOK()
		}
	case s == http.StatusUnauthorized || s == http.StatusForbidden:
		t.m.fail(ErrCodeDeviceRevoked)
	case s == http.StatusTooManyRequests:
		t.m.fail(ErrCodeHTTP429)
	case s >= 500:
		t.m.fail(ErrCodeHTTP5xx)
	case s >= 300 && s < 400:
		t.m.fail(ErrCodeHTTPRedirect)
	default:
		t.m.fail(ErrCodeHTTP4xx)
	}
	return resp, nil
}
