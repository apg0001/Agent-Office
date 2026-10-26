// Package launch는 GUI 창 ↔ 트레이 도우미 프로세스를 서로 띄우고, 기본 브라우저로 주소를 연다(#159).
//
// 두 프로세스는 부모·자식으로 묶지 않는다. 어느 한쪽이 끝나도 다른 쪽과 Core 수집은 계속된다.
package launch

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

// ErrInvalidURL은 http(s)가 아니거나 형식이 틀린 주소다(브라우저 명령에 임의 인자를 넘기지 않음).
var ErrInvalidURL = errors.New("launch: invalid url")

// Detached는 exe를 띄우고 기다리지 않는다(좀비가 남지 않게 뒤에서 Wait).
func Detached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Wait는 exe를 띄우고 timeout까지 끝나기를 기다린다. 넘으면 그 프로세스를 끝낸다.
func Wait(timeout time.Duration, exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return errors.New("launch: timeout")
	}
}

// BrowserCommand는 OS 기본 브라우저로 u를 여는 명령이다. u는 검증된 http(s) 주소여야 한다.
func BrowserCommand(goos, u string) ([]string, error) {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" || p.User != nil {
		return nil, ErrInvalidURL
	}
	s := p.String()
	switch goos {
	case "darwin":
		return []string{"open", s}, nil
	case "windows":
		// cmd /c start는 &·^ 등을 해석하므로 쓰지 않는다.
		return []string{"rundll32.exe", "url.dll,FileProtocolHandler", s}, nil
	default:
		return []string{"xdg-open", s}, nil
	}
}

// OpenURL은 기본 브라우저로 u를 연다.
func OpenURL(u string) error {
	argv, err := BrowserCommand(runtime.GOOS, u)
	if err != nil {
		return err
	}
	return Detached(argv[0], argv[1:]...)
}
