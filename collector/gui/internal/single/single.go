// Package single은 트레이 도우미의 사용자별 단일 실행 잠금이다(#159).
//
// GUI가 뜰 때마다 트레이를 띄우고 자동 시작도 트레이를 띄우므로, 두 번째 트레이는 잠금을 못 얻고 곧바로 끝난다.
// OS 파일 잠금(Unix flock, Windows LockFileEx)이라 프로세스가 비정상 종료해도 잠금이 풀린다.
package single

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrHeld는 다른 프로세스가 이미 잠금을 가졌다.
var ErrHeld = errors.New("single: already running")

// Lock은 잡은 잠금이다. 프로세스가 끝날 때까지 들고 있는다.
type Lock struct{ f *os.File }

// Acquire는 path에 배타 잠금을 건다. 디렉터리가 없으면 0700으로 만든다.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release는 잠금을 푼다.
func (l *Lock) Release() {
	if l != nil && l.f != nil {
		_ = unlockFile(l.f)
		_ = l.f.Close()
		l.f = nil
	}
}
