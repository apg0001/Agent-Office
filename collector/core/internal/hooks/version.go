package hooks

import (
	"bytes"
	"regexp"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
)

var versionRE = regexp.MustCompile(`(?:^|[^0-9.])(\d{1,6}\.\d{1,6}\.\d{1,9}(?:-[0-9A-Za-z][0-9A-Za-z.]{0,40})?)`)

// ParseVersion은 `--version` 출력 첫 줄에서 버전 문자열만 꺼낸다. 다른 내용은 버린다.
// 예: "2.1.308 (Claude Code)" → "2.1.308", "codex-cli 0.162.0-alpha.17.2" → "0.162.0-alpha.17.2".
func ParseVersion(out []byte) string {
	if len(out) > 256 {
		out = out[:256]
	}
	if i := bytes.IndexAny(out, "\r\n"); i >= 0 {
		out = out[:i]
	}
	m := versionRE.FindSubmatch(out)
	if m == nil {
		return ""
	}
	return string(bytes.TrimRight(m[1], "."))
}

// Support는 감지한 도구 버전의 검증 수준이다.
type Support struct {
	Level adapter.SupportLevel
	// Mode는 검증 대상과 일치할 때 그 실측 모드다("print", "exec", "desktop").
	Mode string
}

// AssessVersion은 어댑터의 실측 대상 표와 제품·버전·OS가 정확히 같을 때만 그 수준을 돌려준다.
// 실측 대상은 특정 실행 모드에서만 검증됐으므로 Mode를 함께 보고한다.
func AssessVersion(tool, version, goos string) Support {
	var targets []adapter.Target
	product := ""
	switch tool {
	case ToolClaude:
		targets, product = claude.VerifiedTargets, "claude-code"
	case ToolCodex:
		targets, product = codex.VerifiedTargets, codex.ProductCLI
	}
	if version == "" {
		return Support{Level: adapter.SupportUnverified}
	}
	for _, t := range targets {
		if t.Runtime.Product == product && t.Runtime.Version == version && t.Runtime.OS == goos {
			return Support{Level: t.Level, Mode: t.Runtime.Mode}
		}
	}
	return Support{Level: adapter.SupportUnverified}
}
