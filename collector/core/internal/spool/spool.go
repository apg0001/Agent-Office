// Package spool은 `hook` 명령이 실행 중인 Collector에 Hook 후보를 넘기는 로컬 디렉터리다(#34).
//
// Hook 프로세스는 SQLite를 열지 않는다. 어댑터가 화이트리스트로 만든 후보(enum과 불투명 참조
// 식별자)만 파일 하나로 원자적으로 쓰고 바로 끝난다. 프롬프트·도구 입출력·경로 같은 Hook 원문은
// 디스크에 쓰지 않는다. 실행 중인 `run` 루프가 파일을 읽어 정규화·큐 기록 뒤 지운다.
package spool

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

const (
	// Version은 spool 파일 형식 버전이다.
	Version = 1
	// DefaultMaxFiles는 spool에 쌓아 둘 최대 파일 수다. Collector가 꺼져 있으면 넘는 Hook은 버린다.
	DefaultMaxFiles = 2000
	// MaxFileBytes는 spool 파일 하나의 상한이다.
	MaxFileBytes = 8 << 10
	fileSuffix   = ".json"
	tmpPrefix    = ".tmp-"
)

var (
	// ErrFull은 spool 파일 수가 상한에 닿았을 때다.
	ErrFull = errors.New("spool: full")
	// ErrInvalid는 spool 파일 형식이 틀렸을 때다(내용은 담지 않음).
	ErrInvalid = errors.New("spool: invalid record")
)

// Record는 spool 파일 한 개의 내용이다. 필드는 adapter.Candidate와 같다.
type Record struct {
	V            int       `json:"v"`
	ObservedAt   time.Time `json:"observed_at"`
	Provider     string    `json:"provider"`
	Source       string    `json:"source"`
	EventType    string    `json:"event_type"`
	SessionRef   string    `json:"session_ref"`
	TurnRef      string    `json:"turn_ref,omitempty"`
	ToolUseRef   string    `json:"tool_use_ref,omitempty"`
	AgentRef     string    `json:"agent_ref,omitempty"`
	ToolCategory string    `json:"tool_category,omitempty"`
	Activity     string    `json:"activity"`
	AgentType    string    `json:"agent_type,omitempty"`
	Confidence   string    `json:"confidence"`
	Support      string    `json:"support"`
	// Plan은 계획 도구 호출의 단계 상태 enum 목록이다(#38). 단계 문장은 담지 않는다.
	Plan *PlanRecord `json:"plan,omitempty"`
	// WorkspaceName은 작업 폴더 마지막 이름이다(#181). Hook이 PC별 동의를 확인하고 정리한 값만 쓴다.
	WorkspaceName string `json:"workspace_name,omitempty"`
}

// PlanRecord는 adapter.Plan의 spool 표현이다.
type PlanRecord struct {
	Steps   []string `json:"steps"`
	Support string   `json:"support"`
}

// FromCandidate는 후보를 spool 레코드로 바꾼다.
func FromCandidate(c adapter.Candidate, observedAt time.Time) Record {
	return Record{
		V: Version, ObservedAt: observedAt.UTC(),
		Provider: string(c.Provider), Source: string(c.Source), EventType: string(c.EventType),
		SessionRef: c.SessionRef, TurnRef: c.TurnRef, ToolUseRef: c.ToolUseRef, AgentRef: c.AgentRef,
		ToolCategory: string(c.ToolCategory), Activity: string(c.Activity), AgentType: string(c.AgentType),
		Confidence: string(c.Confidence), Support: string(c.Support),
		Plan: planRecord(c.Plan), WorkspaceName: workspaceName(c.WorkspaceName),
	}
}

// workspaceName은 정리된 이름만 통과시킨다. 아니면 빈 값이다(이벤트는 이름 없이 처리).
func workspaceName(s string) string {
	if adapter.ValidWorkspaceName(s) {
		return s
	}
	return ""
}

func planRecord(p *adapter.Plan) *PlanRecord {
	if p == nil || len(p.Steps) == 0 {
		return nil
	}
	return &PlanRecord{Steps: planSteps(p.Steps), Support: string(p.Support)}
}

