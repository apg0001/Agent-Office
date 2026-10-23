package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var (
	// ErrParse는 설정 파일이 JSON 객체가 아니거나 중복 키·뒤따르는 값이 있을 때다. 쓰지 않는다.
	ErrParse = errors.New("hooks: settings file is not a valid JSON object")
	// ErrShape는 `hooks`나 이벤트 값이 예상한 타입(객체·배열)이 아닐 때다. 쓰지 않는다.
	ErrShape = errors.New("hooks: unexpected hooks section shape")
)

// member·object는 키 순서를 보존하는 JSON 객체다. 값은 원문(RawMessage) 그대로 둔다.
type member struct {
	key string
	val json.RawMessage
}

type object []member

func parseObject(raw []byte) (object, error) {
	if !json.Valid(raw) {
		return nil, ErrParse
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, ErrParse
	}
	obj := object{}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok || seen[key] {
			return nil, ErrParse
		}
		seen[key] = true
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, ErrParse
		}
		obj = append(obj, member{key, v})
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, ErrParse
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, ErrParse
	}
	return obj, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, m := range o {
		if m.key == key {
			return m.val, true
		}
	}
	return nil, false
}

func (o object) set(key string, val json.RawMessage) object {
	for i := range o {
		if o[i].key == key {
			o[i].val = val
			return o
		}
	}
	return append(o, member{key, val})
}

func (o object) del(key string) object {
	out := o[:0:0]
	for _, m := range o {
		if m.key != key {
			out = append(out, m)
		}
	}
	return out
}

func (o object) marshal() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encode(m.key))
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func parseArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || arr == nil {
		return nil, false
	}
	return arr, true
}

func marshalArray(arr []json.RawMessage) json.RawMessage {
	parts := make([]string, len(arr))
	for i, a := range arr {
		parts[i] = string(a)
	}
	return json.RawMessage("[" + strings.Join(parts, ",") + "]")
}

// handlerInfo는 핸들러에서 소유 판정에 필요한 필드만 읽는다.
type handlerInfo struct {
	Type    string      `json:"type"`
	Command string      `json:"command"`
	Timeout json.Number `json:"timeout"`
}

func readHandler(raw json.RawMessage) (handlerInfo, bool) {
	var h handlerInfo
	if bytes.TrimSpace(raw)[0] != '{' || json.Unmarshal(raw, &h) != nil {
		return handlerInfo{}, false
	}
	return h, true
}

// group은 이벤트 배열의 원소 하나(matcher 그룹)다.
type group struct {
	obj      object
	matcher  string
	handlers []json.RawMessage
	ok       bool // 객체이고 hooks 배열이 있다
}

func readGroup(raw json.RawMessage) group {
	obj, err := parseObject(raw)
	if err != nil {
		return group{}
	}
	g := group{obj: obj}
	if m, ok := obj.get("matcher"); ok {
		_ = json.Unmarshal(m, &g.matcher)
	}
	hs, ok := obj.get("hooks")
	if !ok {
		return g
	}
	g.handlers, g.ok = parseArray(hs)
	return g
}

// eventState는 이벤트 하나의 소유 항목 상태다.
type eventState struct {
	current int // 기대값과 같은 소유 핸들러
	other   int // 소유지만 명령·타임아웃·matcher가 다른 핸들러
	foreign int // 소유가 아닌 핸들러
}

func (s Spec) classify(e Event, g group, h json.RawMessage) (owned, current bool) {
	info, ok := readHandler(h)
	if !ok || info.Type != "command" || !IsOwnedCommand(info.Command, s.Tool) {
		return false, false
	}
	return true, info.Command == s.Command && info.Timeout.String() == itoa(s.Timeout) && g.matcher == e.Matcher
}

func itoa(n int) string { return string(encode(n)) }

// document는 파싱된 설정 파일이다.
type document struct {
	top   object
	hooks object
	had   bool // 원래 hooks 키가 있었다
}

func parseDocument(data []byte) (document, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return document{top: object{}}, nil
	}
	top, err := parseObject(data)
	if err != nil {
		return document{}, err
	}
	d := document{top: top}
	if raw, ok := top.get("hooks"); ok {
		d.had = true
		if d.hooks, err = parseObject(raw); err != nil {
			return document{}, ErrShape
		}
	}
	return d, nil
}

func (d document) bytes() []byte {
	return pretty(d.top.marshal())
}

// eventArray는 이벤트 값을 배열로 읽는다. 없으면 빈 배열이고, 배열이 아니면 ErrShape다.
func (d document) eventArray(name string) ([]json.RawMessage, bool, error) {
	raw, ok := d.hooks.get(name)
	if !ok {
		return nil, false, nil
	}
	arr, isArr := parseArray(raw)
	if !isArr {
		return nil, true, ErrShape
	}
	return arr, true, nil
}

// removeOwned는 이벤트 배열에서 소유 핸들러를 지운다. 비게 된 그룹은 지우고 남의 그룹은 그대로 둔다.
func (s Spec) removeOwned(e Event, arr []json.RawMessage) ([]json.RawMessage, int) {
	out := make([]json.RawMessage, 0, len(arr))
	removed := 0
	for _, raw := range arr {
		g := readGroup(raw)
		if !g.ok {
			out = append(out, raw)
			continue
		}
		kept := make([]json.RawMessage, 0, len(g.handlers))
		for _, h := range g.handlers {
			if owned, _ := s.classify(e, g, h); owned {
				removed++
				continue
			}
			kept = append(kept, h)
		}
		switch {
		case len(kept) == len(g.handlers):
			out = append(out, raw)
		case len(kept) == 0:
			// 소유 핸들러만 있던 그룹은 통째로 지운다.
		default:
			out = append(out, g.obj.set("hooks", marshalArray(kept)).marshal())
		}
	}
	return out, removed
}

