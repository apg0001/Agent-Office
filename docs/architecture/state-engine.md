# 상태 엔진

관련 이슈: #36

수집된 사실 이벤트(#35)로 세 개의 독립 상태축을 계산합니다. 전이 규칙은 순수 함수(`apps/server/src/agent_office/state/engine.py`)이고, 이벤트를 저장하는 같은 트랜잭션 안에서 `state/store.py`가 반영합니다. 순수 게임 연출은 이 상태에 들어오지 않습니다.

## 1. Collector 연결

| 상태 | 조건 |
|---|---|
| `ONLINE` | 마지막 heartbeat가 120초 이내 |
| `DISCONNECTED` | heartbeat 없음, 120초 초과, 또는 폐기된 장치 |

저장하지 않고 읽을 때 `devices.last_seen_at`(heartbeat·이벤트 수신 시 갱신)으로 계산합니다. 연결이 끊겨도 세션을 종료하지 않습니다(화면에서는 흐림·연결 배지로 표시, #50).

## 2. 세션 생명주기

| 이벤트 | 전이 |
|---|---|
| `session.discovered` | `DISCOVERED`(아직 다른 근거가 없을 때) |
| `session.started` 또는 그 밖의 모든 이벤트 | `OPEN`. 세션에서 무엇이든 관측됐다는 것은 열려 있다는 근거 |
| `session.ended` | `ENDED`, `ended_at` 기록 |
| `ENDED` 뒤의 더 늦은 이벤트 | `OPEN`으로 재개(#20 실측: resume은 같은 세션 ID, 같은 transcript) |
| `ENDED` 시각보다 오래된(늦게 도착한) 이벤트 | 변화 없음 |

`UNKNOWN`은 저장하지 않습니다. 저장 상태가 `OPEN`이고, Collector가 `DISCONNECTED`이고, 마지막 활동이 30분보다 오래됐을 때만 읽을 때 `UNKNOWN`으로 보입니다(`effective_lifecycle`). 로그가 조용하다는 이유만으로 `ENDED`가 되지 않습니다.

## 3. 에이전트 활동

| 이벤트 | `turn_state` / `activity` |
|---|---|
| `session.started`, `turn.completed`, `turn.interrupted`, `session.ended`, `agent.completed` | `IDLE` / `idle` |
| `turn.started` | `THINKING` / `thinking` |
| `tool.started` | `WORKING` / 이벤트의 활동(`reading`, `coding` …) |
| `tool.completed`, `tool.failed` | `THINKING` / `thinking`(턴은 계속) |
| `approval.requested`(관측) | `WAITING_USER` / `waiting_user` |
| `approval.requested`(추정) | 변화 없음. 실제 승인 대기가 관측될 때만 `WAITING_USER` |
| `approval.resolved` | `WORKING` |
| `agent.started` | 해당 하위 에이전트 `WORKING`, `started_at` 기록 |
| `session.discovered`, `plan.updated`, `unknown` | 변화 없음 |

- 하위 에이전트는 자기 행을 가집니다. 대장의 턴이 끝나도(`Stop`) 하위 에이전트는 계속 `WORKING`일 수 있습니다(#20 실측).
- `agent.completed`는 하위 에이전트의 `ended_at`을 기록합니다. 화면의 퇴근 연출(#50)이 이 사실을 씁니다.
- Claude의 `Stop`은 누락될 수 있으므로(#20), 다음 `turn.started`나 `session.ended`가 오면 상태가 바로잡힙니다.

## 순서와 동시성

- 에이전트 상태는 `(occurred_at, sequence)`가 마지막 반영값보다 클 때만 바뀝니다. 오프라인 큐에서 늦게 온 과거 이벤트는 저장만 되고 현재 캐릭터를 과거 활동으로 되돌리지 않습니다.
- 상태가 실제로 바뀔 때만 `state_version`이 올라갑니다. 실시간 전달(#37)은 이 값으로 증분을 판단합니다.
- 세션 행과 에이전트 상태 행은 `SELECT … FOR UPDATE`로 잠가 동시 배치가 서로 덮어쓰지 않게 합니다.

## 아직 범위 밖

- 스냅샷·WebSocket 증분 조회(#37), 계획 단계 진행도(#38)
- 연결 끊김 상태에서 자동으로 세션을 정리하는 정책은 두지 않습니다. 필요하면 별도 Issue와 ADR로 다룹니다.
