package cli

// 이전 명령행 설치 정리(#180).
//
//	legacy status [--json]          설치 프로그램으로 옮긴 뒤 남은 이전 명령행 설치 파일
//	legacy remove --yes [--json]    알아본 파일만 지운다
//
// 대상은 알려진 위치(macOS·Linux `~/.local/bin`, Windows `%LOCALAPPDATA%\AgentOffice\bin`)의 이 도구 파일뿐이다.
// 실행 파일은 Go 빌드 정보의 모듈 경로로, 제거 도우미·README는 내용으로 알아본다. 지금 실행 중인 Core가
// 설치 프로그램 배치일 때만, 그리고 서비스 정의·Hook 설정이 더는 그 경로를 가리키지 않을 때만 지운다.

import (
	"debug/buildinfo"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/apg0001/Agent-Office/collector/core/internal/hooks"
	"github.com/apg0001/Agent-Office/collector/core/internal/paths"
	"github.com/apg0001/Agent-Office/collector/core/internal/service"
	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

// CoreMainPackage는 Core 실행 파일의 Go 메인 패키지 경로다(빌드 정보로 이 도구의 바이너리인지 알아봄).
const CoreMainPackage = "github.com/apg0001/Agent-Office/collector/core/cmd/agent-office-collector"

type legacyFile struct {
	path string
	// Kind는 binary / backup / uninstall / readme다.
	Kind string `json:"kind"`
	// Name은 파일 이름, Location은 홈을 ~로 줄인 폴더다.
	Name     string `json:"name"`
	Location string `json:"location"`
	// State는 remove 결과(removed / in_use / failed / referenced)다.
	State string `json:"state,omitempty"`
}

// legacyDirs는 이전 명령행 설치 위치다(docs/installation/collector-cli.md).
func legacyDirs(env Env, dirs paths.Dirs) []string {
	if env.GOOS == "windows" {
		if l := env.Getenv("LOCALAPPDATA"); l != "" {
			return []string{filepath.Join(l, "AgentOffice", "bin")}
		}
		return nil
	}
	if dirs.Home == "" {
		return nil
	}
	return []string{filepath.Join(dirs.Home, ".local", "bin")}
}

// coreBinaryCheck는 테스트에서 바꾼다.
var coreBinaryCheck = isCoreBinary

func isCoreBinary(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	bi, err := buildinfo.ReadFile(path)
	return err == nil && bi.Path == CoreMainPackage
}

func fileContains(path string, needles ...string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 256<<10 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 256<<10))
	for _, n := range needles {
		if !strings.Contains(string(data), n) {
			return false
		}
	}
	return true
}

