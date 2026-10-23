//go:build !unix && !windows

package tail

import "os"

func openReadOnly(path string) (*os.File, error) {
	return os.Open(path)
}

// 파일 식별자를 얻을 수 없는 플랫폼은 tail을 지원하지 않는다(StatusUnsupported).
func fileIdentity(_ *os.File, _ os.FileInfo) (FileID, error) {
	return FileID{}, errNoIdentity
}
