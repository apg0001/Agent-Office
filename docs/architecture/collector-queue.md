# Collector 로컬 큐·배치 전송(Go)

관련 이슈: #33 (선행: #15 이벤트 계약, #32 정규화, #35 서버 수집 API)

정규화된 이벤트 v1을 로컬 SQLite에 먼저 기록하고, [수집 계약 v1](../api/event-ingestion-v1.md)대로 배치 전송한 뒤 서버의 영속 ACK를 받은 행만 지웁니다. polling 실행 루프·OS 서비스(#34)와 Hook 설치(#31)는 범위가 아닙니다.

## 패키지

| 패키지 | API | 역할 |
|---|---|---|
| `internal/queue` | `Open(path, Options)`, `Close()` | DB 열기·스키마 마이그레이션. 디렉터리 0700, 파일 0600 |
| | `Next(device, session)` | `normalize.SequenceStore` 구현 |
| | `SeenEventIDs(ctx)` | 영속 중복 제거 집합. `normalize.Options.SeenEventIDs`로 넘김 |
| | `Enqueue(ctx, []normalize.Record, *CheckpointUpdate)` | 레코드 + (선택) tail 체크포인트를 한 트랜잭션으로 커밋 |
| | `Peek`, `Ack`, `Reject`, `Depth`, `Stats` | 전송 대기 조회, 영속 ACK 삭제, 거부 반영, 개수 |
| | `Checkpoint(ctx, key)`, `PruneSeen(ctx, before)` | 체크포인트 읽기, 오래된 중복 제거 ID 정리 |
| `internal/transport` | `New(Config, Store)` | 배치 전송기. HTTP 클라이언트·시계·난수 주입 |
| | `SendOnce(ctx)` | 큐 앞부분 한 배치 전송·ACK 반영 |
| | `Run(ctx)` | 큐를 비우는 루프. `ctx.Err()` 또는 `ErrDeviceRevoked`로만 끝남 |
| | `Notify()`, `Heartbeat(ctx)` | 유휴 대기 깨우기, `POST /api/v1/devices/heartbeat`(버전·큐 깊이) |

## SQLite 드라이버 결정

`modernc.org/sqlite v1.46.1`(SQLite C 소스를 Go로 변환한 순수 Go 드라이버)를 정확한 버전으로 고정했습니다.

- cgo가 필요 없어 Linux에서 `GOOS=windows`/`darwin` 교차 빌드가 되고, 설치 PC에 C 런타임·컴파일러가 필요 없습니다. `mattn/go-sqlite3`는 cgo가 필요해 제외했습니다.
- v1.47.0부터 `go 1.25`를 요구하므로 저장소 Go 1.24와 맞는 마지막 줄인 v1.46.x를 골랐습니다(2026-02-18 배포). Go 1.25로 올릴 때 함께 갱신합니다.
- 비용: 바이너리가 수 MB 커지고 cgo 드라이버보다 느리지만, Collector 쓰기량(초당 수 건)에서는 문제가 되지 않습니다.

## 스키마(v1, `PRAGMA user_version = 1`)

| 테이블 | 내용 |
|---|---|
| `events` | `id`(삽입 순서), `event_id` UNIQUE, `payload`(이벤트 v1 JSON), `enqueued_at`, `attempts` |
| `seen_event_ids` | 결정적 `event_id`(중복 제거 키의 UUIDv5)와 기록 시각 |
| `sequences` | `(device_id, session_id)` → 다음 sequence |
| `checkpoints` | 체크포인트 키(파일 경로 등)의 SHA-256 → `tail.Checkpoint` JSON |
| `counters` | `dropped_oldest`, `dead_letter:<코드>` 개수 |

- 연결 설정: `journal_mode=WAL`, `synchronous=FULL`, `busy_timeout=5000ms`, `_txlock=immediate`, 연결 1개(프로세스 안 쓰기 직렬화).
- 마이그레이션은 버전별 트랜잭션으로 올립니다. DB가 빌드보다 새 버전이면 `ErrSchemaTooNew`로 열지 않고 건드리지 않습니다(다운그레이드 보호).

## 원자적 기록 순서

`Enqueue`는 레코드마다 **중복 제거 → sequence → 큐 행** 순서로 같은 트랜잭션에 씁니다.

1. 결정적 ID(`Record.DeterministicID`)면 `seen_event_ids`에 넣습니다. 이미 있으면 그 레코드는 건너뜁니다(`Duplicates`).
2. `sequences.next = MAX(next, sequence+1)`.
3. `events`에 `event_id`와 payload를 넣습니다. 무작위 UUIDv4 `event_id`도 이 시점 값으로 고정되고, 재전송은 저장된 payload 바이트를 그대로 보냅니다.
4. 체크포인트가 있으면 같은 트랜잭션에서 갱신합니다. 레코드 하나라도 형식이 틀리면(`ErrInvalidRecord`) 아무것도 커밋하지 않습니다.

`Next`는 메모리 캐시만 올리고 영속화는 `Enqueue`가 합니다. 그래서 큐에 들어가지 못한 이벤트의 sequence는 재시작 뒤 다시 쓰일 수 있지만, 큐에 들어간 이벤트의 sequence는 재시작 뒤에도 다시 발급되지 않습니다.

### 원본 중복 제거 키 대신 결정적 event_id 저장

`normalize.SeenKeys()`는 원본 세션·도구 호출 ID를 담습니다. 큐에는 그 키의 UUIDv5인 결정적 `event_id`만 저장하고, #33에서 `normalize.Options.SeenEventIDs`를 추가해 재시작한 Normalizer가 이 집합으로 같은 키를 거르게 했습니다. 따라서 DB 파일에 원본 제공자 ID가 남지 않습니다. `SeenKeys` 옵션은 메모리 테스트용으로 남겨 두었습니다.

`seen_event_ids`는 계속 커지므로 #34 루프가 `PruneSeen(now - 35일)`처럼 서버 보존 기간(30일)보다 길게 잡아 정리합니다. 정리된 원본을 다시 읽으면 서버가 `retention_window_exceeded`로 거부하고 dead-letter로 셉니다.

## 큐 상한과 거부

| 상황 | 처리 |
|---|---|
| 행 수가 `MaxEvents`(기본 100,000) 초과 | 같은 트랜잭션에서 가장 오래된 행부터 지우고 `dropped_oldest`를 올림 |
| `accepted_event_ids`·`duplicate_event_ids` | 행 삭제(영속 ACK) |
| `rejected_events`, `retryable: false` | 행 삭제, `dead_letter:<코드>`만 올림. 이벤트 내용은 남기지 않음 |
| `rejected_events`, `retryable: true` | `attempts`+1, `MaxAttempts`(기본 20)에 닿으면 `dead_letter:max_attempts` |
| v1 enum 밖 거부 코드 | `retryable` 값만 따르고 `dead_letter:unknown_code`로 셈(서버 문자열을 저장하지 않음) |
| 1건 배치가 `422`/`413` | `dead_letter:invalid_request` |

## 전송 규칙

| 상황 | 처리 |
|---|---|
| 배치 구성 | 큐 순서대로 최대 500건, 본문 1MiB 이하(설정으로 더 작게). 저장된 payload를 그대로 이어 붙임 |
| `200` | 응답을 엄격히 해석(`schema_version` 1.0, 세 목록 모두 존재, 추가 필드 금지, 보낸 ID만, 한 ID 한 번). 하나라도 어긋나면 `ErrBadResponse`로 아무것도 ACK하지 않음 |
| `401`/`403`, 빈 토큰 | `ErrDeviceRevoked`. `Run`이 반환하고 큐는 그대로(재등록 후 재개). 프로세스는 죽지 않음 |
| `422`/`413`(배치 2건 이상) | 다음 배치 크기를 반으로 줄여 원인 이벤트를 좁힘. 성공하면 두 배씩 회복 |
| `429`, `5xx`, 그 밖의 상태, 연결 끊김, 타임아웃 | 아무것도 ACK하지 않고 백오프 |
| 리다이렉트 | 따라가지 않음(토큰 전달·POST→GET 변환 방지). 상태 코드 오류로 백오프 |

- 백오프: n번째 연속 실패 뒤 `[0, min(BackoffMax, BackoffBase·2^(n-1)))` 균등 난수(full jitter, 기본 1초·5분). `Retry-After`(초 또는 HTTP-date, 최대 1시간)가 있으면 그 값이 하한입니다. 성공하면 실패 횟수를 초기화합니다.
- 요청 타임아웃 기본 30초. 큐가 비면 `IdleInterval`(기본 2초)마다 확인하고 `Notify()`로 즉시 깨울 수 있습니다.
- 오류 값에는 상태 코드와 개수만 있습니다. 요청·응답 본문, 토큰, URL(Go 네트워크 오류 문자열에 포함됨)을 담지 않습니다.

## 프라이버시

- 큐가 저장하는 값: v1 화이트리스트 13개 필드 JSON, UUID, 정수, 거부 코드 enum, 체크포인트 숫자·해시, 체크포인트 키의 SHA-256.
- `TestQueueStoresNoRawContent`: 모든 Hook ID·원문 필드(prompt, tool_input, tool_response, cwd, transcript_path 등)와 체크포인트 경로, 거부 코드에 sentinel을 넣고 DB 본 파일·WAL 바이트에서 sentinel과 `/home/`이 없음을 확인합니다. 경로가 들어간 열기 오류 문자열에서도 경로를 뺍니다.

## 검증

| 테스트 | 내용 |
|---|---|
| `queue_test.go` | 스키마·PRAGMA·파일 권한, 새 스키마 거부, event_id 고정·재시작 후 동일, 재시작 후 sequence 이어짐·중복 제거 유지, 미기록 sequence 비영속, 중복·잘못된 레코드 원자성, drop-oldest 개수, dead-letter 코드별 개수·원문 미저장, 체크포인트 동시 커밋, 원문 미저장, 동시 Enqueue/Ack, `PruneSeen`, 닫힌 큐 |
| `transport_test.go` | `httptest` 서버로 accepted/duplicate ACK, 송신 후 응답 전 연결 끊김 → 재시작 → 같은 본문·ID 재전송·duplicate 삭제, 500/1MiB·설정 본문 한도, 5xx/429 full jitter 백오프·Retry-After, 401/403 정지, 잘못된 응답 7종 ACK 없음, retryable·알 수 없는 코드, 422 이분 탐색, 타임아웃, 리다이렉트 미추종, 계약 fixture(`valid/batch-response.json`, `invalid/batch-response-freeform-message.json`), 오류 문자열 누출, heartbeat, `Notify` |

Linux arm64(Docker `golang:1.24-bookworm`)에서 `gofmt`, `go vet`, `go test -race`를 실행했습니다. Windows·macOS 실행과 실제 서버(#35) 대상 종단 테스트는 하지 않았습니다(#34에서 루프와 함께 확인).

## 후속 범위(#34)

#34에서 아래 항목을 `internal/runner`로 구현했습니다. 실행·진단 방법은 [Collector CLI·백그라운드 실행·진단](../installation/collector-cli.md)을 봅니다. Linux는 XDG 기본값을 따라 `~/.local/state/agent-office/collector`를 씁니다.

- 실행 루프에서 `queue.Open` → `SeenEventIDs` → `normalize.New(Sequences: q, SeenEventIDs: …)` → tail 결과마다 `Enqueue(recs, &CheckpointUpdate{…})` → `Sender.Notify()`; 별도 고루틴에서 `Sender.Run`과 주기적 `Heartbeat`, 하루 1회 `PruneSeen`.
- `ErrDeviceRevoked`를 받으면 전송·heartbeat를 멈추고 사용자에게 재등록을 안내(큐 유지).
- DB 경로는 OS별 사용자 데이터 디렉터리(Windows `%LOCALAPPDATA%`, macOS `~/Library/Application Support`, Linux `$XDG_STATE_HOME`).
- `parent_agent_unknown`(retryable)은 부모 이벤트가 먼저 ACK되면 풀립니다. 큐 순서상 대부분 같은 배치에 있지만, 상한 초과로 부모가 버려지면 `max_attempts`로 끝납니다.
