package normalize

import (
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/codex"
	"github.com/apg0001/Agent-Office/collector/core/internal/tail"
)

// Pipeline은 Hook payload와 transcript tail 결과를 어댑터와 Normalizer에 차례로 넘기는
// 동기식 조립 단위다. 고루틴·polling 주기·큐·전송은 없다(#33, #34).
// 동시 사용에 안전하지 않으므로 호출자가 한 고루틴에서 쓴다.
type Pipeline struct {
	n *Normalizer
}

// NewPipeline은 Normalizer를 감싼다.
func NewPipeline(n *Normalizer) *Pipeline { return &Pipeline{n: n} }

// Counters는 Normalizer 결과와 어댑터 단계의 무시·오류 개수를 합친 값이다.
func (p *Pipeline) Counters() Counters { return p.n.Counters() }

// Normalizer는 내부 Normalizer다(SeenKeys 영속화용).
func (p *Pipeline) Normalizer() *Normalizer { return p.n }

// Hook은 Hook stdin payload 한 건을 처리한다. 어댑터 오류는 DroppedInvalid로만 센다(fail-open).
func (p *Pipeline) Hook(provider adapter.Provider, rt adapter.Runtime, payload []byte, observedAt time.Time) []Record {
	var (
		c   adapter.Candidate
		err error
	)
	switch provider {
	case adapter.ProviderClaude:
		c, err = claude.ParseHook(payload, rt)
	case adapter.ProviderCodex:
		c, err = codex.ParseHook(payload, rt)
		c = guardCodexCompletion(c, rt)
	default:
		p.n.dropInvalid()
		return nil
	}
	if err != nil {
		p.n.dropInvalid()
		return nil
	}
	return p.emit(nil, Input{Candidate: c, ObservedAt: observedAt})
}

// Transcript는 Claude transcript tail 결과의 줄을 처리한다. 줄의 `timestamp`가 시간대가 있는
// RFC 3339 값이고 관측 시각보다 미래가 아니면 occurred_at으로 쓰고, 아니면 관측 시각을 쓴다.
func (p *Pipeline) Transcript(res tail.Result, rt adapter.Runtime, observedAt time.Time) []Record {
	var out []Record
	for _, line := range res.Records {
		cs, err := claude.ParseTranscriptLine(line, rt)
		if err != nil {
			p.n.dropInvalid()
			continue
		}
		if len(cs) == 0 {
			p.n.ignoreLine()
			continue
		}
		sourceTime := transcriptTime(line)
		for _, c := range cs {
			out = p.emit(out, Input{Candidate: c, ObservedAt: observedAt, SourceTime: sourceTime})
		}
	}
	return out
}

func (p *Pipeline) emit(out []Record, in Input) []Record {
	if rec, outcome := p.n.Normalize(in); outcome == OutcomeAccepted {
		out = append(out, rec)
	}
	return out
}

// guardCodexCompletion은 Desktop이 아닌 Codex 런타임의 도구 완료를 unknown으로 낮춘다.
// codex.ParseHook이 이미 같은 규칙을 적용하지만(#17: CLI PostToolUse 미관측) 어댑터 변경에도
// tool.completed를 만들지 않도록 한 번 더 막는다.
func guardCodexCompletion(c adapter.Candidate, rt adapter.Runtime) adapter.Candidate {
	if rt.Product == codex.ProductDesktop {
		return c
	}
	if c.EventType != adapter.EventToolCompleted && c.EventType != adapter.EventToolFailed {
		return c
	}
	return adapter.Candidate{
		Provider:   c.Provider,
		Source:     c.Source,
		EventType:  adapter.EventUnknown,
		SessionRef: c.SessionRef,
		TurnRef:    c.TurnRef,
		Activity:   adapter.ActivityUnknown,
		Confidence: adapter.ConfidenceInferred,
		Support:    adapter.SupportUnverified,
	}
}

// transcriptTime은 줄 최상위 `timestamp`를 파싱한다. 형식이 아니면 영값이다.
// 값 원문은 반환·기록하지 않는다.
func transcriptTime(line []byte) time.Time {
	obj, err := adapter.DecodeObject(line, adapter.MaxTranscriptLineBytes)
	if err != nil {
		return time.Time{}
	}
	s := obj.String("timestamp")
	if s == "" || len(s) > 40 {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
