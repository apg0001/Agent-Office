//go:build unix

package tail

import (
	"os"
	"syscall"
)

// openReadOnly는 O_RDONLY로만 연다. 쓰기·생성·권한 변경 플래그를 쓰지 않는다.
func openReadOnly(path string) (*os.File, error) {
	return os.Open(path)
}

// fileIdentity는 fstat 결과의 (st_dev, st_ino)를 쓴다. 타입은 OS마다 달라 uint64로 변환한다.
func fileIdentity(_ *os.File, info os.FileInfo) (FileID, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, errNoIdentity
	}
	return FileID{Device: uint64(st.Dev), Inode: uint64(st.Ino)}, nil
}
