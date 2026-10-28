//go:build !windows

package cli

// hookExe는 Windows가 아니면 그대로다(#225, 짧은 경로는 Windows 전용).
func hookExe(exe string) string { return exe }
