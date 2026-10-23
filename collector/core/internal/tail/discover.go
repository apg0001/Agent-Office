package tail

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// DefaultMaxDepth는 Claude projects 기준 projects/<p>/<s>/subagents/<a>.jsonl(깊이 4)까지다.
	DefaultMaxDepth = 4
	// DefaultMaxFiles는 한 번의 탐색에서 돌려줄 최대 파일 수다.
	DefaultMaxFiles = 10000
)

// ErrRootUnavailable은 감시 루트를 읽을 수 없을 때 반환한다. 경로 원문은 담지 않는다.
var ErrRootUnavailable = errors.New("tail: watch root unavailable")

// FileKind는 Claude transcript 배치 규칙에 따른 후보 분류다.
type FileKind string

const (
	// KindMain은 projects/<p>/<session>.jsonl이다.
	KindMain FileKind = "main"
	// KindSubagent는 .../subagents/<agent>.jsonl이다.
	KindSubagent FileKind = "subagent"
	// KindOther는 그 밖의 위치의 .jsonl이다. 호출자가 읽을지 정한다.
	KindOther FileKind = "other"
)

// File은 탐색된 후보 파일이다.
type File struct {
	// Rel은 루트 기준 슬래시 구분 상대 경로다. 프로젝트 디렉터리 이름에 로컬 경로가 인코딩되어
	// 있으므로 로그·서버·오류에 넣지 않는다. 파일을 열 때만 Path로 쓴다.
	Rel string
	// ID는 Rel의 SHA-256 앞 32자 hex다. 체크포인트 키와 로컬 진단에 쓴다(익명화 보장은 아님).
	ID   string
	Kind FileKind
}

// Path는 루트와 Rel을 합친 OS 경로다.
func (f File) Path(root string) string {
	return filepath.Join(root, filepath.FromSlash(f.Rel))
}

// DiscoverOptions는 탐색 상한이다. 0이면 기본값을 쓴다.
type DiscoverOptions struct {
	MaxDepth int
	MaxFiles int
}

// Discovery는 탐색 결과다. 건너뛴 항목은 개수만 센다.
type Discovery struct {
	Files           []File
	SkippedSymlinks int
	SkippedDirs     int
	Truncated       bool
}

// Discover는 root 아래 *.jsonl 일반 파일을 깊이 MaxDepth까지 찾는다. polling용으로 매번 호출한다.
// 심볼릭 링크는 파일·디렉터리 모두 따라가지 않으므로 루트 밖으로 나가지 않는다.
// 읽을 수 없는 하위 디렉터리는 건너뛰고 센다. 파일을 열거나 쓰지 않는다.
func Discover(root string, opts DiscoverOptions) (Discovery, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = DefaultMaxFiles
	}
	var d Discovery
	entries, err := os.ReadDir(root)
	if err != nil {
		return d, ErrRootUnavailable
	}
	d.walk(root, "", 1, entries, opts)
	sort.Slice(d.Files, func(i, j int) bool { return d.Files[i].Rel < d.Files[j].Rel })
	return d, nil
}

func (d *Discovery) walk(dir, rel string, depth int, entries []os.DirEntry, opts DiscoverOptions) {
	for _, e := range entries {
		if d.Truncated {
			return
		}
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		mode := e.Type()
		switch {
		case mode&os.ModeSymlink != 0:
			d.SkippedSymlinks++
		case e.IsDir():
			if depth >= opts.MaxDepth {
				continue
			}
			sub, err := os.ReadDir(filepath.Join(dir, name))
			if err != nil {
				d.SkippedDirs++
				continue
			}
			d.walk(filepath.Join(dir, name), childRel, depth+1, sub, opts)
		case mode.IsRegular() && strings.HasSuffix(name, ".jsonl"):
			if len(d.Files) >= opts.MaxFiles {
				d.Truncated = true
				return
			}
			d.Files = append(d.Files, File{Rel: childRel, ID: fileRefID(childRel), Kind: kindOf(childRel, depth)})
		}
	}
}

func kindOf(rel string, depth int) FileKind {
	switch {
	case depth == 2:
		return KindMain
	case path.Base(path.Dir(rel)) == "subagents":
		return KindSubagent
	default:
		return KindOther
	}
}

func fileRefID(rel string) string {
	sum := sha256.Sum256([]byte("agent-office/tail/v1\x00" + rel))
	return hex.EncodeToString(sum[:16])
}
