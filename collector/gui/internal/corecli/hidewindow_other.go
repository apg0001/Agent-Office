//go:build !windows

package corecli

import "os/exec"

func hideWindow(*exec.Cmd) {}
