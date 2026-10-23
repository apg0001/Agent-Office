# Hook·JSONL 이중 수집 중복 제거와 tail 규칙

관련 이슈: #20 (선행: #17 Codex CLI, #19 Claude Code)

실측일: 2026-10-23 (KST), Raspberry Pi 5 Linux arm64, Claude Code `2.1.308`, 합성 `claude -p` 7회(basic 3, resume 4), 비용 약 $0.14

## 결론

- Claude Code는 Hook과 transcript JSONL이 같은 식별자를 공유합니다. `tool_use_id`(#19)와 `prompt_id`↔`promptId`(이번 실측)가 두 소스에서 일치하므로 **도구 시작·종료와 턴 시작은 결정적 키로 중복 제거**할 수 있습니다.
- 같은 키는 같은 `event_id`(UUIDv5)가 됩니다. Collector가 재시작으로 메모리 상태를 잃어도 서버의 `event_id` 멱등성이 두 번째 방어선이 됩니다.
- transcript는 관측 범위에서 **append-only**였습니다. resume해도 같은 파일(inode 동일)에 이어 쓰고, 앞부분 지문이 바뀌지 않았으며, 크기는 단조 증가했습니다.
- **`Stop`은 보장되지 않습니다.** resume 4회 중 1회는 resumed 턴의 `Stop`이 오지 않았습니다(`SessionEnd`는 옴). 턴 종료는 `Stop`, 다음 `UserPromptSubmit`, `SessionEnd` 중 먼저 오는 근거로 닫아야 합니다.
- Codex는 #17에서 transcript 내부 형식이 안정 계약이 아니라고 판단했으므로, 이 규칙을 Codex에 적용하지 않습니다. Codex 교차 소스 키는 미검증입니다.

## 실측 결과

| 항목 | 결과(횟수) | 근거 |
|---|---|---|
| resume 시 `session_id` | 7회 모두 같은 실행 안에서 1종, resume 전후 동일(4/4) | Hook·stream-json HMAC |
| resume 시 transcript 파일 | 같은 파일(4/4) | `(st_dev, st_ino)` HMAC 1종 |
| append-only | 7/7 | 256B·1KiB·4KiB 앞부분 지문 불변, 크기 단조 증가, 마지막 바이트 개행 |
| BOM | 0/7 | 첫 3바이트 |
| `SessionStart.source` | 처음 `startup`, resume 후 `resume`(4/4) | |
| `SessionEnd` | 각 실행 종료마다 1회(`reason: other`) | resume 실험은 실행 2번 → 2회 |
| `Stop` | resume된 턴에서 1/4 누락 | 첫 턴은 7/7 관측 |
| `promptId`(transcript) = `prompt_id`(Hook) | 일치(1회 검증, 2턴) | HMAC 집합 동일 |
| `uuid` 고유성 | `uuid`가 있는 줄은 모두 고유 | |
| 같은 `message.id` 반복 줄 | 0 | |
| 같은 `tool_use` 블록 중복 | 매회 2배 | 내부 줄 유형 `api-request-blob`가 이전 메시지를 다시 담음 |
| 도착 순서 | Hook `PreToolUse` → transcript `tool_use` 줄(`PostToolUse` 시점에 존재) → `tool_result` 줄 | 각 Hook 시점의 transcript 통계 |

미등록 transcript 최상위 키 이름은 56개였습니다(값은 보지 않음). 그중 `promptId`, `toolUseID`, `sourceToolAssistantUUID` 등이 상관관계 후보이고, 나머지(`api*`, `total*`, `hook*` 등)는 내부 기록입니다. 이 목록은 버전마다 바뀔 수 있으므로 어댑터는 허용 키만 읽습니다.

## 중복 제거 규칙

키는 `device|provider|session|kind|ref`이고 `event_id = UUIDv5(고정 namespace, key)`입니다. 장치 범위를 넣어 다른 PC의 같은 원본 ID와 충돌하지 않게 합니다.

| kind | Hook | transcript | ref |
|---|---|---|---|
| `turn.started` | `UserPromptSubmit` | `user` 줄 중 `promptId`가 있고, `tool_result`가 아니고, `isMeta`·`agentId`가 없는 줄 | `prompt_id` |
| `tool.started` | `PreToolUse` | `assistant` 줄의 `tool_use` 블록 | `tool_use_id` |
| `tool.finished` | `PostToolUse` → `tool.completed`, `PostToolUseFailure` → `tool.failed` | `user` 줄의 `tool_result`(`is_error`면 `tool.failed`) | `tool_use_id` |
| `agent.started`/`agent.completed` | `SubagentStart`/`SubagentStop` | 사용하지 않음 | `agent_id` |

- 완료와 실패는 같은 호출의 끝이므로 키를 `tool.finished` 하나로 합치고, 먼저 관측된 결과 유형을 유지합니다.
- 키가 없는 Hook 전용 이벤트(`session.started`, `session.ended`, `turn.completed`, `approval.requested`)는 큐에 처음 기록할 때 무작위 UUID를 받습니다. `PermissionRequest`에는 `tool_use_id`가 없어 도구 호출과 결정적으로 묶을 수 없습니다.
- transcript는 `assistant`·`user` 줄만 활동으로 읽습니다. `api-request-blob` 등 내부 유형은 중복 원인이므로 읽지 않습니다.
- 하위 에이전트 transcript의 도구는 `agentId`, Hook의 도구는 `agent_id`로 소유자를 붙입니다.
- 키 집합은 #33 SQLite 큐에 영속화하고 보존 기간을 둡니다. 재시작 후 transcript를 처음부터 다시 읽어도 통과하는 이벤트가 0이어야 합니다.

## tail 체크포인트 규칙

| 상황 | 처리 |
|---|---|
| 개행 없는 마지막 조각 | 소비하지 않고 다음 poll에서 완결 후 읽음 |
| 파일 시작 BOM, CRLF, 빈 줄 | 제거·무시 |
| 깨진 JSON, 객체가 아닌 JSON | 개수만 세고 건너뜀. 같은 줄에서 멈추지 않음 |
| 최대 길이(1MiB) 초과 줄 | 다음 개행까지 버리고 계속 |
| 크기 < 저장 offset | `truncated`: 처음부터 |
| 저장 offset까지의 앞부분 지문 변경 | `rewritten`: 처음부터 |
| `(device, inode)` 변경 | `rotated`: 새 파일을 처음부터 |
| 파일 없음, 권한 거부 | 예외 없이 상태만 알리고 체크포인트 유지(fail-open) |
| 재시작 | 영속화한 `{device, inode, offset, prefix_digest}`부터 재개 |

처음부터 다시 읽어도 중복 제거 키 덕분에 이벤트가 중복으로 나가지 않습니다. 로테이션 시 이전 파일의 읽지 않은 꼬리는 참조 구현에서 버려집니다. Go 구현(#30)은 열린 파일 디스크립터로 이전 파일을 끝까지 읽은 뒤 전환해야 합니다. 관측 범위에서 Claude Code transcript는 로테이션하지 않았습니다(보존 기간 경과 시 삭제는 미실측).

## 참조 구현과 fixture

- [`tests/poc/collector`](../../tests/poc/collector/README.md): `tail_reference.py`, `dedup_reference.py`
- `tests/fixtures/collector/claude/dual-source/`: 합성 Hook·메인·하위 에이전트 transcript와 기대 결과. 값은 예약 형식 UUID와 `<synthetic>` 자리표시자뿐입니다.
- `tests/test_collector_reference.py`: 순서 무관성, 결정적 `event_id`, 재시작 재처리 0건, 내부 줄 무시, 하위 에이전트 귀속, fixture 원문 금지, tail 경계 조건 10종

Go Collector는 같은 fixture와 기대 결과로 동일 동작을 검증합니다.

## 미실행·후속 범위

- Codex Hook·transcript 교차 키(#17의 transcript 내부 형식 불안정, Pi Codex 미실측)
- compact(`PreCompact`/`PostCompact`) 후 transcript 재작성 여부, `/clear`, fork(`--fork-session`)
- 보존 기간 경과로 transcript가 삭제될 때의 동작
- 비정상 종료(SIGKILL) 후 `SessionEnd` 누락과 partial write의 실제 발생
- Windows·macOS 파일 식별자(inode 대체: Windows file ID)
- 대화형 TUI 세션
