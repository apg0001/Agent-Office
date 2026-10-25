package adapter

// SupportLevel은 후보를 만든 입력 형식이 실측으로 검증됐는지 나타낸다.
// 어댑터는 미검증 버전에서도 안전하게 파싱하지만, 지원이라고 주장하지 않는다.
type SupportLevel string

const (
	// SupportVerified: 해당 제품·버전·OS·모드에서 이 이벤트를 반복 관측했다.
	SupportVerified SupportLevel = "verified"
	// SupportPartial: 관측은 했지만 반복성이 검증되지 않았다(예: Codex Desktop 1회).
	SupportPartial SupportLevel = "partial"
	// SupportUnverified: 해당 런타임이나 이벤트를 실측하지 않았다.
	SupportUnverified SupportLevel = "unverified"
)

// ConfidenceFor는 검증 수준을 이벤트 v1 confidence로 바꾼다. 반복 검증된 입력만
// observed이고, 부분 관측·미검증 입력의 의미 매핑은 inferred로 낮춘다.
func ConfidenceFor(level SupportLevel) Confidence {
	if level == SupportVerified {
		return ConfidenceObserved
	}
	return ConfidenceInferred
}

// Runtime은 입력을 만든 도구 실행 환경이다. Hook payload에는 버전이 없으므로 호출자
// (Hook 설치·버전 감지, #31)가 로컬에서 감지한 값을 넘긴다. 값은 후보에 복사되지 않는다.
type Runtime struct {
	// Product: "claude-code", "codex-cli", "codex-desktop".
	Product string
	// Version: 도구가 보고한 버전 문자열(예: "2.1.308").
	Version string
	// OS: Go GOOS 형식("linux", "windows", "darwin").
	OS string
	// Mode: 실행 모드("print"=Claude -p, "interactive", "exec"=codex exec, "desktop").
	Mode string
}

// Target은 실측으로 검증된 런타임 한 가지와 그때 관측된 Hook 이벤트 이름이다.
type Target struct {
	Runtime Runtime
	Level   SupportLevel
	// Observed는 해당 런타임에서 관측된 Hook 이벤트 이름 집합이다.
	Observed map[string]bool
}

// Assess는 런타임과 Hook 이벤트 이름의 검증 수준을 반환한다. 대상 목록과 정확히
// 일치하지 않는 런타임, 또는 그 런타임에서 관측되지 않은 이벤트는 unverified다.
func Assess(targets []Target, rt Runtime, hookEvent string) SupportLevel {
	for _, t := range targets {
		if t.Runtime != rt {
			continue
		}
		if t.Observed[hookEvent] {
			return t.Level
		}
		return SupportUnverified
	}
	return SupportUnverified
}

// Set은 문자열 집합 리터럴 도우미다.
func Set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
