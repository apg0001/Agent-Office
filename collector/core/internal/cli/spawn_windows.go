//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// detachAttr는 콘솔 없이 다른 프로세스 그룹으로 띄운다(트레이·설치 프로그램 종료가 전파되지 않게).
func detachAttr(cmd *exec.Cmd) {
	const createNewProcessGroup = 0x00000200
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
