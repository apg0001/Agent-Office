//go:build windows

package cli

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
)

// detachOwnConsole은 이 프로세스만 붙어 있는 콘솔(작업 스케줄러가 새로 만든 창)이면 떼어 내 창을
// 닫는다(#169). 터미널에서 실행해 다른 프로세스와 콘솔을 함께 쓰면 그대로 둔다.
func detachOwnConsole() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return false
	}
	ok, _, _ := procFreeConsole.Call()
	return ok != 0
}
