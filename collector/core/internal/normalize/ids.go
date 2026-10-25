package normalize

import (
	"errors"
	"strings"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

// IdentityNamespace는 제공자 세션·에이전트 참조 → v1 UUID 변환용 고정 namespace다.
// adapter.EventIDNamespace와 다르게 두어 event_id와 식별자 UUID가 같은 이름에서 충돌하지 않게 한다.
// 바꾸면 재시작 전후로 같은 세션이 다른 UUID가 되므로 바꾸지 않는다.
var IdentityNamespace = [16]byte{
	0x2d, 0x6a, 0x1f, 0x84, 0x93, 0x5e, 0x5b, 0x27,
	0xa4, 0x0c, 0x7e, 0x31, 0xd5, 0x98, 0x46, 0xbf,
}

// ErrInvalidDeviceID는 장치 ID가 UUID 형식이 아닐 때 반환한다. 입력 값은 담지 않는다.
var ErrInvalidDeviceID = errors.New("normalize: device id is not a UUID")

// SessionID는 (장치, 제공자, 원본 세션 참조)의 결정적 UUIDv5다.
func SessionID(deviceID, provider, sessionRef string) string {
	return adapter.UUIDv5(IdentityNamespace, "session|"+deviceID+"|"+provider+"|"+sessionRef)
}

// LeadAgentID는 세션의 대장(루트) 에이전트 UUID다. 세션당 하나다.
func LeadAgentID(deviceID, provider, sessionRef string) string {
	return adapter.UUIDv5(IdentityNamespace, "lead|"+deviceID+"|"+provider+"|"+sessionRef)
}

// SubagentID는 같은 세션 안 하위 에이전트 참조의 UUID다. 세션 범위로 묶어 다른 세션의 같은
// 원본 agent_id와 충돌하지 않는다.
func SubagentID(deviceID, provider, sessionRef, agentRef string) string {
	return adapter.UUIDv5(IdentityNamespace, "subagent|"+deviceID+"|"+provider+"|"+sessionRef+"|"+agentRef)
}

// canonicalUUID는 8-4-4-4-12 hex UUID를 소문자로 반환한다. 형식이 아니면 빈 값이다.
func canonicalUUID(s string) string {
	if !IsUUID(strings.ToLower(s)) {
		return ""
	}
	return strings.ToLower(s)
}

// IsUUID는 소문자 8-4-4-4-12 hex 형식인지 확인한다.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}
