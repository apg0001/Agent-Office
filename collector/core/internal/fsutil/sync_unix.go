//go:build !windows

package fsutil

import "os"

const chmodUnsupported = false

// syncDir는 rename을 디렉터리에 영속화한다. 실패는 무시한다(최선 노력).
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
