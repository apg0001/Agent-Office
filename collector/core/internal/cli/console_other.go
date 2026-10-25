//go:build !windows

package cli

// detachOwnConsole은 Windows 밖에서는 아무것도 하지 않는다.
func detachOwnConsole() bool { return false }
