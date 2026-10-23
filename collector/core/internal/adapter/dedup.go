package adapter

import (
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // UUIDv5(RFC 9562)는 SHA-1을 규정한다. 보안 해시 용도가 아니다.
	"fmt"
)

// EventIDNamespace는 키 → event_id 변환용 고정 namespace다(tests/poc/collector/dedup_reference.py와 동일).
// 바꾸면 기존 큐와 서버의 멱등성이 깨진다.
var EventIDNamespace = [16]byte{
	0x6f, 0x9c, 0x2b, 0x1e, 0x4d, 0x0a, 0x5c, 0x3b,
	0x9e, 0x7f, 0x2a, 0x1d, 0x8c, 0x4b, 0x6e, 0x50,
}

// DedupKey는 교차 소스 키 `device|provider|session|kind|ref`다. 키가 없으면 빈 값이다.
func DedupKey(deviceRef string, c Candidate) string {
	ref := c.DedupRef()
	if ref == "" {
		return ""
	}
	return deviceRef + "|" + string(c.Provider) + "|" + c.SessionRef + "|" + c.DedupKind() + "|" + ref
}

// UUIDv5는 RFC 9562 이름 기반 SHA-1 UUID를 소문자 문자열로 반환한다.
func UUIDv5(namespace [16]byte, name string) string {
	h := sha1.New() //nolint:gosec // UUIDv5 규격.
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)
	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50
	u[8] = (u[8] & 0x3f) | 0x80
	return formatUUID(u)
}

func randomUUID() (string, error) {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return "", err
	}
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return formatUUID(u), nil
}

func formatUUID(u [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// EventID는 키가 있으면 결정적 UUIDv5를, 없으면 무작위 UUIDv4를 반환한다.
// deterministic이 false인 ID는 큐(#33)에 처음 기록할 때 한 번만 만들어 영속화해야 한다.
func EventID(deviceRef string, c Candidate) (id string, deterministic bool, err error) {
	if key := DedupKey(deviceRef, c); key != "" {
		return UUIDv5(EventIDNamespace, key), true, nil
	}
	id, err = randomUUID()
	return id, false, err
}

// Deduplicator는 키별 최초 관측만 통과시킨다. 운영에서는 #33 SQLite 큐가 키 집합을
// 영속화한다. 동시 사용에 안전하지 않으므로 호출자가 직렬화한다.
type Deduplicator struct {
	deviceRef  string
	seen       map[string]struct{}
	suppressed int
}

// NewDeduplicator는 영속화된 키 집합(seen)으로 재시작할 수 있다.
func NewDeduplicator(deviceRef string, seen ...string) *Deduplicator {
	d := &Deduplicator{deviceRef: deviceRef, seen: make(map[string]struct{}, len(seen))}
	for _, k := range seen {
		d.seen[k] = struct{}{}
	}
	return d
}

// Accept는 처음 보는 키이거나 키가 없는 후보면 true다.
func (d *Deduplicator) Accept(c Candidate) bool {
	key := DedupKey(d.deviceRef, c)
	if key == "" {
		return true
	}
	if _, ok := d.seen[key]; ok {
		d.suppressed++
		return false
	}
	d.seen[key] = struct{}{}
	return true
}

// Suppressed는 중복으로 걸러진 후보 수다.
func (d *Deduplicator) Suppressed() int { return d.suppressed }

// Seen은 영속화용 키 목록을 반환한다(순서 무관).
func (d *Deduplicator) Seen() []string {
	keys := make([]string, 0, len(d.seen))
	for k := range d.seen {
		keys = append(keys, k)
	}
	return keys
}