func (s Spec) inspectEvent(e Event, arr []json.RawMessage) eventState {
	var st eventState
	for _, raw := range arr {
		g := readGroup(raw)
		if !g.ok {
			st.foreign++
			continue
		}
		for _, h := range g.handlers {
			owned, cur := s.classify(e, g, h)
			switch {
			case cur:
				st.current++
			case owned:
				st.other++
			default:
				st.foreign++
			}
		}
	}
	return st
}

// Action은 이벤트 하나의 변경 종류다.
type Action string

const (
	ActionAdd     Action = "add"
	ActionReplace Action = "replace"
	ActionRemove  Action = "remove"
)

// Change는 이벤트 하나의 변경이다.
type Change struct {
	Event  string
	Action Action
}

// planInstall은 설치 후 문서와 변경 목록을 만든다. 변경이 없으면 nil 문서다.
func (s Spec) planInstall(data []byte) ([]byte, []Change, error) {
	d, err := parseDocument(data)
	if err != nil {
		return nil, nil, err
	}
	var changes []Change
	for _, e := range s.Events {
		arr, _, err := d.eventArray(e.Name)
		if err != nil {
			return nil, nil, err
		}
		st := s.inspectEvent(e, arr)
		if st.current == 1 && st.other == 0 {
			continue
		}
		action := ActionAdd
		if st.current+st.other > 0 {
			action = ActionReplace
		}
		arr, _ = s.removeOwned(e, arr)
		arr = append(arr, s.groupJSON(e))
		d.hooks = d.hooks.set(e.Name, marshalArray(arr))
		changes = append(changes, Change{Event: e.Name, Action: action})
	}
	if len(changes) == 0 {
		return nil, nil, nil
	}
	d.top = d.top.set("hooks", d.hooks.marshal())
	return d.bytes(), changes, nil
}

// planUninstall은 모든 이벤트에서 소유 핸들러를 지운다. 이 제거로 비게 된 이벤트 키와
// hooks 키만 지운다(원래 비어 있던 것은 건드리지 않음).
func (s Spec) planUninstall(data []byte) ([]byte, []Change, error) {
	d, err := parseDocument(data)
	if err != nil {
		return nil, nil, err
	}
	var changes []Change
	for _, m := range append(object(nil), d.hooks...) {
		arr, isArr := parseArray(m.val)
		if !isArr {
			continue
		}
		arr, removed := s.removeOwned(Event{Name: m.key}, arr)
		if removed == 0 {
			continue
		}
		if len(arr) == 0 {
			d.hooks = d.hooks.del(m.key)
		} else {
			d.hooks = d.hooks.set(m.key, marshalArray(arr))
		}
		changes = append(changes, Change{Event: m.key, Action: ActionRemove})
	}
	if len(changes) == 0 {
		return nil, nil, nil
	}
	if len(d.hooks) == 0 {
		d.top = d.top.del("hooks")
	} else {
		d.top = d.top.set("hooks", d.hooks.marshal())
	}
	return d.bytes(), changes, nil
}

// State는 설치 상태다.
type State string

const (
	StateInstalled    State = "installed"
	StateNotInstalled State = "not_installed"
	// StatePartial: 일부 이벤트에만 소유 항목이 있다.
	StatePartial State = "partial"
	// StateModified: 소유 항목의 명령·타임아웃·matcher가 기대와 다르다(실행 파일 이동, 수동 수정).
	StateModified State = "modified"
	// StateUnreadable: 설정 파일을 읽거나 파싱할 수 없다.
	StateUnreadable State = "unreadable"
)

// Status는 설정 파일의 소유 항목 요약이다(경로·명령 원문 없음).
type Status struct {
	State State
	// Current·Missing·Modified는 기대 이벤트 개수 기준이다.
	Current, Missing, Modified int
	// Foreign은 hooks 전체에서 소유가 아닌 핸들러 수다(보존 대상).
	Foreign int
}

func (s Spec) inspect(data []byte) (Status, error) {
	d, err := parseDocument(data)
	if err != nil {
		return Status{State: StateUnreadable}, err
	}
	var st Status
	expected := map[string]bool{}
	for _, e := range s.Events {
		expected[e.Name] = true
		arr, _, err := d.eventArray(e.Name)
		if err != nil {
			return Status{State: StateUnreadable}, err
		}
		es := s.inspectEvent(e, arr)
		st.Foreign += es.foreign
		switch {
		case es.current == 1 && es.other == 0:
			st.Current++
		case es.current+es.other > 0:
			st.Modified++
		default:
			st.Missing++
		}
	}
	for _, m := range d.hooks {
		if expected[m.key] {
			continue
		}
		arr, ok := parseArray(m.val)
		if !ok {
			continue
		}
		es := s.inspectEvent(Event{Name: m.key}, arr)
		st.Foreign += es.foreign
		if es.current+es.other > 0 {
			st.Modified++
		}
	}
	switch {
	case st.Current == len(s.Events) && st.Modified == 0:
		st.State = StateInstalled
	case st.Current == 0 && st.Modified == 0:
		st.State = StateNotInstalled
	case st.Modified > 0:
		st.State = StateModified
	default:
		st.State = StatePartial
	}
	return st, nil
}
