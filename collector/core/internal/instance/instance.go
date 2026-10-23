// Package instance는 사용자당 Collector `run` 프로세스를 하나로 제한하는 잠금이다(#34).
//
// 잠금은 열린 파일 핸들에 묶이므로 프로세스가 비정상 종료해도 OS가 풀어 준다(오래된 잠금 없음).
// Unix는 flock(LOCK_EX|LOCK_NB), Windows는 공유 모드 0 CreateFile이다.
package instance

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrLocked는 다른 프로세스가 이미 잠금을 갖고 있을 때 반환한다.
var ErrLocked = errors.New("instance: another collector is running")

// Lock은 획득한 잠금이다.
type Lock struct{ f *os.File }

// Acquire는 잠금을 시도한다. 기다리지 않는다.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errors.New("instance: create directory failed")
	}
	f, err := lockFile(path)
	if err != nil {
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release는 잠금을 푼다. 파일은 지우지 않는다(지우면 다른 프로세스와 경쟁이 생긴다).
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Held는 다른 프로세스가 잠금을 갖고 있는지 확인한다(status용). 잠깐 획득했다가 바로 푼다.
func Held(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	l, err := Acquire(path)
	if errors.Is(err, ErrLocked) {
		return true
	}
	if err == nil {
		_ = l.Release()
	}
	return false
}
