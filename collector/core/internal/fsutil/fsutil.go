// Package fsutil은 Collector가 소유한 작은 파일을 원자적으로 쓰는 도우미다(#34).
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNotRegular는 쓰려는 경로가 일반 파일이 아닐 때(디렉터리·심볼릭 링크 등) 반환한다.
var ErrNotRegular = errors.New("fsutil: target is not a regular file")

// WriteFileAtomic은 같은 디렉터리의 임시 파일에 쓰고 fsync한 뒤 rename으로 교체한다.
// 디렉터리는 0700으로 만든다. 대상이 심볼릭 링크면 따라가지 않고 거부한다.
// 오류에는 경로를 넣지 않는다(호출자가 진단에 그대로 쓸 수 있게).
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("fsutil: create directory failed")
	}
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return ErrNotRegular
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return errors.New("fsutil: create temp file failed")
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(perm); err != nil && !chmodUnsupported {
		return errors.New("fsutil: chmod failed")
	}
	if _, err := tmp.Write(data); err != nil {
		return errors.New("fsutil: write failed")
	}
	if err := tmp.Sync(); err != nil {
		return errors.New("fsutil: sync failed")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("fsutil: close failed")
	}
	if err := os.Rename(name, path); err != nil {
		return errors.New("fsutil: rename failed")
	}
	ok = true
	syncDir(dir)
	return nil
}
