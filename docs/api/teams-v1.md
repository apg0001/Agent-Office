# 팀·초대·프로젝트 공유 API v1

관련 이슈: #27 (후속 #54·#55 팀 사무실 화면·실시간 범위)

모든 경로는 `/api/v1` 아래이고 세션 쿠키가 필요하며, 쓰기 요청은 CSRF 토큰이 필요합니다.

## 권한 원칙

- 팀 권한은 팀 역할(`OWNER`·`MANAGER`·`MEMBER`)로만 정합니다. 인스턴스 역할(`SUPER_ADMIN`·`ADMIN`)은 팀 권한을 주지 않습니다.
- 내가 속하지 않은 팀은 없는 팀과 같은 `404 not_found`입니다. 속해 있지만 권한이 모자라면 `403 forbidden`입니다.
- 팀마다 `OWNER`는 정확히 한 명입니다(DB 부분 고유 인덱스 + 커밋 시점 트리거).
- 구성원 목록에는 표시 이름·역할·합류 시각만 나오고 이메일은 나오지 않습니다.

| 동작 | OWNER | MANAGER | MEMBER |
|---|---|---|---|
| 팀 보기·공유 프로젝트 보기 | ✓ | ✓ | ✓ |
| 팀 이름·설명 수정 | ✓ | ✓ | |
| 초대 | MANAGER·MEMBER | MEMBER만 | |
| 초대 목록·취소 | ✓ | ✓ | |
| 역할 변경(MANAGER↔MEMBER) | ✓ | | |
| 내보내기 | 누구나(본인 제외) | MEMBER만 | |
| 탈퇴 | 이전 후에만 | ✓ | ✓ |
| 소유권 이전 | ✓ | | |
| 팀 삭제 | ✓ | | |

## 엔드포인트

| 요청 | 설명 |
|---|---|
| `POST /teams {name, description}` | 만든 사람이 OWNER. 사용자당 소속 팀 50개까지 |
| `GET /teams` | 내 팀과 내 역할 |
| `GET /teams/{id}` / `PATCH` / `DELETE` | 상세(구성원 포함)·수정·삭제 |
| `PATCH /teams/{id}/members/{user_id} {role}` | MANAGER·MEMBER로만. OWNER 지정은 `409 use_transfer` |
| `DELETE /teams/{id}/members/{user_id}` | 내보내기 또는 본인 탈퇴. OWNER 탈퇴는 `409 owner_must_transfer` |
| `POST /teams/{id}/transfer {user_id}` | 대상 구성원이 OWNER, 이전 OWNER는 MANAGER |
| `POST /teams/{id}/invitations {email, role}` | 7일 유효 초대. `{id, email, role, expires_at, invite_url, mailed}` |
| `GET /teams/{id}/invitations` / `DELETE …/{invitation_id}` | 열린 초대 목록·취소 |
| `POST /invitations/accept {token}` | 초대받은 이메일로 로그인한 사용자만 수락. 실패는 모두 `400 invalid_token` |
| `POST /projects {display_name}` / `GET /projects` | 내 프로젝트와 공유한 팀 |
| `PUT` / `DELETE /projects/{id}/shares/{team_id}` | 프로젝트 소유자가 자신이 속한 팀에만 공유·공유 해제 |
| `GET /teams/{id}/projects` | 팀에 공유된 프로젝트(이름·소유자 표시 이름) |

## 초대

- 토큰은 SHA-256 해시만 저장하고 한 번만 쓸 수 있습니다. 같은 주소로 다시 초대하면 이전 초대는 취소됩니다. 이미 구성원이면 `409 already_member`입니다.
- 메일 설정이 있으면 초대 메일을 보내고(`mailed: true`), 없어도 응답의 `invite_url`을 초대한 사람에게 한 번 돌려주어 직접 전달할 수 있습니다.
- 링크는 `{기준 주소}/invite#token=…`입니다. 웹 `/invite` 화면은 토큰을 주소창에서 지우고 탭 저장소(sessionStorage)에만 잠시 둡니다.
- 가입 정책이 `INVITE_ONLY`이면 `POST /auth/signup`에 `invite_token`을 함께 보내고, 초대받은 이메일과 같아야 가입됩니다. 가입 후에도 이메일 확인과 로그인 후 수락이 필요합니다.

## 프로젝트 공유

- 공유는 프로젝트 소유자의 명시적 요청(동의)으로만 생깁니다. 다른 구성원은 남의 프로젝트를 공유할 수 없습니다.
- 공유를 해제하면 팀 목록과 팀 사무실에서 바로 사라집니다. 팀 사무실 표시 규칙은 `docs/api/office-live-v1.md`의 팀 사무실 절(#54·#55)입니다.
- 구성원이 팀을 떠나거나 내보내지면 그 사람이 이 팀에 공유한 프로젝트 공유도 함께 지워집니다.
- 프로젝트와 Collector 저장소의 연결(`project_sources` 지문)은 아직 수집 경로가 없습니다.

## 웹 화면(#52)

`/teams`(사무실 상단 "팀" 링크):

- 팀 만들기, 내 역할 표시, "팀 사무실 보기"(`/?team=<id>`)
- 구성원 표: OWNER는 역할 변경(관리자↔구성원)·내보내기·소유권 넘기기, MANAGER는 구성원만 내보내기. 할 수 없는 동작은 숨기고 서버가 최종 판단합니다.
- 초대: 이메일·역할(OWNER는 관리자도 가능). 메일 발송 여부와 함께 초대 링크를 보여 주고 복사할 수 있습니다. 열린 초대 목록과 취소.
- 팀 나가기(소유자는 숨기고 "소유권을 넘긴 뒤에 나갈 수 있습니다" 안내), 팀 삭제(소유자). 내보내기·나가기·이전·삭제는 확인 창을 거칩니다.
- 내 프로젝트와 공유: 프로젝트 만들기, 팀별 공유 체크, 열린 세션을 프로젝트에 넣기(비공개가 기본).

## 알려진 제한

- 실제 브라우저에서 화면 조작은 확인하지 않았습니다(헤드리스 브라우저 없음, #63). 권한 규칙과 경로는 단위 테스트로 확인했습니다.
- 감사 로그에는 사용자·팀 ID와 역할 이름만 남깁니다.
