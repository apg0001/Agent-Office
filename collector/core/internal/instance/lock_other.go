//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package instance

import (
	"errors"
	"os"
)

// 지원 대상이 아닌 OS에서는 잠금 없이 파일만 연다(중복 실행 방지 없음, 문서화된 제한).
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errors.New("instance: open lock file failed")
	}
	return f, nil
}
