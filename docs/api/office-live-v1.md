# 사무실 스냅샷·실시간 API v1

관련 이슈: #37

브라우저는 첫 접속·새로고침 때 스냅샷을 받고, 이후 WebSocket 증분으로 화면을 갱신합니다. 재접속하면 다시 스냅샷부터 받습니다. 현재 범위는 **개인 사무실**(자기 장치)입니다. 팀 사무실은 #53~#55에서 같은 구조로 공유 범위 재검사를 추가합니다.

## 데이터

담는 값은 서버 ID, enum, 시각, 사용자가 붙인 장치 이름뿐입니다. Collector가 보낸 세션·에이전트 ID, 토큰, 원문은 없습니다.

| 배열 | 필드 |
|---|---|
| `devices` | `id`, `name`, `connection`(`ONLINE`/`DISCONNECTED`) |
| `sessions` | `id`, `device_id`, `provider`, `lifecycle`(읽을 때 투영한 값, `UNKNOWN` 포함), `started_at`, `ended_at`, `last_event_at`, `desk_slot`(개인 사무실 책상 번호, 반납되면 `null`, #48), `change_seq` |
| `agents` | `id`, `session_id`, `parent_agent_id`, `role`(`lead`/`subagent`), `turn_state`, `activity`, `confidence`(현재 활동을 만든 이벤트의 `observed`/`inferred`, #47), `state_version`, `started_at`, `ended_at`, `change_seq` |

- 스냅샷의 세션은 종료되지 않았거나 종료 후 10분이 지나지 않은 것입니다(퇴근 연출용).
- 폐기된 장치와 그 세션은 포함하지 않습니다.
- 상태 의미는 [상태 엔진](../architecture/state-engine.md)을 따릅니다.

## `GET /api/v1/office`

세션 쿠키 인증. `200 {type: "snapshot", server_time, devices, sessions, agents}`.

## `WS /api/v1/live`

| 단계 | 동작 |
|---|---|
| 연결 | 세션 쿠키로 인증. 브라우저가 보낸 `Origin`은 요청 `Host`와 같거나 `AGENT_OFFICE_ALLOWED_ORIGINS`(쉼표 구분)에 있어야 함(교차 사이트 WebSocket 하이재킹 방지) |
| 첫 메시지 | 스냅샷(`type: "snapshot"`) |
| 이후 | 변경이 있을 때 `{type: "delta", devices, sessions, agents}`. 장치는 연결 상태가 바뀔 때만 포함 |
| 유휴 | 25초마다 `{type: "ping"}` |
| 권한 철회 | 매 주기(1초) 세션을 다시 검증. 로그아웃·세션 폐기·사용자 비활성화 시 즉시 종료 |

종료 코드: `4401`(인증 실패·철회), `4403`(허용되지 않은 Origin).

### 증분 규칙

- `change_seq`는 전역 시퀀스(`state_change_seq`)에서 나오며 화면에 보이는 값이 바뀔 때만 증가합니다. 클라이언트는 같은 `id`에 대해 `change_seq`가 더 큰 값만 적용합니다.
- 서버는 매 주기 최근 30초 동안 바뀐 행을 다시 조회하고, 연결별로 이미 보낸 `change_seq`를 걸러 보냅니다. 늦게 커밋된 트랜잭션도 놓치지 않기 위해서입니다.
- 화면 애니메이션은 이 사실 상태와 비동기로 움직입니다. 연출 상태는 서버로 보내지 않습니다.

## 알려진 제한

- 연결마다 1초 주기로 DB를 조회합니다. 30세션·소수 사용자 규모(Pi 목표) 기준이며, 부하 실측은 #63에서 합니다. 필요하면 PostgreSQL `LISTEN/NOTIFY`로 바꿉니다.
- 클라이언트 메시지(구독 범위 변경 등)는 아직 받지 않습니다.
- 계획 진행도·타임라인은 #38입니다.
