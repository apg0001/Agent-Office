# 운영 알림

관련 이슈: #70(공통 관리자), #71 Discord, #72 Telegram, #73 Microsoft Teams Workflows, #74 Email SMTP

## 무엇을 보내나

| 종류 | 심각도 | 언제 |
|---|---|---|
| `backup.failed` / `backup.recovered` | 긴급 / 정보 | 예약 백업 실패, 실패 뒤 다시 성공(관리 화면에서 직접 성공시켜도 닫힘) |
| `restore_test.failed` / `restore_test.recovered` | 높음 / 정보 | 30일 복원 시험 실패와 복구 |
| `retention.failed` | 주의 | 보존 기간 정리 실패 |
| `heartbeat.failed` / `heartbeat.recovered` | 주의 / 정보 | 외부 Heartbeat 전송 실패와 복구 |
| `channel.test` | 정보 | 관리 화면 "시험" |

호스트 전원이 꺼진 것은 서버가 스스로 알릴 수 없으므로 외부 Heartbeat 서비스(`docs/operations/monitoring.md`)가 알립니다.

## 규칙

- 켜진 채널 중 그 심각도를 구독한 채널로만 보냅니다(채널 여러 개 가능, 최대 20개).
- 같은 장애 알림은 1시간 안에 다시 보내지 않습니다. `*.recovered`는 그 장애 알림을 보낸 뒤에만 보내며, 열린 장애 목록(`notifications.open_incidents`)은 재시작에도 유지됩니다.
- 채널마다 시간당 20건. 실패하면 재시도할 수 있는 경우(연결 실패, 429, 5xx) 1초·4초 뒤 최대 3번 보냅니다.
- 결과는 감사 로그 `notification.sent`에 채널 ID와 원인 코드만 남습니다.

## 문구와 비밀

- 문구는 종류별 고정 템플릿(서비스 이름·심각도·요약·원인 코드·조치·시각)만 씁니다. 프롬프트·코드·토큰·경로·사용자 입력은 넣지 않고, 보내기 직전에 URL·사용자 경로·긴 토큰·이메일처럼 보이는 문자열을 한 번 더 지웁니다.
- 서비스 이름은 40자로 자르고 멘션·마크다운 기호를 뺍니다. Discord는 `allowed_mentions: {parse: []}`로 보내 `@everyone`이 동작하지 않습니다.
- Webhook 주소·봇 토큰은 `AGENT_OFFICE_SECRET_KEY`로 AES-256-GCM 암호화해 `notification_channels.secret_ciphertext`에 둡니다. 키가 없으면 비밀 채널을 만들 수 없습니다(`503 secret_key_missing`). 화면·API에는 설정 여부만 나옵니다.

## 공급자

| 공급자 | 비밀 | 설정 | 허용 주소(SSRF 방지) |
|---|---|---|---|
| Discord Webhook | Webhook 주소 | — | `https://discord.com/api/webhooks/…`(`discordapp.com` 포함) |
| Telegram Bot | 봇 토큰(`123456:ABC…`) | `chat_id`(숫자 또는 `@채널`) | 고정 `api.telegram.org` |
| Microsoft Teams Workflows | "Webhook 요청 수신" 트리거 주소 | — | `https://*.logic.azure.com`, `*.api.powerplatform.com` |
| Email SMTP | — | 수신자 1~10명 | 관리 화면 SMTP 설정(#67) |

모든 Webhook은 https만, 자격 증명이 든 주소는 거부합니다. Teams는 Adaptive Card 1.4 텍스트 블록으로 보냅니다.

## API (`/api/v1/admin/notifications`, 관리자 + CSRF)

| 요청 | 설명 |
|---|---|
| `GET` | 채널 목록(`secret_set`만) |
| `POST {provider, display_name, enabled, config, secret, severities}` | 채널 추가. 잘못된 주소 `400 webhook_url_not_allowed` 등 |
| `PATCH /{id}` | 이름·켜기·설정·심각도·비밀(생략하면 유지) |
| `DELETE /{id}` | 삭제 |
| `POST /{id}/test` | 시험 알림. `{ok, error}`(오류는 `http_403`·`connect_failed`·`rate_limited` 같은 코드만) |

관리 화면 `/admin`의 "알림 채널"에서 같은 일을 합니다.

## 검증

- 단위·통합 테스트: 템플릿·지우기, 공급자 주소 검사(http·다른 호스트·유사 도메인·IP·자격 증명 거부), 심각도·켜기 라우팅, 1시간 억제·복구 알림, 재시도·재시도 불가 코드, API 비밀 비노출·암호문 저장.
- **실제 Discord·Telegram·Teams·SMTP로 보내는 것은 검증하지 않았습니다**(외부 계정 필요). 공급자 호출은 가짜로 바꿔 시험했습니다.
