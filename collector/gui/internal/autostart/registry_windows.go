//go:build windows

package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// winRegistry는 HKCU Run 키다(현재 사용자, 관리자 권한 불필요).
type winRegistry struct{}

// SystemRegistry는 이 OS의 Run 키 접근자다. Windows 외에서는 nil이다.
func SystemRegistry() Registry { return winRegistry{} }

func (winRegistry) Get(name string) (string, bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		// 문자열이 아닌 값 등: 소유하지 않은 값으로 본다.
		return "", true, nil
	}
	return v, true, nil
}

func (winRegistry) Set(name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func (winRegistry) Delete(name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(name)
}
