//go:build !windows

package launch

import (
	"os/exec"
	"syscall"
)

// detach는 새 세션으로 띄워 부모 터미널·프로세스 그룹 신호(Ctrl+C 등)를 같이 받지 않게 한다.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
