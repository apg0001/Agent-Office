//go:build windows

package cli

import (
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/apg0001/Agent-Office/collector/core/internal/hooks"
)

var procGetShortPathNameW = kernel32.NewProc("GetShortPathNameW")

// hookExe는 Hook 명령에 넣을 실행 파일 경로다(#225). 설치 폴더에 공백이 있으면(기본
// `Agent Office Collector`) 따옴표가 필요해 PowerShell에서 실행되지 않으므로 폴더만 8.3 짧은 경로로
// 바꾼다. 파일 이름은 그대로 두어 소유 판정(IsOwnedCommand)이 유지된다. 짧은 경로를 얻지 못하거나
// 여전히 안전하지 않으면 원래 경로를 쓴다.
func hookExe(exe string) string {
	dir, base := filepath.Split(exe)
	short, ok := shortPath(filepath.Clean(dir))
	if !ok {
		return exe
	}
	candidate := filepath.Join(short, base)
	if !hooks.AnyShellSafe(filepath.ToSlash(candidate)) {
		return exe
	}
	return candidate
}

func shortPath(long string) (string, bool) {
	in, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetShortPathNameW.Call(uintptr(unsafe.Pointer(in)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) >= len(buf) {
		return "", false
	}
	return syscall.UTF16ToString(buf[:n]), true
}
