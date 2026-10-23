# 설치·최초 관리자 API v1

관련 이슈: #66 (후속 #67 설치 마법사 화면·설정 단계)

인터넷에 먼저 접속한 사람이 관리자를 차지하지 못하도록, 서버 콘솔에 접근할 수 있는 설치자만 받을 수 있는 일회용 토큰으로 최초 `SUPER_ADMIN`을 만듭니다.

## 토큰 발급(서버 콘솔)

```sh
docker compose -f deploy/compose.production.yaml exec server agent-office-bootstrap
```

- 토큰(`aob_…`)은 이 명령의 출력으로만 나옵니다. 서버 시작 로그·컨테이너 로그에는 남지 않습니다.
- 1시간 유효, 한 번만 사용합니다. 다시 실행하면 새 토큰이 나오고 이전 토큰은 무효가 됩니다.
- DB(`instance_settings`의 `setup.bootstrap`)에는 SHA-256 해시와 만료 시각만 저장합니다.
- `SUPER_ADMIN`이 이미 있거나 설치가 완료되었으면 발급을 거부합니다(종료 코드 1).

## 엔드포인트(`/api/v1/setup`)

| 요청 | 인증 | 성공 | 실패 |
|---|---|---|---|
| `GET /status` | 없음 | `{setup_completed, admin_exists, bootstrap_pending}` | — |
| `POST /admin {bootstrap_token, email, password, display_name}` | 토큰 | `201`, `GET /auth/session`과 같은 본문 + 세션 쿠키 | `403 bootstrap_rejected`, `400 invalid_email`·비밀번호 정책, `409 email_already_registered`, `429` |
| `POST /complete` | `SUPER_ADMIN` 세션 + CSRF | `204`, 설치 완료 잠금 | `403 forbidden` |

- `POST /admin`은 로그인 전 요청이라 #23과 같이 `Content-Type: application/json`과 같은 출처 `Origin`을 요구합니다. IP당 10분 10회로 제한합니다.
- 토큰 오류·만료·재사용·관리자 존재·설치 완료는 모두 같은 `403 bootstrap_rejected`입니다.
- 토큰 검증은 설정 행을 `SELECT … FOR UPDATE`로 잠근 뒤 하므로, 같은 토큰으로 동시에 요청해도 하나만 성공합니다.
- 입력 오류(이메일·비밀번호)는 토큰을 소비하지 않습니다. 같은 이메일의 계정이 이미 있으면(`409`) 트랜잭션 전체를 되돌려 토큰을 다시 쓸 수 있습니다.
- 최초 관리자는 바로 `active`이고 이메일 확인된 상태입니다. 콘솔 접근 권한이 있는 설치자가 직접 입력한 주소이고, 이 시점에는 메일 설정이 없어 확인 메일을 보낼 수 없기 때문입니다.
- 성공·완료는 감사 로그(`setup.bootstrap`, `setup.completed`)에 사용자 ID만 남깁니다.

## 알려진 제한

- 설치 마법사 화면과 서비스명·URL·가입 정책·SMTP 단계는 #67입니다. 그 전까지 가입 정책 기본값은 `CLOSED`입니다(#23).
- 관리자를 모두 잃었을 때의 복구 절차(콘솔에서 역할 부여)는 아직 없습니다.
