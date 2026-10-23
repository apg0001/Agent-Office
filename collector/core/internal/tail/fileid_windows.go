//go:build windows

package tail

import (
	"os"
	"syscall"
)

// openReadOnly는 GENERIC_READ와 FILE_SHARE_READ|WRITE|DELETE로 연다.
// os.Open은 FILE_SHARE_DELETE를 주지 않아 핸들을 연 동안 기록 도구의 이름 변경·삭제를 막을 수 있다.
// Windows 실행 검증은 하지 않았다(교차 컴파일·vet만 확인).
func openReadOnly(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// fileIdentity는 GetFileInformationByHandle의 볼륨 일련번호와 64비트 파일 인덱스를 쓴다.
// os.SameFile과 같은 기준이다. ReFS의 128비트 파일 ID는 하위 64비트만 비교하게 된다.
func fileIdentity(f *os.File, _ os.FileInfo) (FileID, error) {
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d); err != nil {
		return FileID{}, errNoIdentity
	}
	return FileID{
		Device: uint64(d.VolumeSerialNumber),
		Inode:  uint64(d.FileIndexHigh)<<32 | uint64(d.FileIndexLow),
	}, nil
}
