# 장치 등록·인증 API v1

관련 이슈: #26

Collector는 계정 비밀번호를 받지 않습니다. 로그인한 사용자가 웹에서 단기 일회용 등록 코드를 받고, Collector가 그 코드로 장치를 등록해 장치 토큰을 받습니다. 사용자와 장치는 항상 서버가 코드·토큰으로 판정하며, 요청 본문에 사용자·장치 식별자를 넣으면 `422`로 거부합니다.

## 인증 방식

| 대상 | 방식 | 실패 |
|---|---|---|
| 웹 사용자 | `ao_session` 쿠키(로그인 세션 토큰. 서버는 SHA-256 해시만 저장). 세션 발급·로그아웃은 #25 | `401 unauthorized` |
| 웹 사용자의 쓰기 요청 | 위 쿠키 + `X-CSRF-Token` 헤더([세션 API](auth-sessions-v1.md)의 CSRF 토큰) | `403 csrf_required` |
| Collector | `Authorization: Bearer aod_...` 장치 토큰(서버는 SHA-256 해시만 저장) | `401 unauthorized` |

CSRF 토큰은 #25에서 도입했습니다. #26 초기의 임시 `X-Agent-Office-Request` 헤더는 더 이상 받지 않습니다.

장치 토큰은 다음 조건을 모두 만족할 때만 유효합니다: 자격증명이 폐기되지 않음, 만료 시각이 없거나 미래, 장치가 폐기되지 않음, 소유 사용자가 `active`.

## 엔드포인트

| 메서드·경로 | 인증 | 요청 | 응답 |
|---|---|---|---|
| `POST /api/v1/devices/registration-codes` | 세션 + CSRF | 없음 | `201 {code, expires_at}`. 코드는 `XXXX-XXXX`, 10분, 일회용. 열린 코드 5개 초과 시 `429 too_many_open_codes` |
| `POST /api/v1/devices/register` | 없음(등록 코드) | `{registration_code, name, platform?, collector_version?}` | `201 {device_id, device_token}`. 잘못·만료·사용된 코드는 구분 없이 `400 invalid_registration_code`. 같은 사용자의 활성 장치 이름 중복은 `409 device_name_conflict`(코드는 소비되지 않음) |
| `POST /api/v1/devices/heartbeat` | 장치 | `{collector_version?, queue_depth?}` | `204`. `device_heartbeats` 기록, `last_seen_at`·버전 갱신 |
| `POST /api/v1/devices/token/rotate` | 장치 | 없음 | `200 {device_token, previous_token_expires_at}`. 이전 토큰은 5분 유예 후 만료. 유예 중인 토큰으로 다시 회전하면 `409 rotation_in_progress` |
| `POST /api/v1/devices/revoke` | 장치 | 없음 | `204`. Collector 로그아웃·제거 시 자기 장치와 모든 토큰 폐기 |
| `GET /api/v1/devices` | 세션 | 없음 | `200 [{id, name, platform, collector_version, created_at, last_seen_at, revoked_at}]`. 자기 장치만 |
| `POST /api/v1/devices/{device_id}/revoke` | 세션 + CSRF | 없음 | `204`. 다른 사용자의 장치는 존재 여부와 무관하게 `404 not_found` |

입력 제약: `name` 1~80자(앞뒤 공백 제거), `platform`은 `windows`/`macos`/`linux`/`unknown`, `collector_version`은 `^[0-9A-Za-z.+-]{1,64}$`, `queue_depth` 0~10,000,000. 알 수 없는 필드는 `422`.

등록 코드 입력은 대소문자·공백·하이픈을 정리해 비교합니다(`abcd efgh` = `ABCD-EFGH`). 알파벳은 혼동 문자(0/O, 1/I/L, U)를 뺀 30자입니다.

## 단일 PC 사용자 정책

초기 버전은 PC당 하나의 Agent Office 사용자만 지원합니다(마스터 사양 §1).

- Collector는 장치 토큰을 하나만 OS 보안 저장소에 보관합니다(#41). 다른 사용자로 바꾸려면 `POST /api/v1/devices/revoke`로 로그아웃한 뒤 새 코드로 다시 등록합니다.
- 서버는 같은 PC인지 판정하지 않습니다. 등록할 때마다 새 장치가 생기고, 같은 사용자의 활성 장치 이름만 중복을 막습니다.

## 감사 로그

`device.registration_code.created`, `device.registered`, `device.token.rotated`, `device.revoked`를 기록합니다. 상세에는 `platform`·`reason` 같은 허용 키만 남기고, 토큰·코드 원문은 넣지 않습니다(테스트로 확인).

## 알려진 제한

- 등록 코드는 약 39비트입니다. 10분 만료·일회용·사용자당 열린 코드 5개 제한이 있지만, IP 단위 속도 제한은 #28에서 추가합니다.
- 코드와 토큰은 서버 비밀 없이 SHA-256으로 해시합니다. DB가 유출되면 만료 전 등록 코드는 오프라인 대입이 가능하므로, 유출 대응 절차(#58·#61)에서 미사용 코드를 즉시 무효화합니다.
- 로그인 엔드포인트(#23·#24)가 없어 테스트는 세션 발급 함수(`issue_session`)를 직접 호출합니다.
- 이벤트 수집 API(`POST /api/v1/events/batch`)는 #35에서 이 장치 인증을 사용합니다.
