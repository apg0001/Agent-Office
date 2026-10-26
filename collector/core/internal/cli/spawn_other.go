//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// detachAttr는 새 세션으로 띄워 부모(트레이)가 끝나도 함께 끝나지 않게 한다.
func detachAttr(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
