# 배포·롤백

관련 이슈: #60 (요구사항: 배포 전 백업, 마이그레이션 사전 점검, health·smoke, 수동 승인, 이미지 롤백, 비역호환 DB 복원 계획)

## 한 번에 실행

```sh
deploy/upgrade.sh --server ghcr.io/apg0001/agent-office-server:<버전>@sha256:<digest> \
                  --web ghcr.io/apg0001/agent-office-web:<버전>@sha256:<digest>
# 기본 env 파일 deploy/.env.production, Compose 프로젝트 agent-office
```

| 단계 | 하는 일 | 실패하면 |
|---|---|---|
| 계획 | 이전·새 이미지, 현재 DB 버전과 두 이미지가 기대하는 마이그레이션 버전 표시 | — |
| 승인 | `deploy`를 직접 입력(수동 승인 게이트). 자동화에서만 `--yes` | 취소, 아무것도 바뀌지 않음 |
| 1. 백업 | 새 이미지의 `agent-office-backup create`(백업 기능 이전 이미지에서 올라올 때도 동작). 백업 볼륨 소유자 정리 | 중단 |
| 2. 사전 실행 | 새 이미지의 `agent-office-backup migrate-check`: 방금 만든 백업을 임시 DB에 복원하고 새 마이그레이션을 적용해 본 뒤 삭제 | 중단(운영 DB 그대로) |
| 3. 교체 | env 파일 사본을 남기고(`.env.production.bak.<시각>`) 이미지 교체, `up --wait` | 롤백 |
| 4. 마이그레이션 | `alembic upgrade head` | 롤백 |
| 5. 점검 | 준비 상태(`/api/v1/system/ready`, 예전 이미지는 `/health`), 웹 `/`, SPA `/setup`을 proxy로 확인 | 롤백 |

결과는 `deploy/.deploy-history`(git 무시)에 한 줄씩 남습니다. 백업 키(`AGENT_OFFICE_BACKUP_KEY`)가 없으면 1단계에서 멈춥니다.

## 롤백

자동 롤백은 다음 순서입니다.

1. DB 버전이 이전 이미지가 기대하는 버전과 다르면, **아직 실행 중인 새 이미지**로 `alembic downgrade <이전 버전>`을 실행합니다(다운그레이드 스크립트는 새 이미지에만 있음).
2. env 파일의 이미지를 이전 값으로 되돌리고 `up --wait`, 점검을 다시 합니다.

### 비역호환 마이그레이션(다운그레이드 실패)

다운그레이드가 실패하면 스크립트는 멈추고 이 절차를 안내합니다.

1. `docker compose ... stop server web`
2. 배포 전 백업(1단계에서 만든 최신 파일)으로 DB를 복원합니다. 절차는 `docs/operations/backup.md`의 "수동 복원"을 따릅니다.
3. env 파일을 `.env.production.bak.<시각>`으로 되돌리고 `docker compose ... up -d --wait`.
4. 복원 후 `agent-office-retention`을 실행해 삭제 기록 기준 재삭제를 적용합니다(#39).

배포 전 백업 이후 들어온 이벤트는 복원하면 사라집니다. Collector는 서버가 ACK하지 않은 이벤트만 다시 보내므로, 이미 ACK된 그 사이 이벤트는 되살아나지 않습니다.

## 승인 게이트와 CI

- 프로덕션 배포는 관리자가 호스트에서 이 스크립트를 실행하고 `deploy`를 입력하는 것으로만 일어납니다. GitHub Actions에서 Pi로 자동 배포하지 않습니다(자체 호스팅 러너·Environment 승인 설정 필요, 미구성).
- 이미지는 `tag@digest`로 지정합니다. GHCR 게시는 관리자 승인 전까지 하지 않습니다(`docs/installation/docker-images.md`).

## 검증(Pi 5, USB SSD, 2026-10-24)

- 업그레이드: 마이그레이션 0005까지만 있는 이전 커밋(`ac09293`)으로 만든 서버 이미지 → 현재 이미지(0006). 백업·사전 실행·교체·마이그레이션·점검까지 24초에 성공.
- 롤백: 마이그레이션 적용 뒤 점검이 실패하도록(SPA 대체 경로 없는 이전 웹 이미지) 배포 → DB 0006→0005 다운그레이드, 이전 이미지 복귀, health 200.
- 승인 거부 시 아무것도 바꾸지 않고 종료, `shellcheck` 통과.
- 미검증: GHCR에서 실제로 받은 이미지, 다운그레이드가 실패하는 비역호환 마이그레이션(현재 마이그레이션은 모두 다운그레이드 가능).
