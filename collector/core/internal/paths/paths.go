// Package paths는 OS별 Collector 설정·상태 디렉터리를 정한다(#34).
//
// 경로 원문은 로그·진단·서버로 내보내지 않는다. 진단에는 Label 같은 기호 이름만 쓴다.
package paths

import (
	"errors"
	"os"
	"path"
	"runtime"
	"strings"
)

// EnvHome은 모든 Collector 파일을 한 디렉터리 아래에 두는 재정의 변수다(테스트·휴대용 설치).
const EnvHome = "AGENT_OFFICE_COLLECTOR_HOME"

// ErrUnavailable은 OS가 사용자 디렉터리를 알려주지 않을 때 반환한다.
var ErrUnavailable = errors.New("paths: user directory unavailable")

// Dirs는 Collector가 쓰는 디렉터리다.
type Dirs struct {
	GOOS string
	// Home은 사용자 홈이다. 서비스 정의 위치 계산에 쓴다.
	Home string
	// Config는 config.json(장치 토큰, 0600)과 pause.json을 둔다(로밍 가능 설정).
	Config string
	// State는 queue.db, status.json, spool/, collector.lock을 둔다(기기 로컬 상태).
	State string
	// ClaudeProjects는 기본 Claude Code transcript 감시 루트다. 읽기 전용으로만 쓴다.
	ClaudeProjects string
}

// Env는 환경 변수 조회 함수다.
type Env func(string) string

// Default는 현재 프로세스 OS·환경 변수로 Dirs를 정한다.
func Default() (Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return Dirs{}, ErrUnavailable
	}
	return Resolve(runtime.GOOS, os.Getenv, home)
}

// Resolve는 순수 함수다. goos는 Go GOOS 값이다.
//
//   - Linux 등: 설정 `${XDG_CONFIG_HOME:-~/.config}/agent-office/collector`,
//     상태 `${XDG_STATE_HOME:-~/.local/state}/agent-office/collector`(XDG 값은 절대 경로일 때만 사용)
//   - macOS: `~/Library/Application Support/AgentOffice/Collector`, 상태는 그 아래 `state`
//   - Windows: 설정 `%APPDATA%\AgentOffice\Collector`, 상태 `%LOCALAPPDATA%\AgentOffice\Collector`
//   - `AGENT_OFFICE_COLLECTOR_HOME`이 있으면 그 아래 `config`, `state`
//   - Claude transcript 루트: `$CLAUDE_CONFIG_DIR/projects`, 없으면 `~/.claude/projects`
func Resolve(goos string, env Env, home string) (Dirs, error) {
	if home == "" {
		return Dirs{}, ErrUnavailable
	}
	d := Dirs{GOOS: goos, Home: home}
	switch override := env(EnvHome); {
	case override != "" && isAbs(goos, override):
		d.Config = Join(goos, override, "config")
		d.State = Join(goos, override, "state")
	case goos == "windows":
		appData, local := env("APPDATA"), env("LOCALAPPDATA")
		if !isAbs(goos, appData) || !isAbs(goos, local) {
			return Dirs{}, ErrUnavailable
		}
		d.Config = Join(goos, appData, "AgentOffice", "Collector")
		d.State = Join(goos, local, "AgentOffice", "Collector")
	case goos == "darwin":
		base := Join(goos, home, "Library", "Application Support", "AgentOffice", "Collector")
		d.Config = base
		d.State = Join(goos, base, "state")
	default:
		cfg := env("XDG_CONFIG_HOME")
		if !isAbs(goos, cfg) {
			cfg = Join(goos, home, ".config")
		}
		st := env("XDG_STATE_HOME")
		if !isAbs(goos, st) {
			st = Join(goos, home, ".local", "state")
		}
		d.Config = Join(goos, cfg, "agent-office", "collector")
		d.State = Join(goos, st, "agent-office", "collector")
	}
	if c := env("CLAUDE_CONFIG_DIR"); isAbs(goos, c) {
		d.ClaudeProjects = Join(goos, c, "projects")
	} else {
		d.ClaudeProjects = Join(goos, home, ".claude", "projects")
	}
	return d, nil
}

// ConfigFile, PauseFile 등은 고정 파일 위치다.
func (d Dirs) ConfigFile() string { return Join(d.GOOS, d.Config, "config.json") }
func (d Dirs) PauseFile() string  { return Join(d.GOOS, d.Config, "pause.json") }
func (d Dirs) QueueFile() string  { return Join(d.GOOS, d.State, "queue.db") }
func (d Dirs) StatusFile() string { return Join(d.GOOS, d.State, "status.json") }
func (d Dirs) LockFile() string   { return Join(d.GOOS, d.State, "collector.lock") }
func (d Dirs) SpoolDir() string   { return Join(d.GOOS, d.State, "spool") }

// BaselineFile은 최초 실행 기준선(기존 transcript 끝에서 시작) 완료 표시다.
func (d Dirs) BaselineFile() string { return Join(d.GOOS, d.State, "baseline.json") }

// Join은 goos의 구분자로 경로를 잇는다(Linux에서 Windows 경로를 생성·테스트하기 위해).
func Join(goos string, parts ...string) string {
	if goos != "windows" {
		return path.Join(parts...)
	}
	var b strings.Builder
	for i, p := range parts {
		p = strings.ReplaceAll(p, "/", `\`)
		if i > 0 {
			p = strings.TrimLeft(p, `\`)
			if b.Len() > 0 && !strings.HasSuffix(b.String(), `\`) {
				b.WriteByte('\\')
			}
		}
		b.WriteString(p)
	}
	return b.String()
}

func isAbs(goos, p string) bool {
	if p == "" {
		return false
	}
	if goos == "windows" {
		if strings.HasPrefix(p, `\\`) {
			return true
		}
		return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
	}
	return strings.HasPrefix(p, "/")
}
