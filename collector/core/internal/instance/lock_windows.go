//go:build windows

package instance

import (
	"errors"
	"os"
	"syscall"
)

// errorSharingViolation은 ERROR_SHARING_VIOLATION(32)이다.
const errorSharingViolation = syscall.Errno(32)

func lockFile(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, errors.New("instance: invalid lock path")
	}
	// 공유 모드 0: 핸들이 열려 있는 동안 다른 프로세스는 같은 파일을 열 수 없다.
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errorSharingViolation) {
			return nil, ErrLocked
		}
		return nil, errors.New("instance: lock failed")
	}
	return os.NewFile(uintptr(h), path), nil
}
