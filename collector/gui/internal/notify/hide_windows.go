//go:build windows

package notify

import (
	"os/exec"
	"syscall"
)

// hide는 PowerShell 콘솔 창이 깜빡이지 않게 한다.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