func samePath(goos, a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if goos == "windows" || goos == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// findLegacy는 알아본 이전 설치 파일 목록이다. 현재 실행 파일과 같은 위치는 넣지 않는다.
func findLegacy(env Env, dirs paths.Dirs, current string) []legacyFile {
	var out []legacyFile
	for _, dir := range legacyDirs(env, dirs) {
		if samePath(env.GOOS, dir, filepath.Dir(current)) {
			continue
		}
		bin := update.BinaryName(env.GOOS)
		cands := []struct{ name, kind string }{
			{bin, "binary"}, {bin + ".bak", "backup"}, {bin + ".new", "backup"},
		}
		if env.GOOS == "windows" {
			cands = append(cands, struct{ name, kind string }{"uninstall.ps1", "uninstall"})
		} else {
			cands = append(cands, struct{ name, kind string }{"agent-office-collector-uninstall", "uninstall"})
		}
		cands = append(cands, struct{ name, kind string }{"README.txt", "readme"})
		for _, c := range cands {
			p := filepath.Join(dir, c.name)
			ok := false
			switch c.kind {
			case "binary", "backup":
				ok = coreBinaryCheck(p)
			case "uninstall":
				ok = fileContains(p, "agent-office-collector", "Agent Office")
			case "readme":
				ok = fileContains(p, "Agent Office Collector", "agent-office-collector")
			}
			if ok {
				out = append(out, legacyFile{path: p, Kind: c.kind, Name: c.name, Location: displayPath(dir, dirs)})
			}
		}
	}
	return out
}

// referenced는 서비스 정의나 Hook 설정이 아직 path를 가리키는지다(후처리 전에는 지우지 않음).
func referenced(env Env, dirs paths.Dirs, path string) bool {
	files := []string{hooksPath(dirs, hooks.ToolClaude), hooksPath(dirs, hooks.ToolCodex)}
	if loc, err := service.Location(env.GOOS, env.Getenv, dirs); err == nil {
		files = append(files, loc)
	}
	needles := []string{path}
	if env.GOOS == "windows" {
		// JSON 안의 Windows 경로는 역슬래시가 두 번 쓰이고, Hook 명령은 슬래시로 쓴다(hooks.Command).
		needles = append(needles, strings.ReplaceAll(path, `\`, `\\`), strings.ReplaceAll(path, `\`, "/"))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := string(data)
		if env.GOOS == "windows" {
			s = decodeMaybeUTF16(data)
		}
		for _, n := range needles {
			if strings.Contains(strings.ToLower(s), strings.ToLower(n)) {
				return true
			}
		}
	}
	return false
}

// decodeMaybeUTF16은 UTF-16LE(BOM) 작업 XML을 문자열로 읽는다(ASCII 범위 비교용).
func decodeMaybeUTF16(data []byte) string {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xFE {
		return string(data)
	}
	var b strings.Builder
	for i := 2; i+1 < len(data); i += 2 {
		b.WriteRune(rune(uint16(data[i]) | uint16(data[i+1])<<8))
	}
	return b.String()
}

type legacyReport struct {
	// Eligible은 지금 Core가 설치 프로그램 배치라 이전 파일을 정리해도 되는지다.
	Eligible bool         `json:"eligible"`
	Files    []legacyFile `json:"files"`
	Removed  int          `json:"removed"`
	Failed   int          `json:"failed"`
}

func cmdLegacy(args []string, env Env) int {
	if len(args) == 0 || (args[0] != "status" && args[0] != "remove") {
		fmt.Fprintln(env.Stderr, "사용법: agent-office-collector legacy status|remove [--yes] [--json]")
		return ExitUsage
	}
	sub := args[0]
	fs := newFlags("legacy "+sub, env)
	yes := fs.Bool("yes", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	exe, err := env.Executable()
	if err != nil {
		return ExitError
	}
	dirs, err := env.Dirs()
	if err != nil {
		return ExitError
	}
	rep := legacyReport{Eligible: update.Managed(exe), Files: findLegacy(env, dirs, exe)}
	if rep.Files == nil {
		rep.Files = []legacyFile{}
	}
	code := ExitOK
	if sub == "remove" {
		switch {
		case !*yes:
			fmt.Fprintln(env.Stderr, "지우려면 --yes를 붙이세요")
			return ExitUsage
		case !rep.Eligible:
			fmt.Fprintln(env.Stderr, "설치 프로그램으로 설치한 Collector에서만 이전 명령행 설치를 정리합니다")
			return ExitError
		}
		for i := range rep.Files {
			f := &rep.Files[i]
			if (f.Kind == "binary" || f.Kind == "backup") && referenced(env, dirs, f.path) {
				// 자동 시작·Hook이 아직 이 파일을 가리킨다. 먼저 update post-install로 옮긴다.
				f.State = "referenced"
				rep.Failed++
				continue
			}
			if err := os.Remove(f.path); err != nil {
				f.State = "failed"
				if env.GOOS == "windows" {
					f.State = "in_use"
				}
				rep.Failed++
				continue
			}
			f.State = "removed"
			rep.Removed++
		}
		if rep.Failed > 0 {
			code = ExitError
		}
	}
	if *asJSON {
		printJSON(env, rep)
		return code
	}
	if len(rep.Files) == 0 {
		fmt.Fprintln(env.Stdout, "정리할 이전 명령행 설치 파일이 없습니다.")
		return code
	}
	for _, f := range rep.Files {
		fmt.Fprintf(env.Stdout, "  %s/%s (%s) %s\n", f.Location, f.Name, f.Kind, f.State)
	}
	if sub == "status" {
		if rep.Eligible {
			fmt.Fprintln(env.Stdout, "지우려면: agent-office-collector legacy remove --yes")
		} else {
			fmt.Fprintln(env.Stdout, "지금 실행한 Core가 설치 프로그램 배치가 아니라 정리하지 않습니다.")
		}
	}
	return code
}
