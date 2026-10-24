// Package secret은 장치 토큰을 OS 보안 저장소에 보관하는 계층이다(#41).
//
//   - Linux: Secret Service(D-Bus, GNOME Keyring·KWallet 등)
//   - macOS: 로그인 키체인(`/usr/bin/security`, 비밀 값은 표준 입력으로 전달)
//   - Windows: 자격 증명 관리자(Credential Manager, 일반 자격 증명)
//
// 구현은 github.com/zalando/go-keyring(순수 Go, cgo 없음)을 감싼다. 호출마다 시간 제한을 두고,
// 오류는 ErrNotFound·ErrUnavailable 두 가지로만 돌려준다(OS 오류 문자열에는 경로·계정이 섞일 수 있어 버림).
// 테스트는 Memory를 쓰고 실제 저장소를 건드리지 않는다.
package secret

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// Service는 보안 저장소 항목의 서비스 이름이다. 계정(account)은 장치 ID다.
const Service = "io.github.apg0001.agent-office.collector"

// EnvStore는 토큰 보관 위치 강제 변수다. "file"이면 OS 보안 저장소를 쓰지 않는다
// (headless 서버·컨테이너·테스트). 비어 있거나 "auto"면 보안 저장소를 먼저 시도한다.
const EnvStore = "AGENT_OFFICE_COLLECTOR_TOKEN_STORE"

// DefaultTimeout은 보안 저장소 호출 하나의 상한이다. 잠금 해제 프롬프트가 떠도 Collector가 멈추지 않게 한다.
const DefaultTimeout = 10 * time.Second

var (
	// ErrNotFound는 항목이 없을 때다.
	ErrNotFound = errors.New("secret: item not found")
	// ErrUnavailable은 보안 저장소가 없거나 잠겨 있거나 응답하지 않을 때다.
	ErrUnavailable = errors.New("secret: os secret store unavailable")
)

// Store는 비밀 값 저장소다. account는 장치 ID처럼 비밀이 아닌 식별자다.
type Store interface {
	Get(account string) (string, error)
	Set(account, value string) error
	Delete(account string) error
}

// System은 이 OS의 보안 저장소를 돌려준다. 강제 파일 모드이거나 사용할 수 없음이 분명하면 nil이다.
//
// Linux에서 세션 D-Bus 주소(DBUS_SESSION_BUS_ADDRESS 또는 /run/user/<uid>/bus)가 없으면 nil이다.
// D-Bus 라이브러리의 자동 실행(dbus-launch)으로 headless 서버에 데몬을 띄우지 않기 위해서다.
func System(getenv func(string) string, goos string) Store {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvStore))) {
	case "", "auto":
	default:
		// "file" 또는 알 수 없는 값: 안전하게 파일로 둔다(알 수 없는 값을 보안 저장소 강제로 보지 않음).
		return nil
	}
	switch goos {
	case "linux":
		if !sessionBusLikely(getenv, os.Getuid(), fileExists) {
			return nil
		}
	case "darwin", "windows":
	default:
		return nil
	}
	return &keyringStore{timeout: DefaultTimeout}
}

func sessionBusLikely(getenv func(string) string, uid int, exists func(string) bool) bool {
	if a := getenv("DBUS_SESSION_BUS_ADDRESS"); a != "" && a != "autolaunch:" {
		return true
	}
	if uid < 0 {
		return false
	}
	base := "/run/user/" + strconv.Itoa(uid)
	return exists(base+"/bus") || exists(base+"/dbus-session")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

type keyringStore struct {
	timeout time.Duration
}

// call은 fn을 시간 제한 안에서 실행한다. 제한을 넘기면 고루틴은 남지만 호출자는 ErrUnavailable을 받는다.
func (k *keyringStore) call(fn func() (string, error)) (string, error) {
	type res struct {
		v   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := fn()
		ch <- res{v, err}
	}()
	t := time.NewTimer(k.timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		switch {
		case r.err == nil:
			return r.v, nil
		case errors.Is(r.err, keyring.ErrNotFound):
			return "", ErrNotFound
		default:
			return "", ErrUnavailable
		}
	case <-t.C:
		return "", ErrUnavailable
	}
}

func (k *keyringStore) Get(account string) (string, error) {
	return k.call(func() (string, error) { return keyring.Get(Service, account) })
}

func (k *keyringStore) Set(account, value string) error {
	_, err := k.call(func() (string, error) { return "", keyring.Set(Service, account, value) })
	return err
}

func (k *keyringStore) Delete(account string) error {
	_, err := k.call(func() (string, error) { return "", keyring.Delete(Service, account) })
	return err
}

// Memory는 테스트용 저장소다. Unavailable이면 모든 호출이 ErrUnavailable이다.
type Memory struct {
	mu          sync.Mutex
	items       map[string]string
	Unavailable bool
	// FailGet은 Set은 되지만 Get이 실패하는 저장소(잠김 등)를 흉내 낸다.
	FailGet bool
	// Corrupt면 Get이 저장한 값과 다른 값을 돌려준다(이전 검증 테스트).
	Corrupt bool
	Calls   int
}

// NewMemory는 빈 Memory다.
func NewMemory() *Memory { return &Memory{items: map[string]string{}} }

func (m *Memory) Get(account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	if m.Unavailable || m.FailGet {
		return "", ErrUnavailable
	}
	v, ok := m.items[account]
	if !ok {
		return "", ErrNotFound
	}
	if m.Corrupt {
		return v + "x", nil
	}
	return v, nil
}

func (m *Memory) Set(account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	if m.Unavailable {
		return ErrUnavailable
	}
	m.items[account] = value
	return nil
}

func (m *Memory) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls++
	if m.Unavailable {
		return ErrUnavailable
	}
	if _, ok := m.items[account]; !ok {
		return ErrNotFound
	}
	delete(m.items, account)
	return nil
}

// SetFailGet은 동시에 쓰이는 중에 FailGet을 바꾼다(경쟁 없이).
func (m *Memory) SetFailGet(v bool) {
	m.mu.Lock()
	m.FailGet = v
	m.mu.Unlock()
}

// Has는 항목 존재 여부다(테스트 확인용).
func (m *Memory) Has(account string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.items[account]
	return ok
}

// Len은 항목 수다.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}
