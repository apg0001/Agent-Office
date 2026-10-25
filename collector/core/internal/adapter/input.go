package adapter

import (
	"bytes"
	"encoding/json"
	"errors"
)

// 입력 크기 상한. 초과 입력은 파싱하지 않고 ErrTooLarge를 반환한다(fail-open).
const (
	// MaxHookPayloadBytes는 Hook stdin 한 건의 상한이다. tool_response가 커도 대부분 들어간다.
	MaxHookPayloadBytes = 8 << 20
	// MaxTranscriptLineBytes는 transcript 한 줄의 상한이다(#20 tail 규칙과 같은 1MiB).
	MaxTranscriptLineBytes = 1 << 20
	// MaxRefLength는 식별자 최대 길이다.
	MaxRefLength = 128
)

// 오류는 고정 문자열 sentinel만 쓴다. 입력 값이나 encoding/json 오류 메시지(입력 문자 포함
// 가능)를 감싸지 않으므로 오류 문자열로 원문이 새지 않는다.
var (
	ErrTooLarge       = errors.New("adapter: input exceeds size limit")
	ErrMalformed      = errors.New("adapter: input is not a JSON object")
	ErrMissingSession = errors.New("adapter: missing or invalid session identifier")
	ErrMissingRef     = errors.New("adapter: missing or invalid activity identifier")
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Object는 최상위 키를 대소문자까지 정확히 일치시켜 읽는 JSON 객체다.
// encoding/json 구조체 디코딩은 키를 대소문자 무시로 일치시키므로 쓰지 않는다.
type Object map[string]json.RawMessage

// DecodeObject는 크기를 확인한 뒤 JSON 객체 하나를 읽는다. BOM과 앞뒤 공백은 허용한다.
func DecodeObject(data []byte, limit int) (Object, error) {
	if len(data) > limit {
		return nil, ErrTooLarge
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	var obj Object
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return nil, ErrMalformed
	}
	return obj, nil
}

// String은 값이 비어 있지 않은 JSON 문자열일 때만 반환한다(참조 구현의 _string).
func (o Object) String(key string) string {
	raw, ok := o[key]
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// Ref는 String 값 중 ValidRef를 통과한 식별자만 반환한다.
func (o Object) Ref(key string) string {
	s := o.String(key)
	if !ValidRef(s) {
		return ""
	}
	return s
}

// IsTrue는 값이 JSON true 리터럴일 때만 true다(참조 구현의 `is True`).
func (o Object) IsTrue(key string) bool {
	return bytes.Equal(bytes.TrimSpace(o[key]), []byte("true"))
}

// Has는 키 존재 여부다.
func (o Object) Has(key string) bool {
	_, ok := o[key]
	return ok
}

// Object는 값이 JSON 객체일 때만 반환한다.
func (o Object) Object(key string) (Object, bool) {
	raw, ok := o[key]
	if !ok {
		return nil, false
	}
	var obj Object
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

// Objects는 값이 배열일 때 객체 원소만 반환한다. 객체가 아닌 원소는 건너뛴다.
func (o Object) Objects(key string) []Object {
	raw, ok := o[key]
	if !ok {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]Object, 0, len(items))
	for _, item := range items {
		var obj Object
		if json.Unmarshal(item, &obj) == nil && obj != nil {
			out = append(out, obj)
		}
	}
	return out
}

// ValidRef는 불투명 식별자 형식인지 확인한다. 공백·경로 구분자·따옴표 등을 포함한 값은
// 식별자가 아니라 원문일 수 있으므로 거부한다.
func ValidRef(s string) bool {
	if s == "" || len(s) > MaxRefLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}
