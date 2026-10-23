package tail

import (
	"encoding/json"
	"errors"
)

// CheckpointVersion은 직렬화 형식 버전이다. 형식이 바뀌면 올리고 이전 값은 처음부터 다시 읽는다.
const CheckpointVersion = 1

// ErrCheckpointVersion은 저장된 체크포인트가 이 빌드가 읽을 수 없는 버전일 때 반환한다.
// 호출자는 빈 체크포인트로 처음부터 다시 읽고, 중복은 event_id 멱등성(#29·#33)이 막는다.
var ErrCheckpointVersion = errors.New("tail: unsupported checkpoint version")

// ErrCheckpointMalformed는 저장된 체크포인트 JSON을 읽을 수 없을 때 반환한다(입력 문자는 포함하지 않음).
var ErrCheckpointMalformed = errors.New("tail: malformed checkpoint")

// FileID는 파일 식별자다. POSIX는 (st_dev, st_ino), Windows는 (볼륨 일련번호, 64비트 파일 인덱스)다.
type FileID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

// Checkpoint는 파일 하나의 읽기 위치다. 로컬 큐(#33)에 영속화하는 단위이며 줄 내용은 담지 않는다.
type Checkpoint struct {
	Version int `json:"version"`
	// File은 마지막으로 읽은 파일의 식별자다. nil이면 아직 읽은 적이 없다.
	File *FileID `json:"file,omitempty"`
	// Offset은 소비한 바이트 수다. 완결 줄 경계이거나 건너뛰는 초과 줄 안이다.
	Offset int64 `json:"offset"`
	// PrefixDigest는 앞부분 최대 PrefixBytes 바이트의 SHA-256 hex다. Offset이 0이면 비어 있다.
	PrefixDigest string `json:"prefix_digest,omitempty"`
	// SkippingOversizedLine이면 다음 개행까지 버린다.
	SkippingOversizedLine bool `json:"skipping_oversized_line,omitempty"`
}

// MarshalCheckpoint는 체크포인트를 버전이 붙은 JSON으로 직렬화한다.
func MarshalCheckpoint(cp Checkpoint) ([]byte, error) {
	cp.Version = CheckpointVersion
	return json.Marshal(cp)
}

// UnmarshalCheckpoint는 저장된 JSON을 읽는다. 버전이 다르면 ErrCheckpointVersion이다.
func UnmarshalCheckpoint(data []byte) (Checkpoint, error) {
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return Checkpoint{}, ErrCheckpointMalformed
	}
	if cp.Version != CheckpointVersion {
		return Checkpoint{}, ErrCheckpointVersion
	}
	if cp.Offset < 0 {
		return Checkpoint{}, ErrCheckpointMalformed
	}
	return cp, nil
}
