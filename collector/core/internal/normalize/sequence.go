package normalize

import (
	"errors"
	"sync"
)

// ErrSequenceExhausted는 세션 sequence가 JavaScript 안전 정수 상한에 닿았을 때 반환한다.
var ErrSequenceExhausted = errors.New("normalize: sequence exhausted")

// SequenceStore는 (장치, 세션)별 단조 증가 sequence를 발급·영속화한다. 키는 정규화된
// UUID뿐이므로 저장소에 원본 제공자 ID가 남지 않는다. 운영 구현은 #33 SQLite 큐가 맡고,
// 같은 트랜잭션에서 중복 제거 키·큐 기록과 함께 커밋해야 한다.
type SequenceStore interface {
	// Next는 다음 값을 반환하고 기록한다. 첫 값은 0이다.
	Next(deviceID, sessionID string) (int64, error)
}

// MemorySequenceStore는 테스트·임시 실행용 메모리 구현이다. 동시 사용에 안전하다.
type MemorySequenceStore struct {
	mu   sync.Mutex
	next map[string]int64
}

// NewMemorySequenceStore는 빈 저장소를 만든다.
func NewMemorySequenceStore() *MemorySequenceStore {
	return &MemorySequenceStore{next: map[string]int64{}}
}

// Next는 SequenceStore 구현이다.
func (s *MemorySequenceStore) Next(deviceID, sessionID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := deviceID + "|" + sessionID
	v := s.next[key]
	if v > MaxSequence {
		return 0, ErrSequenceExhausted
	}
	s.next[key] = v + 1
	return v, nil
}

// Set은 다음에 발급할 값을 지정한다(영속 값 복원·경계 테스트용).
func (s *MemorySequenceStore) Set(deviceID, sessionID string, next int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next[deviceID+"|"+sessionID] = next
}
