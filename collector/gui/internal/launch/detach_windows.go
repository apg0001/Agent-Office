//go:build windows

package launch

import (
	"os/exec"
	"syscall"
)

// detach는 부모 콘솔 없이, 다른 프로세스 그룹으로 띄운다(Ctrl+C·콘솔 종료가 전파되지 않게).
// HideWindow(SW_HIDE)는 쓰지 않는다. GUI 창의 첫 ShowWindow가 그 값을 따라 숨겨지기 때문이다.
func detach(cmd *exec.Cmd) {
	const createNewProcessGroup = 0x00000200
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
