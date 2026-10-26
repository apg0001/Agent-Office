package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/fsutil"
)

// maxSettingsBytes는 설정 파일 읽기 상한이다.
const maxSettingsBytes = 4 << 20

// BackupInfix는 백업 파일 이름 표시다: `<설정 파일>.agent-office-backup-<UTC 시각>`.
const BackupInfix = ".agent-office-backup-"

var (
	// ErrNotRegular는 설정 파일이 심볼릭 링크·디렉터리 등이라 쓰지 않을 때다.
	ErrNotRegular = errors.New("hooks: settings path is not a regular file")
	// ErrReadOnly는 설정 파일에 소유자 쓰기 권한이 없어 쓰지 않을 때다.
	ErrReadOnly = errors.New("hooks: settings file is read-only")
	// ErrTooLarge는 설정 파일이 상한보다 클 때다.
	ErrTooLarge = errors.New("hooks: settings file is too large")
	// ErrChanged는 읽은 뒤 쓰기 전에 다른 프로세스가 파일을 바꿨을 때다.
	ErrChanged = errors.New("hooks: settings file changed during update")
	// ErrIO는 그 밖의 읽기·쓰기 실패다(경로를 담지 않음).
	ErrIO = errors.New("hooks: settings file read/write failed")
)

// Result는 설치·제거 결과다.
type Result struct {
	Changes []Change
	// Wrote는 파일을 실제로 썼는지다(apply=false면 항상 false).
	Wrote bool
	// Backup은 만든 백업 파일 경로다(원래 파일이 없었으면 빈 값).
	Backup string
}

type snapshot struct {
	data   []byte
	mode   fs.FileMode
	exists bool
}

func readSettings(path string) (snapshot, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return snapshot{mode: 0o600}, nil
	}
	if err != nil {
		return snapshot{}, ErrIO
	}
	if !fi.Mode().IsRegular() {
		return snapshot{}, ErrNotRegular
	}
	if fi.Size() > maxSettingsBytes {
		return snapshot{}, ErrTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return snapshot{}, ErrIO
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSettingsBytes+1))
	if err != nil {
		return snapshot{}, ErrIO
	}
	if len(data) > maxSettingsBytes {
		return snapshot{}, ErrTooLarge
	}
	return snapshot{data: data, mode: fi.Mode().Perm(), exists: true}, nil
}

// Install은 소유 Hook 항목을 설정 파일에 추가·갱신한다. apply가 false면 변경 목록만 돌려준다.
func Install(path string, s Spec, now time.Time, apply bool) (Result, error) {
	return update(path, now, apply, s.planInstall)
}

// Uninstall은 설정 파일에서 소유 Hook 항목만 지운다. 파일이 없으면 변경 없음이다.
func Uninstall(path string, s Spec, now time.Time, apply bool) (Result, error) {
	return update(path, now, apply, s.planUninstall)
}

// Inspect는 설정 파일의 소유 항목 상태를 읽는다. 파일이 없으면 not_installed다.
func Inspect(path string, s Spec) (Status, error) {
	snap, err := readSettings(path)
	if err != nil {
		return Status{State: StateUnreadable}, err
	}
	return s.inspect(snap.data)
}

func update(path string, now time.Time, apply bool, plan func([]byte) ([]byte, []Change, error)) (Result, error) {
	snap, err := readSettings(path)
	if err != nil {
		return Result{}, err
	}
	next, changes, err := plan(snap.data)
	if err != nil || len(changes) == 0 {
		return Result{}, err
	}
	res := Result{Changes: changes}
	if !apply {
		return res, nil
	}
	if snap.exists && snap.mode&0o200 == 0 {
		return res, ErrReadOnly
	}
	if snap.exists {
		if res.Backup, err = writeBackup(path, snap.data, now); err != nil {
			return res, err
		}
	}
	// 다른 프로세스(예: Claude Code 설정 화면)가 그 사이 바꿨으면 덮어쓰지 않는다.
	again, err := readSettings(path)
	if err != nil {
		return res, err
	}
	if again.exists != snap.exists || !bytes.Equal(again.data, snap.data) {
		return res, ErrChanged
	}
	if err := fsutil.WriteFileAtomic(path, next, snap.mode); err != nil {
		if errors.Is(err, fsutil.ErrNotRegular) {
			return res, ErrNotRegular
		}
		return res, ErrIO
	}
	res.Wrote = true
	return res, nil
}

// writeBackup은 원본 바이트를 같은 디렉터리에 0600으로 새로 만든다(기존 파일을 덮어쓰지 않음).
func writeBackup(path string, data []byte, now time.Time) (string, error) {
	base := path + BackupInfix + now.UTC().Format("20060102T150405Z")
	for i := 0; i < 100; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", ErrIO
		}
		_, werr := f.Write(data)
		serr := f.Sync()
		cerr := f.Close()
		if werr != nil || serr != nil || cerr != nil {
			_ = os.Remove(name)
			return "", ErrIO
		}
		return name, nil
	}
	return "", ErrIO
}

// CodexHooksPath는 사용자 범위 Codex Hook 파일 위치 `$CODEX_HOME/hooks.json`이다. codexHome은
// paths.Dirs.CodexHome(`$CODEX_HOME` 또는 `~/.codex`)이다. Codex 0.163.0은 사용자 설정 계층의
// 폴더(config.toml이 있는 곳)에서 hooks.json을 읽는다(소스 `discovery.rs` `load_hooks_json`, #170 실측).
func CodexHooksPath(codexHome string) string {
	return filepath.Join(codexHome, "hooks.json")
}

// ClaudeSettingsPath는 사용자 범위 Claude Code 설정 파일 위치다. claudeProjects는
// paths.Dirs.ClaudeProjects(`$CLAUDE_CONFIG_DIR/projects` 또는 `~/.claude/projects`)다.
func ClaudeSettingsPath(claudeProjects string) string {
	return strings.TrimSuffix(claudeProjects, "projects") + "settings.json"
}
