//go:build !windows

package autostart

// SystemRegistry는 이 OS의 Run 키 접근자다. Windows 외에서는 nil이다.
func SystemRegistry() Registry { return nil }
