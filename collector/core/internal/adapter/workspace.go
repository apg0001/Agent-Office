package adapter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxWorkspaceNameRunes는 이벤트 v1 `workspace_name` 최대 길이(코드 포인트)다(#181).
const MaxWorkspaceNameRunes = 64

// HookOptions는 Hook payload에서 선택적으로 읽는 항목이다. 0 값은 아무것도 더 읽지 않는다.
type HookOptions struct {
	// WorkspaceName이 true일 때만(PC별 사용자 동의, #181) `cwd`를 읽어 마지막 폴더 이름만 후보에 담는다.
	// false면 `cwd` 값을 문자열로 꺼내지도 않는다.
	WorkspaceName bool
	// ExcludeDirs는 이름을 보내지 않을 작업 폴더다(사용자 홈 등: 마지막 이름이 OS 사용자 이름이다).
	ExcludeDirs []string
}

// Workspace는 opts가 허용할 때 obj의 `cwd`에서 작업 폴더 이름을 꺼낸다. 아니면 빈 값이다.
func (opts HookOptions) Workspace(obj Object) string {
	if !opts.WorkspaceName {
		return ""
	}
	return WorkspaceName(obj.String("cwd"), opts.ExcludeDirs...)
}

// WorkspaceName은 작업 폴더 경로에서 마지막 이름(basename)만 정리해 반환한다. 보낼 수 없으면 빈 값이다.
//
//   - `/`와 `\`를 모두 구분자로 본다(Windows 경로를 다른 OS에서 받아도 상위 폴더가 남지 않는다).
//   - 제어·서식 문자(Cc·Cf·줄/문단 구분자)를 지우고 앞뒤 공백을 자른 뒤 64자로 자른다.
//   - 빈 값, `.`, `..`, 드라이브 루트(`C:`), exclude와 같은 폴더(대소문자 무시)는 보내지 않는다.
//
// 상위 폴더와 전체 경로는 반환값에 남지 않는다.
func WorkspaceName(cwd string, exclude ...string) string {
	if cwd == "" || len(cwd) > 4096 {
		return ""
	}
	trimmed := trimSeparators(cwd)
	for _, dir := range exclude {
		if d := trimSeparators(dir); d != "" && strings.EqualFold(d, trimmed) {
			return ""
		}
	}
	base := trimmed
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if !utf8.ValidString(base) {
		base = strings.ToValidUTF8(base, "")
	}
	base = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return -1
		}
		return r
	}, base)
	base = strings.TrimSpace(base)
	if utf8.RuneCountInString(base) > MaxWorkspaceNameRunes {
		r := []rune(base)
		base = strings.TrimSpace(string(r[:MaxWorkspaceNameRunes]))
	}
	if base == "" || base == "." || base == ".." || isDriveRoot(base) {
		return ""
	}
	return base
}

// ValidWorkspaceName은 이미 정리된 값인지 확인한다(spool·Normalizer 2차 방어).
func ValidWorkspaceName(s string) bool {
	return s != "" && WorkspaceName(s) == s
}

func trimSeparators(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), `/\`)
}

func isDriveRoot(s string) bool {
	return len(s) == 2 && s[1] == ':' && (s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z')
}