// planSteps는 상태를 enum으로 다시 거르고 길이를 adapter.MaxPlanSteps로 자른다.
func planSteps(in []string) []string {
	if len(in) > adapter.MaxPlanSteps {
		in = in[:adapter.MaxPlanSteps]
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = adapter.PlanStatusOf(s)
	}
	return out
}

// Candidate는 레코드를 후보로 되돌린다. 참조 식별자는 adapter.ValidRef로 다시 검증한다
// (spool 파일은 같은 사용자만 쓸 수 있지만 2차 방어). enum 범위는 Normalizer가 확인한다.
func (r Record) Candidate() (adapter.Candidate, error) {
	if r.V != Version || r.ObservedAt.IsZero() || !adapter.ValidRef(r.SessionRef) {
		return adapter.Candidate{}, ErrInvalid
	}
	for _, ref := range []string{r.TurnRef, r.ToolUseRef, r.AgentRef} {
		if ref != "" && !adapter.ValidRef(ref) {
			return adapter.Candidate{}, ErrInvalid
		}
	}
	var plan *adapter.Plan
	if r.Plan != nil && len(r.Plan.Steps) > 0 {
		plan = &adapter.Plan{Steps: planSteps(r.Plan.Steps), Support: adapter.SupportLevel(r.Plan.Support)}
	}
	return adapter.Candidate{
		Provider: adapter.Provider(r.Provider), Source: adapter.Source(r.Source), EventType: adapter.EventType(r.EventType),
		SessionRef: r.SessionRef, TurnRef: r.TurnRef, ToolUseRef: r.ToolUseRef, AgentRef: r.AgentRef,
		ToolCategory: adapter.ToolCategory(r.ToolCategory), Activity: adapter.Activity(r.Activity),
		AgentType: adapter.AgentTypeCategory(r.AgentType), Confidence: adapter.Confidence(r.Confidence),
		Support: adapter.SupportLevel(r.Support), Plan: plan, WorkspaceName: workspaceName(r.WorkspaceName),
	}, nil
}

// Write는 레코드를 dir에 원자적으로 쓴다(임시 파일 → rename). 파일 이름은 관측 시각 순서다.
func Write(dir string, r Record, maxFiles int) error {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("spool: create directory failed")
	}
	if n, err := count(dir, maxFiles); err != nil {
		return err
	} else if n >= maxFiles {
		return ErrFull
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > MaxFileBytes {
		return ErrInvalid
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return errors.New("spool: random failed")
	}
	name := fmt.Sprintf("%020d-%s%s", r.ObservedAt.UnixNano(), hex.EncodeToString(rnd[:]), fileSuffix)
	tmp := filepath.Join(dir, tmpPrefix+name)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		_ = os.Remove(tmp)
		return errors.New("spool: write failed")
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return errors.New("spool: rename failed")
	}
	return nil
}

func count(dir string, limit int) (int, error) {
	d, err := os.Open(dir)
	if err != nil {
		return 0, errors.New("spool: open directory failed")
	}
	defer d.Close()
	n := 0
	for n < limit {
		names, err := d.Readdirnames(256)
		n += len(names)
		if err == io.EOF || len(names) == 0 {
			break
		}
		if err != nil {
			return 0, errors.New("spool: read directory failed")
		}
	}
	return n, nil
}

// Entry는 읽은 spool 파일이다.
type Entry struct {
	Name   string
	Record Record
	Err    error
}

// List는 오래된 순으로 최대 limit개의 완성된 spool 파일을 읽는다. 1시간 넘은 임시 파일은 지운다.
func List(dir string, limit int, now time.Time) ([]Entry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("spool: read directory failed")
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, tmpPrefix) {
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		if e.Type().IsRegular() && strings.HasSuffix(name, fileSuffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > limit {
		names = names[:limit]
	}
	out := make([]Entry, 0, len(names))
	for _, name := range names {
		out = append(out, read(dir, name))
	}
	return out, nil
}

func read(dir, name string) Entry {
	e := Entry{Name: name}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		e.Err = ErrInvalid
		return e
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil || len(data) > MaxFileBytes || json.Unmarshal(data, &e.Record) != nil {
		e.Err = ErrInvalid
	}
	return e
}

// Remove는 처리한 spool 파일을 지운다.
func Remove(dir, name string) {
	_ = os.Remove(filepath.Join(dir, filepath.Base(name)))
}
