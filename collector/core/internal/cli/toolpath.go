package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// toolSearchDirs는 PATH에 없을 때 Claude·Codex를 찾아볼 흔한 설치 위치다(#204).
// Finder·시작 메뉴·로그인 항목으로 뜬 GUI와 트레이는 터미널 PATH(Homebrew, npm 전역, nvm 등)를
// 물려받지 못해 설치된 도구를 "감지 안 됨"으로 보였다(소유자 맥 실측).
func toolSearchDirs(goos, home string, getenv func(string) string, glob func(string) ([]string, error)) []string {
	var dirs []string
	add := func(parts ...string) {
		if parts[0] != "" {
			dirs = append(dirs, filepath.Join(parts...))
		}
	}
	add(home, ".local", "bin")
	add(home, ".claude", "local")
	add(home, ".npm-global", "bin")
	add(home, ".bun", "bin")
	add(home, ".volta", "bin")
	add(home, ".cargo", "bin")
	if goos == "windows" {
		add(getenv("APPDATA"), "npm")
		add(getenv("LOCALAPPDATA"), "Programs", "claude")
	} else {
		if goos == "darwin" {
			dirs = append(dirs, "/opt/homebrew/bin")
		}
		dirs = append(dirs, "/usr/local/bin", "/snap/bin")
		// nvm은 Node 버전마다 폴더가 있다. 버전 이름 역순(대개 최신 먼저).
		if home != "" {
			if found, err := glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); err == nil {
				sort.Sort(sort.Reverse(sort.StringSlice(found)))
				dirs = append(dirs, found...)
			}
		}
	}
	return dirs
}

// findTool은 PATH에서 먼저 찾고, 없으면 흔한 설치 위치에서 찾는다. 함께 돌려주는 PATH는 도구 폴더와
// 추가 위치를 앞에 붙인 값이다(npm으로 설치한 claude는 `#!/usr/bin/env node`라 node도 찾아야 한다).
func findTool(name string) (string, string, error) {
	home, _ := os.UserHomeDir()
	extra := toolSearchDirs(runtime.GOOS, home, os.Getenv, filepath.Glob)
	path := os.Getenv("PATH")
	if exe, err := exec.LookPath(name); err == nil {
		return exe, joinPath(append([]string{filepath.Dir(exe)}, extra...), path), nil
	}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ".cmd", ".bat"}
	}
	for _, dir := range extra {
		for _, ext := range exts {
			candidate := filepath.Join(dir, name+ext)
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() && (runtime.GOOS == "windows" || fi.Mode()&0o111 != 0) {
				return candidate, joinPath(append([]string{dir}, extra...), path), nil
			}
		}
	}
	return "", "", exec.ErrNotFound
}

func joinPath(front []string, rest string) string {
	parts := append([]string{}, front...)
	if rest != "" {
		parts = append(parts, rest)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}
