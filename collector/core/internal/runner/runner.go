// Package runner는 headless Collector 실행 루프다(#34).
//
// 한 프로세스(`agent-office-collector run`)가 다음을 함께 돌린다.
//
//   - 수집 고루틴(이 패키지의 collectLoop): Claude transcript 탐색·tail, Hook spool 처리,
//     정규화(normalize), 로컬 큐 기록(queue, 체크포인트와 같은 트랜잭션), 하루 1회 PruneSeen
//   - 전송 고루틴: transport.Sender.Run. 일시중지·장치 폐기(401/403) 때 멈춘다
//   - heartbeat 고루틴: 주기적으로 `POST /api/v1/devices/heartbeat`
//
// 진단 정보는 고정 코드와 개수만 status.json에 남긴다. 원본 로그는 읽기 전용으로만 연다.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/config"
	"github.com/apg0001/Agent-Office/collector/core/internal/fsutil"
	"github.com/apg0001/Agent-Office/collector/core/internal/instance"
	"github.com/apg0001/Agent-Office/collector/core/internal/normalize"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/queue"
	"github.com/apg0001/Agent-Office/collector/core/internal/secret"
	"github.com/apg0001/Agent-Office/collector/core/internal/spool"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"
	"github.com/apg0001/Agent-Office/collector/core/internal/transport"
)

// 기본 주기.
const (
	DefaultPollInterval      = 2 * time.Second
	DefaultDiscoverInterval  = 10 * time.Second
	DefaultHeartbeatInterval = 60 * time.Second
	DefaultStatusInterval    = 15 * time.Second
	DefaultPruneInterval     = 24 * time.Hour
	// SeenRetention은 결정적 event_id 보관 기간이다. 서버 보존 기간(30일)보다 길게 둔다.
	SeenRetention = 35 * 24 * time.Hour
	// idleCloseAfter가 지나도록 바뀌지 않은 transcript는 파일 핸들을 닫는다(열린 핸들 수 제한).
	idleCloseAfter  = 2 * time.Minute
	maxPollsPerTick = 64
	maxSpoolPerTick = 500
	retryAfterError = 30 * time.Second
	// 읽기 권한이 없는 transcript는 2초마다 다시 열지 않고 간격을 두 배씩 늘린다(최대 10분, #184).
	deniedBackoffMin = 30 * time.Second
	deniedBackoffMax = 10 * time.Minute
)

var (
	// ErrAlreadyRunning은 같은 사용자의 다른 `run` 프로세스가 잠금을 갖고 있을 때다.
	ErrAlreadyRunning = errors.New("runner: another collector instance is running")
	errReload         = errors.New("runner: configuration changed")
	errDeadline       = context.DeadlineExceeded
)

// Options는 실행 설정이다. 0 값은 기본값이다.
type Options struct {
	Dirs              paths.Dirs
	Version           string
	GOOS              string
	PollInterval      time.Duration
	DiscoverInterval  time.Duration
	HeartbeatInterval time.Duration
	StatusInterval    time.Duration
	PruneInterval     time.Duration
	// Transport는 HTTP 전송 주입 지점(테스트)이다. nil이면 http.DefaultTransport 복제본.
	Transport http.RoundTripper
	// TuneSender는 전송기 설정 조정(테스트에서 짧은 백오프)이다.
	TuneSender func(*transport.Config)
	Now        func() time.Time
	// Logf는 운영 로그다. 코드·개수만 넘긴다.
	Logf func(format string, args ...any)
	// Secrets는 장치 토큰 보안 저장소다(#41). nil이면 파일 보관 토큰만 쓰고 이전(migration)하지 않는다.
	Secrets secret.Store
}

