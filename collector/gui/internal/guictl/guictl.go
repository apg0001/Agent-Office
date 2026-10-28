// Package guictl은 GUI 창 프로세스의 단일 실행과 "창 보이기·종료" 요청이다(#159).
//
// Wails v2.12.0 Windows 단일 실행은 메시지 전용 창(HWND_MESSAGE)을 FindWindowW로 찾으므로 기존 창을 찾지
// 못해 두 번째 GUI가 그대로 뜨고 인자도 전달되지 않는다(windows-latest 러너 실측: 트레이 "종료" 뒤에도 GUI가
// 남음). 그래서 OS와 상관없이 같은 방식을 쓴다.
//
//   - 첫 GUI는 사용자별 파일 잠금(gui.lock)을 잡고, 같은 폴더의 요청 파일(gui.request)을 짧은 주기로 읽는다.
//   - 두 번째 실행(트레이 "설정 열기"·"종료", 사용자가 다시 실행)은 잠금을 못 잡으면 요청 파일에
//     show 또는 quit을 원자적으로 쓰고 Wails를 띄우지 않은 채 곧바로 끝난다.
//   - --quit으로 떴는데 잠금을 잡았다면 떠 있는 GUI가 없으므로 아무것도 하지 않고 끝난다.
//
// 요청 파일은 동사 한 단어만 담는다. 같은 사용자의 다른 프로세스만 쓸 수 있는 0700 폴더(GUI 설정 폴더)에 있다.
package guictl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/single"
)

// Verb는 요청 종류다.
type Verb string

const (
	Show Verb = "show"
	Quit Verb = "quit"
)

const (
	lockName    = "gui.lock"
	requestName = "gui.request"
	// PollInterval은 첫 GUI가 요청 파일을 읽는 주기다.
	PollInterval = 300 * time.Millisecond
)

// Primary는 첫 GUI 프로세스의 잠금이다.
type Primary struct {
	dir  string
	lock *single.Lock
}

// Acquire는 dir(GUI 설정 폴더)의 잠금을 잡는다. 이미 다른 GUI가 잡고 있으면 single.ErrHeld다.
// 잡으면 이전 실행이 남긴 요청 파일을 지운다(비정상 종료 뒤 남은 quit으로 곧바로 끝나지 않게).
func Acquire(dir string) (*Primary, error) {
	l, err := single.Acquire(filepath.Join(dir, lockName))
	if err != nil {
		return nil, err
	}
	_ = os.Remove(filepath.Join(dir, requestName))
	return &Primary{dir: dir, lock: l}, nil
}

// Release는 잠금을 푼다.
func (p *Primary) Release() {
	if p != nil {
		p.lock.Release()
	}
}

// Pending은 아직 읽지 않은 요청을 지우지 않고 돌려준다(없거나 알 수 없으면 "").
// 창이 늦게 뜨는 동안 트레이 "종료"가 이미 도착했는지 보려는 것이다(#177).
func (p *Primary) Pending() Verb {
	if p == nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(p.dir, requestName))
	if err != nil {
		return ""
	}
	switch v := Verb(strings.TrimSpace(string(b))); v {
	case Show, Quit:
		return v
	}
	return ""
}

// Serve는 ctx가 끝날 때까지 요청 파일을 읽어 handle을 부른다. 읽은 요청은 지운다.
func (p *Primary) Serve(ctx context.Context, handle func(Verb)) {
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	path := filepath.Join(p.dir, requestName)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		_ = os.Remove(path)
		switch v := Verb(strings.TrimSpace(string(b))); v {
		case Show, Quit:
			handle(v)
		}
	}
}

// ErrInvalidVerb는 알 수 없는 요청이다.
var ErrInvalidVerb = errors.New("guictl: invalid verb")

// Send는 첫 GUI에 요청을 남긴다(같은 폴더 임시 파일 → rename). 첫 GUI가 없어도 오류가 아니다.
func Send(dir string, v Verb) error {
	if v != Show && v != Quit {
		return ErrInvalidVerb
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gui-request-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(string(v) + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, requestName))
}
