package normalize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
)

var schemaPath = filepath.Join("..", "..", "..", "..", "packages", "event-schema", "schema", "event-v1.schema.json")

// eventSchema는 event-v1.schema.json 중 이 테스트가 직접 검사하는 부분이다(외부 라이브러리 없음).
type eventSchema struct {
	AdditionalProperties *bool                     `json:"additionalProperties"`
	Required             []string                  `json:"required"`
	Properties           map[string]schemaProperty `json:"properties"`
}

type schemaProperty struct {
	Type      any              `json:"type"`
	Const     *string          `json:"const"`
	Enum      []string         `json:"enum"`
	Format    string           `json:"format"`
	MinLength *int             `json:"minLength"`
	MaxLength *int             `json:"maxLength"`
	Pattern   string           `json:"pattern"`
	Minimum   *float64         `json:"minimum"`
	Maximum   *float64         `json:"maximum"`
	OneOf     []schemaProperty `json:"oneOf"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func loadSchema(t *testing.T) eventSchema {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var s eventSchema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.AdditionalProperties == nil || *s.AdditionalProperties {
		t.Fatal("schema must forbid additional properties")
	}
	return s
}

// validateEvent는 직렬화한 이벤트 하나를 스키마로 검사한다. 오류 메시지에는 필드 이름만 쓴다.
func validateEvent(s eventSchema, raw []byte) error {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	for k := range obj {
		if _, ok := s.Properties[k]; !ok {
			return fmt.Errorf("extra field %s", k)
		}
	}
	for _, k := range s.Required {
		v, ok := obj[k]
		if !ok {
			return fmt.Errorf("missing %s", k)
		}
		if err := checkValue(s.Properties[k], v); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	if obj["source"] == "inferred" && obj["confidence"] != "inferred" {
		return fmt.Errorf("source inferred requires confidence inferred")
	}
	return nil
}

func checkValue(p schemaProperty, v any) error {
	if len(p.OneOf) > 0 {
		for _, alt := range p.OneOf {
			if checkValue(alt, v) == nil {
				return nil
			}
		}
		return fmt.Errorf("no oneOf branch matched")
	}
	switch p.Type {
	case "null":
		if v != nil {
			return fmt.Errorf("want null")
		}
		return nil
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("want integer")
		}
		i, err := n.Int64()
		if err != nil {
			return fmt.Errorf("want integer")
		}
		if p.Minimum != nil && float64(i) < *p.Minimum || p.Maximum != nil && float64(i) > *p.Maximum {
			return fmt.Errorf("out of range")
		}
		return nil
	}
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("want string")
	}
	if p.Const != nil && str != *p.Const {
		return fmt.Errorf("const mismatch")
	}
	if len(p.Enum) > 0 && !contains(p.Enum, str) {
		return fmt.Errorf("not in enum")
	}
	if p.MinLength != nil && len(str) < *p.MinLength || p.MaxLength != nil && len(str) > *p.MaxLength {
		return fmt.Errorf("length")
	}
	if p.Pattern != "" && !regexp.MustCompile(p.Pattern).MatchString(str) {
		return fmt.Errorf("pattern")
	}
	switch p.Format {
	case "uuid":
		if !uuidPattern.MatchString(str) {
			return fmt.Errorf("uuid format")
		}
	case "date-time":
		if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
			return fmt.Errorf("date-time format")
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sampleEvent() Event {
	parent := "00000000-0000-4000-8000-000000000002"
	return Event{
		SchemaVersion: SchemaVersion,
		EventID:       "00000000-0000-4000-8000-000000000003",
		DeviceID:      "00000000-0000-4000-8000-000000000004",
		SessionID:     "00000000-0000-4000-8000-000000000005",
		AgentID:       "00000000-0000-4000-8000-000000000006",
		ParentAgentID: &parent,
		Provider:      "claude",
		Source:        "hook",
		EventType:     "tool.started",
		Activity:      "reading",
		Confidence:    "observed",
		OccurredAt:    "2026-10-23T00:00:00Z",
		Sequence:      0,
	}
}

func TestEventKeySetEqualsSchemaRequiredAndProperties(t *testing.T) {
	s := loadSchema(t)
	marshalKeys := func(ev Event) []string {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatal(err)
		}
		return keys(obj)
	}
	required := append([]string(nil), s.Required...)
	sort.Strings(required)
	// 선택 필드가 없으면 필수 필드만, 있으면 스키마 속성 전부(plan_steps #38, workspace_name #181).
	for _, ev := range []Event{sampleEvent(), {}} {
		if got := marshalKeys(ev); fmt.Sprint(got) != fmt.Sprint(required) {
			t.Fatalf("event keys %v, required %v", got, required)
		}
	}
	withPlan := sampleEvent()
	withPlan.EventType, withPlan.PlanSteps = "plan.updated", []string{"completed", "pending"}
	withPlan.WorkspaceName = "Agent-Office" // 선택 필드(#181)
	if got := marshalKeys(withPlan); fmt.Sprint(got) != fmt.Sprint(keys(s.Properties)) {
		t.Fatalf("event keys %v, properties %v", got, keys(s.Properties))
	}
	if err := validateEvent(s, mustMarshal(t, sampleEvent())); err != nil {
		t.Fatal(err)
	}
	root := sampleEvent()
	root.ParentAgentID = nil
	if err := validateEvent(s, mustMarshal(t, root)); err != nil {
		t.Fatalf("root parent null: %v", err)
	}
}

func TestValidatorRejectsContractViolations(t *testing.T) {
	s := loadSchema(t)
	bad := map[string]func(*Event){
		"enum":        func(e *Event) { e.EventType = "tool.exec" },
		"uuid":        func(e *Event) { e.SessionID = "raw-session" },
		"timezone":    func(e *Event) { e.OccurredAt = "2026-10-23T00:00:00" },
		"version":     func(e *Event) { e.SchemaVersion = "2.0" },
		"sequence":    func(e *Event) { e.Sequence = -1 },
		"inferredsrc": func(e *Event) { e.Source = "inferred" },
	}
	for name, mutate := range bad {
		ev := sampleEvent()
		mutate(&ev)
		if validateEvent(s, mustMarshal(t, ev)) == nil {
			t.Errorf("%s: violation accepted", name)
		}
	}
	extra := []byte(`{"schema_version":"1.0","prompt":"x"}`)
	if validateEvent(s, extra) == nil {
		t.Fatal("extra field accepted")
	}
}

func TestPackageEnumsMatchSchema(t *testing.T) {
	s := loadSchema(t)
	for field, values := range map[string]map[string]bool{
		"provider": providers, "source": sources, "event_type": eventTypes,
		"activity": activities, "confidence": confidences,
	} {
		want := append([]string(nil), s.Properties[field].Enum...)
		sort.Strings(want)
		if fmt.Sprint(keys(values)) != fmt.Sprint(want) {
			t.Fatalf("%s: package %v schema %v", field, keys(values), want)
		}
	}
	if c := s.Properties["schema_version"].Const; c == nil || *c != SchemaVersion {
		t.Fatal("schema_version const mismatch")
	}
	if m := s.Properties["sequence"].Maximum; m == nil || int64(*m) != MaxSequence {
		t.Fatal("sequence maximum mismatch")
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Collector가 만드는 작업 폴더 이름은 모두 스키마 `workspace_name`을 통과하고, 경로·제어 문자는 거부된다(#181).
func TestWorkspaceNameMatchesSchema(t *testing.T) {
	var raw struct {
		Properties map[string]struct {
			MinLength int `json:"minLength"`
			MaxLength int `json:"maxLength"`
			Not       struct {
				AnyOf []struct {
					Enum    []string `json:"enum"`
					Pattern string   `json:"pattern"`
				} `json:"anyOf"`
			} `json:"not"`
		} `json:"properties"`
	}
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	p, ok := raw.Properties["workspace_name"]
	if !ok || len(p.Not.AnyOf) != 2 || p.Not.AnyOf[1].Pattern == "" || p.MaxLength != adapter.MaxWorkspaceNameRunes || p.MinLength != 1 {
		t.Fatal("workspace_name schema mismatch")
	}
	forbidden := regexp.MustCompile(p.Not.AnyOf[1].Pattern)
	valid := func(s string) bool {
		n := utf8.RuneCountInString(s)
		return n >= p.MinLength && n <= p.MaxLength && !forbidden.MatchString(s) && !contains(p.Not.AnyOf[0].Enum, s)
	}
	for _, in := range []string{
		"/a/Agent-Office", `C:\x\blog`, "/a/" + strings.Repeat("가", 80), "/a/x\u200by", "/a/\u00adz\ufeff",
		"/a/ name ", "/a/\u0085w", "/a/\u2066rtl\u2069", "/a/emoji\U0001F600",
	} {
		got := adapter.WorkspaceName(in)
		if got == "" || !valid(got) {
			t.Errorf("collector output %q rejected by schema", got)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "a\x00", "a\n", "a\u007f", "a\u0085", "a\u200b", "a\u202e", "a\u2028", "a\ufeff", strings.Repeat("x", 65)} {
		if valid(bad) {
			t.Errorf("schema accepted %q", bad)
		}
	}
}