func (o *Options) defaults() {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	if o.DiscoverInterval <= 0 {
		o.DiscoverInterval = DefaultDiscoverInterval
	}
	if o.HeartbeatInterval <= 0 {
		o.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if o.StatusInterval <= 0 {
		o.StatusInterval = DefaultStatusInterval
	}
	if o.PruneInterval <= 0 {
		o.PruneInterval = DefaultPruneInterval
	}
	if o.Transport == nil {
		o.Transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
}

type runner struct {
	o       Options
	m       *metrics
	started time.Time
	// 마지막 세션의 큐 깊이·감시 파일 수(상태 기록용).
	depth   atomic.Int64
	watched atomic.Int64
	pipe    atomic.Pointer[normalize.Pipeline]
	lastSt  time.Time
}

// Run은 ctx가 끝날 때까지 실행한다. SIGINT/SIGTERM으로 ctx가 끝나면 nil을 반환한다.
// 등록 전이면 등록될 때까지 기다리고, 등록 정보가 바뀌면 새 장치로 다시 시작한다.
func Run(ctx context.Context, o Options) error {
	o.defaults()
	lock, err := instance.Acquire(o.Dirs.LockFile())
	if errors.Is(err, instance.ErrLocked) {
		return ErrAlreadyRunning
	}
	if err != nil {
		return err
	}
	defer lock.Release()

	r := &runner{o: o, m: newMetrics(o.Now, o.Logf), started: o.Now()}
	o.Logf("Collector %s 시작", o.Version)
	defer func() {
		r.m.setState(StateStopped)
		r.writeStatus(true)
		o.Logf("Collector 종료")
	}()
	for {
		cfg, err := config.Load(o.Dirs.ConfigFile())
		if err != nil {
			state := StateConfigInvalid
			if errors.Is(err, config.ErrNotRegistered) {
				state = StateNotRegistered
			}
			changed := r.m.setState(state)
			if changed {
				o.Logf("상태 %s: `agent-office-collector register`로 등록하면 자동으로 수집을 시작합니다", state)
			}
			r.writeStatus(changed)
			if !sleep(ctx, o.PollInterval) {
				return nil
			}
			continue
		}
		// 파일에 있는 토큰(#41 이전 형식·대체 경로)을 보안 저장소로 옮긴다. 실패하면 파일 보관을 유지한다.
		if migrated, ok := config.MigrateToken(o.Dirs.ConfigFile(), cfg, o.Secrets); ok {
			cfg = migrated
			o.Logf("장치 토큰을 OS 보안 저장소로 옮기고 설정 파일에서 지웠습니다")
		}
		token, err := cfg.Token(o.Secrets)
		if err != nil {
			changed := r.m.setState(StateTokenUnavailable)
			if changed {
				o.Logf("상태 %s: OS 보안 저장소에서 장치 토큰을 읽지 못했습니다. 저장소 잠금을 해제하면 자동으로 다시 시도합니다", StateTokenUnavailable)
			}
			r.writeStatus(changed)
			if !sleep(ctx, o.DiscoverInterval) {
				return nil
			}
			continue
		}
		err = r.session(ctx, cfg, token)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errReload) {
			o.Logf("등록 정보 변경을 감지해 다시 시작합니다")
			continue
		}
		if err != nil {
			r.m.fail(ErrCodeQueue)
			r.writeStatus(true)
			if !sleep(ctx, retryAfterError) {
				return nil
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// control은 수집·전송·heartbeat 고루틴이 공유하는 상태다.
type control struct {
	paused  atomic.Bool
	revoked atomic.Bool
	mu      sync.Mutex
	cancel  context.CancelFunc
}

func (c *control) setSendCancel(f context.CancelFunc) {
	c.mu.Lock()
	c.cancel = f
	c.mu.Unlock()
}

func (c *control) stopSending() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
}

// countingStore는 큐 ACK·거부 개수를 집계한다.
type countingStore struct {
	*queue.Queue
	m *metrics
}

func (s countingStore) Ack(ctx context.Context, ids []string) (int, error) {
	n, err := s.Queue.Ack(ctx, ids)
	if err == nil {
		s.m.add("acked", int64(n))
		s.m.sendOK()
	}
	return n, err
}

func (s countingStore) Reject(ctx context.Context, rejs []queue.Rejection) (queue.RejectResult, error) {
	r, err := s.Queue.Reject(ctx, rejs)
	if err == nil {
		s.m.add("rejected", int64(len(rejs)))
		s.m.add("dead_lettered", int64(r.DeadLettered))
	}
	return r, err
}

func (r *runner) session(ctx context.Context, cfg config.Config, tok config.Secret) error {
	q, err := queue.Open(r.o.Dirs.QueueFile(), queue.Options{})
	if err != nil {
		return err
	}
	defer q.Close()
	seen, err := q.SeenEventIDs(ctx)
	if err != nil {
		return err
	}
	norm, err := normalize.New(normalize.Options{DeviceID: cfg.DeviceID, Sequences: q, SeenEventIDs: seen})
	if err != nil {
		return err
	}
	pipe := normalize.NewPipeline(norm)
	r.pipe.Store(pipe)

	token := string(tok)
	client := &http.Client{
		Transport:     observingTransport{base: r.o.Transport, m: r.m},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	tcfg := transport.Config{BaseURL: cfg.ServerURL, Token: func() string { return token }, Client: client, CollectorVersion: r.o.Version}
	if r.o.TuneSender != nil {
		r.o.TuneSender(&tcfg)
	}
	sender, err := transport.New(tcfg, countingStore{Queue: q, m: r.m})
	if err != nil {
		return err
	}

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctl := &control{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.sendLoop(sctx, sender, ctl) }()
	go func() { defer wg.Done(); r.heartbeatLoop(sctx, sender, ctl) }()
	c := &collector{r: r, cfg: cfg, q: q, pipe: pipe, sender: sender, ctl: ctl, files: map[string]*watched{}}
	err = c.loop(sctx)
	cancel()
	wg.Wait()
	c.closeAll()
	r.depth.Store(int64(q.Depth()))
	return err
}

func (r *runner) sendLoop(ctx context.Context, sender *transport.Sender, ctl *control) {
	for ctx.Err() == nil {
		if ctl.paused.Load() || ctl.revoked.Load() {
			if !sleep(ctx, r.o.PollInterval) {
				return
			}
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		ctl.setSendCancel(cancel)
		if ctl.paused.Load() {
			cancel()
			continue
		}
		err := sender.Run(rctx)
		ctl.setSendCancel(nil)
		cancel()
		if errors.Is(err, transport.ErrDeviceRevoked) && !ctl.revoked.Swap(true) {
			r.o.Logf("서버가 장치 자격증명을 거부했습니다(%s). 전송을 멈추고 큐는 보존합니다. `agent-office-collector register --force`로 다시 등록하세요", ErrCodeDeviceRevoked)
		}
	}
}

func (r *runner) heartbeatLoop(ctx context.Context, sender *transport.Sender, ctl *control) {
	t := time.NewTicker(r.o.HeartbeatInterval)
	defer t.Stop()
	for {
		if !ctl.paused.Load() && !ctl.revoked.Load() {
			if err := sender.Heartbeat(ctx); errors.Is(err, transport.ErrDeviceRevoked) && !ctl.revoked.Swap(true) {
				ctl.stopSending()
				r.o.Logf("heartbeat가 장치 자격증명 거부(%s)를 받았습니다. 다시 등록하세요", ErrCodeDeviceRevoked)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *runner) writeStatus(force bool) {
	now := r.o.Now()
	if !force && now.Sub(r.lastSt) < r.o.StatusInterval {
		return
	}
	r.lastSt = now
	s := r.m.snapshot(r.o.Version, r.started)
	s.QueueDepth = int(r.depth.Load())
	s.WatchedFiles = int(r.watched.Load())
	if p := r.pipe.Load(); p != nil {
		c := p.Counters()
		s.Counters["normalize_accepted"] = int64(c.Accepted)
		s.Counters["normalize_duplicate_suppressed"] = int64(c.DuplicateSuppressed)
		s.Counters["normalize_dropped_unknown"] = int64(c.DroppedUnknown)
		s.Counters["normalize_dropped_invalid"] = int64(c.DroppedInvalid)
		s.Counters["normalize_ignored_lines"] = int64(c.IgnoredLines)
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	if err := fsutil.WriteFileAtomic(r.o.Dirs.StatusFile(), append(data, '\n'), 0o600); err != nil {
		r.m.fail(ErrCodeStatusWrite)
	}
}

// watched는 tail 중인 transcript 하나다.
type watched struct {
	key       string
	path      string
	t         *tail.Tailer
	persisted tail.Checkpoint
	size      int64
	mtime     time.Time
	checked   bool
	// 권한 오류 뒤 다시 시도할 때까지 건너뛴다(#184).
	deniedUntil time.Time
	deniedWait  time.Duration
}

type collector struct {
	r      *runner
	cfg    config.Config
	q      *queue.Queue
	pipe   *normalize.Pipeline
	sender *transport.Sender
	ctl    *control
	files  map[string]*watched

	lastDiscover time.Time
	lastPrune    time.Time
	baselineDone bool
	// skipAll이면 다음 탐색에서 등록하는 모든 파일을 끝까지 건너뛴다(일시중지 해제 직후).
	skipAll     bool
	skipHandled time.Time
	pause       config.Pause
}

func (c *collector) loop(ctx context.Context) error {
	o := c.r.o
	_, err := os.Stat(o.Dirs.BaselineFile())
	c.baselineDone = err == nil
	tick := time.NewTicker(o.PollInterval)
	defer tick.Stop()
	for {
		if cfg, err := config.Load(o.Dirs.ConfigFile()); err != nil || !cfg.Same(c.cfg) {
			return errReload
		}
		pause, err := config.LoadPause(o.Dirs.PauseFile())
		if err != nil {
			c.r.m.fail(ErrCodePauseStateInvalid)
		}
		c.pause = pause
		if !pause.Paused && pause.SkipBacklog && !c.skipAll && !pause.ChangedAt.Equal(c.skipHandled) {
			// 일시중지 동안 쌓인 줄은 수집하지 않는다. 열린 파일을 놓고 다음 탐색에서 끝으로 이동한다.
			c.closeAll()
			c.skipAll = true
		}
		if pause.Paused != c.ctl.paused.Load() {
			c.ctl.paused.Store(pause.Paused)
			if pause.Paused {
				c.ctl.stopSending()
				c.closeAll()
				o.Logf("수집 일시중지: tail·Hook 처리·전송·heartbeat를 멈춥니다")
			} else {
				o.Logf("수집 재개")
				c.lastDiscover = time.Time{}
			}
		}
		state := StateRunning
		switch {
		case pause.Paused:
			state = StatePaused
		case c.ctl.revoked.Load():
			state = StateDeviceRevoked
		}
		changed := c.r.m.setState(state)
		if !pause.Paused {
			c.drainSpool(ctx)
			now := o.Now()
			if now.Sub(c.lastDiscover) >= o.DiscoverInterval {
				c.discover(ctx)
				c.lastDiscover = now
			}
			c.pollAll(ctx)
			if now.Sub(c.lastPrune) >= o.PruneInterval {
				if _, err := c.q.PruneSeen(ctx, now.Add(-SeenRetention)); err != nil && ctx.Err() == nil {
					c.r.m.fail(ErrCodeQueue)
				}
				c.lastPrune = now
			}
		}
		c.r.depth.Store(int64(c.q.Depth()))
		c.r.watched.Store(int64(len(c.files)))
		c.r.writeStatus(changed)
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func (c *collector) root() string {
	if c.cfg.ClaudeProjectsDir != "" {
		return c.cfg.ClaudeProjectsDir
	}
	return c.r.o.Dirs.ClaudeProjects
}

func (c *collector) runtime() adapter.Runtime {
	return adapter.Runtime{Product: "claude-code", Version: c.cfg.ClaudeVersion, OS: c.r.o.GOOS, Mode: c.cfg.ClaudeMode}
}

func (c *collector) markBaseline() {
	if c.baselineDone {
		return
	}
	data, _ := json.Marshal(map[string]any{"completed_at": c.r.o.Now().UTC()})
	if err := fsutil.WriteFileAtomic(c.r.o.Dirs.BaselineFile(), append(data, '\n'), 0o600); err != nil {
		c.r.m.fail(ErrCodeStatusWrite)
		return
	}
	c.baselineDone = true
}

// discover는 감시 루트를 다시 훑어 새 파일을 등록하고 사라진 파일을 놓는다.
//
// 최초 실행(baseline 표시 없음)에서는 이미 있는 transcript를 끝까지 건너뛰고 체크포인트만 남긴다.
// 오래된 줄에는 timestamp가 없을 수 있어 관측 시각으로 찍히면 지금 일어난 일처럼 보이기 때문이다.
// 그 뒤에 새로 생긴 파일은 처음부터 읽는다.
func (c *collector) discover(ctx context.Context) {
	root := c.root()
	d, err := tail.Discover(root, tail.DiscoverOptions{})
	if err != nil {
		if _, statErr := os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
			c.r.m.setWatchRoot("not_found")
			c.markBaseline()
		} else {
			c.r.m.setWatchRoot("unavailable")
			c.r.m.fail(ErrCodeWatchRootUnavailable)
		}
		return
	}
	c.r.m.setWatchRoot("available")
	seen := make(map[string]bool, len(d.Files))
	for _, f := range d.Files {
		key := "claude-transcript:" + f.Rel
		seen[key] = true
		if c.files[key] != nil {
			continue
		}
		cp, ok, err := c.q.Checkpoint(ctx, key)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// 읽을 수 없는 체크포인트는 처음부터 다시 읽는다. 중복은 결정적 event_id가 막는다.
			cp, ok = tail.Checkpoint{}, false
		}
		w := &watched{key: key, path: f.Path(root), persisted: cp}
		w.t = tail.New(w.path, cp, tail.Options{})
		c.files[key] = w
		if c.skipAll || (!ok && !c.baselineDone) {
			c.skipToEnd(ctx, w)
		}
	}
	for key, w := range c.files {
		if !seen[key] {
			_ = w.t.Close()
			delete(c.files, key)
		}
	}
	if ctx.Err() == nil {
		c.markBaseline()
		c.finishSkip()
	}
}

// finishSkip은 재개 후 건너뛰기를 마쳤음을 pause.json에 기록한다. 그 사이 사용자가 상태를
// 바꿨으면(ChangedAt 변경) 건드리지 않는다.
func (c *collector) finishSkip() {
	if !c.skipAll {
		return
	}
	c.skipAll = false
	c.skipHandled = c.pause.ChangedAt
	c.r.m.add("resume_backlog_skips", 1)
	path := c.r.o.Dirs.PauseFile()
	cur, err := config.LoadPause(path)
	if err != nil || cur.Paused || !cur.SkipBacklog || !cur.ChangedAt.Equal(c.pause.ChangedAt) {
		return
	}
	cur.SkipBacklog = false
	if err := config.WritePause(path, cur); err != nil {
		c.r.m.fail(ErrCodePauseStateInvalid)
	}
}

func (c *collector) skipToEnd(ctx context.Context, w *watched) {
	for i := 0; i < 1<<16; i++ {
		res := w.t.Poll()
		if res.Status != tail.StatusOK || !res.More {
			break
		}
	}
	cp := w.t.Checkpoint()
	if _, err := c.q.Enqueue(ctx, nil, &queue.CheckpointUpdate{Key: w.key, Checkpoint: cp}); err != nil {
		if ctx.Err() == nil {
			c.r.m.fail(ErrCodeQueue)
		}
		return
	}
	w.persisted = cp
	_ = w.t.Close()
	c.r.m.add("baseline_skipped_files", 1)
}

func (c *collector) pollAll(ctx context.Context) {
	for key, w := range c.files {
		if ctx.Err() != nil {
			return
		}
		if gone := c.poll(ctx, w); gone {
			_ = w.t.Close()
			delete(c.files, key)
		}
	}
}

// poll은 바뀐 파일만 읽는다. 파일이 사라졌으면 true다.
func (c *collector) poll(ctx context.Context, w *watched) bool {
	now := c.r.o.Now()
	if now.Before(w.deniedUntil) {
		return false
	}
	fi, statErr := os.Stat(w.path)
	if statErr == nil && w.checked && fi.Size() == w.size && fi.ModTime().Equal(w.mtime) {
		if now.Sub(w.mtime) > idleCloseAfter {
			_ = w.t.Close()
		}
		return false
	}
	for i := 0; i < maxPollsPerTick; i++ {
		res := w.t.Poll()
		c.r.m.add("tail_invalid_lines", int64(res.InvalidLines))
		c.r.m.add("tail_oversized_lines", int64(res.OversizedLines))
		if res.Reset != tail.ResetNone {
			c.r.m.add("tail_reset_"+string(res.Reset), 1)
		}
		recs := c.pipe.Transcript(res, c.runtime(), c.r.o.Now())
		if len(recs) > 0 || !sameCheckpoint(res.Checkpoint, w.persisted) {
			er, err := c.q.Enqueue(ctx, recs, &queue.CheckpointUpdate{Key: w.key, Checkpoint: res.Checkpoint})
			if err != nil {
				if ctx.Err() == nil {
					c.r.m.fail(ErrCodeQueue)
				}
				// 영속화된 위치에서 다시 시작한다.
				_ = w.t.Close()
				w.t = tail.New(w.path, w.persisted, tail.Options{})
				return false
			}
			w.persisted = res.Checkpoint
			c.r.m.add("enqueued_transcript", int64(er.Enqueued))
			c.r.m.add("queue_dropped_oldest", int64(er.DroppedOldest))
			if er.Enqueued > 0 {
				c.sender.Notify()
			}
		}
		switch res.Status {
		case tail.StatusOK:
			w.deniedWait = 0
		case tail.StatusMissing:
			return true
		case tail.StatusPermissionDenied:
			c.r.m.fail(ErrCodeTailPermission)
			if res.Denied != "" {
				c.r.m.add("tail_denied_"+res.Denied, 1)
			}
			w.deniedWait = nextDeniedWait(w.deniedWait)
			w.deniedUntil = now.Add(w.deniedWait)
			return false
		case tail.StatusUnsupported:
			c.r.m.fail(ErrCodeTailUnsupported)
			return false
		default:
			c.r.m.fail(ErrCodeTailError)
			return false
		}
		if !res.More {
			break
		}
	}
	if statErr == nil {
		w.size, w.mtime, w.checked = fi.Size(), fi.ModTime(), true
	}
	return false
}

// nextDeniedWait는 권한 오류가 이어질 때 다음 재시도까지의 간격이다(30초부터 두 배, 최대 10분).
func nextDeniedWait(prev time.Duration) time.Duration {
	return min(max(prev*2, deniedBackoffMin), deniedBackoffMax)
}

func sameCheckpoint(a, b tail.Checkpoint) bool {
	if (a.File == nil) != (b.File == nil) {
		return false
	}
	if a.File != nil && *a.File != *b.File {
		return false
	}
	return a.Offset == b.Offset && a.PrefixDigest == b.PrefixDigest && a.SkippingOversizedLine == b.SkippingOversizedLine
}

// drainSpool은 `hook` 명령이 남긴 후보를 정규화해 큐에 넣고 파일을 지운다.
func (c *collector) drainSpool(ctx context.Context) {
	dir := c.r.o.Dirs.SpoolDir()
	entries, err := spool.List(dir, maxSpoolPerTick, c.r.o.Now())
	if err != nil {
		c.r.m.fail(ErrCodeSpoolError)
		return
	}
	if len(entries) == 0 {
		return
	}
	var recs []normalize.Record
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		var cand adapter.Candidate
		if e.Err == nil {
			cand, err = e.Record.Candidate()
		}
		if e.Err != nil || err != nil {
			c.r.m.fail(ErrCodeSpoolInvalid)
			spool.Remove(dir, e.Name)
			continue
		}
		names = append(names, e.Name)
		recs = append(recs, c.pipe.Candidate(normalize.Input{Candidate: cand, ObservedAt: e.Record.ObservedAt})...)
	}
	er, err := c.q.Enqueue(ctx, recs, nil)
	if err != nil {
		if ctx.Err() == nil {
			c.r.m.fail(ErrCodeQueue)
		}
		return
	}
	for _, n := range names {
		spool.Remove(dir, n)
	}
	c.r.m.add("hook_spool_processed", int64(len(names)))
	c.r.m.add("enqueued_hook", int64(er.Enqueued))
	c.r.m.add("queue_dropped_oldest", int64(er.DroppedOldest))
	if er.Enqueued > 0 {
		c.sender.Notify()
	}
}

func (c *collector) closeAll() {
	for _, w := range c.files {
		_ = w.t.Close()
	}
	c.files = map[string]*watched{}
	c.lastDiscover = time.Time{}
}
