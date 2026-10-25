# Collector transcript 탐색·tail(Go)

관련 이슈: #30 (선행: #20 tail 규칙, #29 어댑터)

Claude transcript JSONL을 polling으로 찾고, 새로 append된 완결 줄만 읽어 어댑터(`claude.ParseTranscriptLine`)에 넘기는 Go 패키지입니다. Hook 설치(#31), 이벤트 v1 정규화(#32), SQLite 큐·전송(#33), OS 서비스(#34)는 이 범위가 아닙니다.

## 패키지

`collector/core/internal/tail`(표준 라이브러리 전용)

| API | 역할 |
|---|---|
| `New(path, Checkpoint, Options) *Tailer`, `Poll() Result`, `Checkpoint()`, `Close()` | 경로 하나를 tail. 같은 파일을 읽는 동안 읽기 전용 핸들을 유지 |
| `PollOnce(path, Checkpoint, Options) Result` | 핸들을 유지하지 않는 1회 읽기(참조 구현 `poll`과 같은 동작) |
| `MarshalCheckpoint`/`UnmarshalCheckpoint` | 버전형 JSON 체크포인트 |
| `Discover(root, DiscoverOptions) (Discovery, error)` | `*.jsonl` 후보 탐색 |

`Result`는 `Records [][]byte`(JSON 객체로 검증된 줄 바이트, BOM·CR 제거), `Status`(`ok`·`missing`·`permission_denied`·`unsupported`·`error`), `Reset`(`rotated`·`truncated`·`rewritten`), `InvalidLines`, `OversizedLines`, `PendingPartialBytes`, `DrainedRecords`, `More`를 담습니다. `Poll`은 오류를 반환하지 않고 panic도 `recover`로 `error` 상태로 바꿉니다(fail-open).

## 체크포인트

```json
{"version":1,"file":{"device":0,"inode":0},"offset":0,"prefix_digest":"<sha256 hex>","skipping_oversized_line":false}
```

- `file`은 POSIX `(st_dev, st_ino)`, Windows `(볼륨 일련번호, 64비트 파일 인덱스)`입니다. 처음 읽기 전에는 없습니다.
- `prefix_digest`는 앞부분 `min(offset, 256)` 바이트의 SHA-256입니다. `offset`이 0이면 생략합니다.
- 버전이 다르면 `ErrCheckpointVersion`, 읽을 수 없으면 `ErrCheckpointMalformed`입니다. 호출자는 처음부터 다시 읽고, 중복은 `event_id` 멱등성(#29, #33)이 막습니다.
- 체크포인트 영속화 위치는 #33 SQLite 큐입니다. 이 패키지는 직렬화만 제공합니다.

## tail 규칙(참조 구현과 동일)

[#20 규칙](../poc/collector-dedup-tail.md#tail-체크포인트-규칙)과 [`tail_reference.py`](../../tests/poc/collector/tail_reference.py)를 그대로 옮겼습니다.

| 상황 | 처리 |
|---|---|
| 개행 없는 마지막 조각 | 소비하지 않음(`PendingPartialBytes`) |
| 파일 시작 BOM, CRLF, 빈 줄 | 제거·무시 |
| 깨진 JSON, 객체가 아닌 JSON, 잘못된 UTF-8 | `InvalidLines`만 세고 진행 |
| 1MiB 초과 줄 | `OversizedLines` 1회, 다음 개행까지 버림(개행 없으면 `skipping_oversized_line` 저장) |
| 크기 < offset | `truncated`, 처음부터 |
| 앞부분 지문 불일치 | `rewritten`, 처음부터 |
| 파일 식별자 변경 | `rotated`, 새 파일을 처음부터 |
| 파일 없음·권한 거부 | 상태만 알리고 체크포인트 유지 |

## 참조 구현과 다른 점

| 항목 | 참조 구현 | Go |
|---|---|---|
| 로테이션 | 이전 파일의 읽지 않은 꼬리를 버림 | `Tailer`가 열린 핸들로 이전 파일의 남은 완결 줄을 먼저 읽고 전환(`DrainedRecords`). `PollOnce`나 재시작 직후처럼 핸들이 없으면 참조 구현과 같이 버림 |
| 경로가 사라짐(이름 변경 후 재생성 전) | `missing`만 | `missing`이면서 열린 핸들의 남은 완결 줄은 읽음 |
| 읽기 방식 | 파일 끝까지 한 번에 메모리로 | 64KiB 단위 읽기, Poll당 상한 16MiB(`More=true`면 다시 Poll), 줄 버퍼는 상한+1까지만 |
| 줄 결과 | 파싱한 dict | 검증한 원문 바이트(어댑터의 정확한 키 일치 파서가 다시 읽음) |
| 개행 없이 남은 조각의 상한 비교 | BOM 포함 길이 | BOM 제외 길이(완결 줄과 같은 기준) |
| 일반 파일이 아님, 식별자 불가 플랫폼 | 해당 없음 | `unsupported` |

## 탐색

- `root`(예: Claude `projects` 디렉터리) 아래 `*.jsonl` 일반 파일을 깊이 4(`projects/<p>/<s>/subagents/<a>.jsonl`)까지 찾습니다. 깊이 2는 `main`, 부모 디렉터리가 `subagents`이면 `subagent`, 그 밖은 `other`입니다.
- 심볼릭 링크는 파일·디렉터리 모두 따라가지 않고 개수만 셉니다. 루트 밖으로 나가지 않습니다.
- 읽을 수 없는 하위 디렉터리는 건너뛰고 셉니다. 기본 상한 10,000개를 넘으면 `Truncated`입니다.
- `Rel`은 루트 기준 슬래시 경로라 정렬·재탐색에 안정적입니다. Claude 프로젝트 디렉터리 이름에는 로컬 작업 경로가 인코딩되어 있으므로 `Rel`은 파일을 열 때만 쓰고 로그·서버·오류에 넣지 않습니다. 진단과 체크포인트 키는 `ID`(`Rel`의 SHA-256 앞 128비트 hex)를 씁니다. `ID`는 추측 가능한 경로에 대한 익명화를 보장하지 않습니다.
- fsnotify 없이 호출자가 주기적으로 `Discover`·`Poll`합니다. 주기와 유휴 Tailer 닫기는 Collector 실행 루프(#32 이후)가 정합니다.

## 읽기 전용·프라이버시

- POSIX는 `os.Open`(O_RDONLY), Windows는 `CreateFile(GENERIC_READ, FILE_SHARE_READ|WRITE|DELETE)`만 씁니다. 쓰기·권한 변경·삭제 API를 호출하지 않습니다. `readonly_test.go`가 감시 루트의 항목 수·내용·mode·mtime이 탐색·tail(로테이션·truncate 경로 포함) 전후로 같은지 확인합니다.
- 오류는 고정 sentinel, 상태는 enum이며 경로·줄 내용을 담지 않습니다. 패키지는 로그를 쓰지 않습니다.
- 테스트 데이터는 합성 값과 #20 합성 fixture뿐입니다.

## 플랫폼

| OS | 상태 |
|---|---|
| Linux arm64(Docker `golang:1.24-bookworm`, tmpfs) | 실행 검증(`go test`, `-race`, 퍼즈 30초) |
| Windows | **미검증.** `GOOS=windows go vet`만 통과. 파일 ID는 `GetFileInformationByHandle`(os.SameFile과 같은 기준). ReFS 128비트 ID는 하위 64비트만 비교. 긴 경로(`\\?\`) 미처리. 열린 핸들이 있는 동안 삭제된 파일은 delete-pending이 되어 같은 이름 재생성이 지연될 수 있음. 권한 거부·삭제 후 재생성 테스트는 Windows에서 건너뛰고, 심볼릭 링크 테스트는 생성 권한이 없으면 건너뜀 |
| macOS | **미검증.** `GOOS=darwin go vet`만 통과 |
| 그 밖(`!unix && !windows`) | 식별자를 얻을 수 없어 `unsupported` |

## 후속 범위

- #32: 탐색·tail 실행 루프(주기, 유휴 Tailer 닫기, `Records` → `ParseTranscriptLine` → 정규화)
- #33: 파일별 체크포인트(`ID` 키)와 중복 제거 키 영속화
- #34: Windows·macOS 실장비에서 파일 ID·로테이션·권한 거부·핸들 공유 모드 검증
- transcript 보존 기간 경과 삭제, compact 후 재작성 여부는 #20 미실측 항목으로 남아 있음
