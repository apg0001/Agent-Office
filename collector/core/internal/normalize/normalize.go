package normalize

import (
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

// DefaultMaxFutureSkew는 원본 타임스탬프가 관측 시각보다 이만큼 넘게 미래이면 믿지 않는 기준이다.
// 원본 기록과 Collector는 같은 PC 시계를 쓰므로 작게 둔다.
const DefaultMaxFutureSkew = 5 * time.Minute

// Outcome은 후보 하나의 처리 결과다.
type Outcome string

const (
	OutcomeAccepted       Outcome = "accepted"
	OutcomeDuplicate      Outcome = "duplicate"
	OutcomeDroppedUnknown Outcome = "dropped_unknown"
	OutcomeDroppedInvalid Outcome = "dropped_invalid"
)

// Counters는 처리 결과 개수다. 값·식별자 원문을 담지 않는다.
type Counters struct {
	Accepted            int
	DuplicateSuppressed int
	// DroppedUnknown은 세션 참조가 없어 보낼 수 없는 unknown 이벤트 수다.
	DroppedUnknown int
	// DroppedInvalid는 어댑터 파싱 오류, v1 enum 밖 source, 식별자·시각 형식 오류,
	// sequence 발급 실패로 버린 수다.
	DroppedInvalid int
	// IgnoredLines는 활동이 아닌 transcript 줄(api-request-blob 등) 수다.
	IgnoredLines int
}

// Input은 정규화 입력이다.
type Input struct {
	Candidate adapter.Candidate
	// ObservedAt은 Collector가 입력을 받은 시각이다. 필수다.
	ObservedAt time.Time
	// SourceTime은 원본(transcript 줄 등)에서 파싱·검증한 시각이다. 없으면 영값이다.
	SourceTime time.Time
}

// Options는 Normalizer 설정이다.
type Options struct {
	// DeviceID는 서버에 등록된 장치 UUID다.
	DeviceID string
	// Sequences가 nil이면 메모리 저장소를 쓴다.
	Sequences SequenceStore
	// SeenKeys는 영속화된 중복 제거 키 집합이다(#33). 재시작 후 같은 키를 다시 보내지 않는다.
	SeenKeys []string
	// MaxFutureSkew가 0이면 DefaultMaxFutureSkew다.
	MaxFutureSkew time.Duration
}

// Normalizer는 후보를 이벤트 v1로 바꾼다. 동시 사용에 안전하지 않으므로 호출자가 직렬화한다.
type Normalizer struct {
	deviceID string
	seq      SequenceStore
	dedup    *adapter.Deduplicator
	skew     time.Duration
	counters Counters
}

// New는 Normalizer를 만든다. DeviceID가 UUID가 아니면 ErrInvalidDeviceID다.
func New(opts Options) (*Normalizer, error) {
	device := canonicalUUID(opts.DeviceID)
	if device == "" {
		return nil, ErrInvalidDeviceID
	}
	seq := opts.Sequences
	if seq == nil {
		seq = NewMemorySequenceStore()
	}
	skew := opts.MaxFutureSkew
	if skew <= 0 {
		skew = DefaultMaxFutureSkew
	}
	return &Normalizer{
		deviceID: device,
		seq:      seq,
		dedup:    adapter.NewDeduplicator(device, opts.SeenKeys...),
		skew:     skew,
	}, nil
}

// DeviceID는 정규화된 장치 UUID다.
func (n *Normalizer) DeviceID() string { return n.deviceID }

// Counters는 지금까지의 처리 결과 개수다.
func (n *Normalizer) Counters() Counters { return n.counters }

// SeenKeys는 영속화할 중복 제거 키 집합이다. 키에는 원본 참조가 들어 있으므로 로컬 큐(#33)에만
// 저장하고 서버·로그로 보내지 않는다.
func (n *Normalizer) SeenKeys() []string { return n.dedup.Seen() }

// Normalize는 후보 하나를 이벤트 v1로 바꾼다. OutcomeAccepted일 때만 Record가 유효하다.
//
//   - provider·event_type·activity가 v1 enum 밖이면 `unknown`, confidence는 `inferred`로 낮춘다.
//   - source가 v1 enum 밖이면 버린다(대체할 안전한 값이 없다).
//   - 세션 참조가 없으면 unknown 이벤트는 DroppedUnknown, 그 밖은 DroppedInvalid로 버린다.
//   - 검증 수준이 verified가 아니거나 source가 inferred면 confidence는 항상 `inferred`다.
//   - 이벤트를 새로 만들지 않는다(Codex tool.completed 합성, Stop 대체 등 없음).
func (n *Normalizer) Normalize(in Input) (Record, Outcome) {
	rec, out := n.normalize(in)
	switch out {
	case OutcomeAccepted:
		n.counters.Accepted++
	case OutcomeDuplicate:
		n.counters.DuplicateSuppressed++
	case OutcomeDroppedUnknown:
		n.counters.DroppedUnknown++
	default:
		n.counters.DroppedInvalid++
	}
	return rec, out
}

func (n *Normalizer) dropInvalid() { n.counters.DroppedInvalid++ }
func (n *Normalizer) ignoreLine()  { n.counters.IgnoredLines++ }

func (n *Normalizer) normalize(in Input) (Record, Outcome) {
	c := in.Candidate
	provider := enumOr(providers, string(c.Provider), "unknown")
	eventType := enumOr(eventTypes, string(c.EventType), "unknown")
	activity := enumOr(activities, string(c.Activity), "unknown")
	source := string(c.Source)
	if !sources[source] {
		return Record{}, OutcomeDroppedInvalid
	}
	if !adapter.ValidRef(c.SessionRef) {
		if eventType == "unknown" {
			return Record{}, OutcomeDroppedUnknown
		}
		return Record{}, OutcomeDroppedInvalid
	}
	if c.AgentRef != "" && !adapter.ValidRef(c.AgentRef) {
		return Record{}, OutcomeDroppedInvalid
	}

	confidence := "inferred"
	converted := provider != string(c.Provider) || eventType != string(c.EventType) || activity != string(c.Activity)
	if c.Confidence == adapter.ConfidenceObserved && c.Support == adapter.SupportVerified && source != "inferred" && !converted {
		confidence = "observed"
	}

	occurredAt, ok := n.occurredAt(in)
	if !ok {
		return Record{}, OutcomeDroppedInvalid
	}

	// 중복 제거와 event_id는 enum 정리를 마친 값으로 계산한다. unknown은 키가 없다.
	keyed := c
	keyed.Provider = adapter.Provider(provider)
	keyed.EventType = adapter.EventType(eventType)
	if ref := keyed.DedupRef(); ref != "" && !adapter.ValidRef(ref) {
		return Record{}, OutcomeDroppedInvalid
	}
	if !n.dedup.Accept(keyed) {
		return Record{}, OutcomeDuplicate
	}

	sessionID := SessionID(n.deviceID, provider, c.SessionRef)
	lead := LeadAgentID(n.deviceID, provider, c.SessionRef)
	agentID, parent := lead, (*string)(nil)
	if c.AgentRef != "" {
		agentID = SubagentID(n.deviceID, provider, c.SessionRef, c.AgentRef)
		parent = &lead
	}

	seq, err := n.seq.Next(n.deviceID, sessionID)
	if err != nil || seq < 0 || seq > MaxSequence {
		return Record{}, OutcomeDroppedInvalid
	}
	eventID, deterministic, err := adapter.EventID(n.deviceID, keyed)
	if err != nil {
		return Record{}, OutcomeDroppedInvalid
	}
	return Record{
		Event: Event{
			SchemaVersion: SchemaVersion,
			EventID:       eventID,
			DeviceID:      n.deviceID,
			SessionID:     sessionID,
			AgentID:       agentID,
			ParentAgentID: parent,
			Provider:      provider,
			Source:        source,
			EventType:     eventType,
			Activity:      activity,
			Confidence:    confidence,
			OccurredAt:    occurredAt,
			Sequence:      seq,
		},
		DeterministicID: deterministic,
	}, OutcomeAccepted
}

// occurredAt은 검증된 원본 시각, 없으면 관측 시각을 UTC RFC 3339로 반환한다.
func (n *Normalizer) occurredAt(in Input) (string, bool) {
	if !saneTime(in.ObservedAt) {
		return "", false
	}
	t := in.ObservedAt
	if saneTime(in.SourceTime) && !in.SourceTime.After(in.ObservedAt.Add(n.skew)) {
		t = in.SourceTime
	}
	return t.UTC().Format(time.RFC3339Nano), true
}

func saneTime(t time.Time) bool {
	if t.IsZero() {
		return false
	}
	y := t.UTC().Year()
	return y >= 2000 && y <= 9999
}
