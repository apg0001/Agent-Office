//go:build windows

package corecli

import (
	"os/exec"
	"syscall"
)

// hideWindow는 GUI에서 콘솔 바이너리를 실행할 때 콘솔 창이 깜빡이지 않게 한다(Windows 미검증).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
