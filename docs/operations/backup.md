# 백업·암호화·복원 시험

관련 이슈: #58 (요구사항 OPS-004~008)

## 무엇을 하나

| 단계 | 방법 |
|---|---|
| 일관성 덤프 | `pg_dump -Fc --no-owner --no-privileges`(PostgreSQL 17 클라이언트, 서버와 같은 주 버전). 한 트랜잭션 스냅샷이라 실행 중에도 일관됨 |
| 암호화 | 덤프 출력을 곧바로 AES-256-GCM 1MiB 청크로 암호화. 평문 덤프는 디스크에 남지 않음. 청크 순서·잘림·덧붙임은 복호화에서 실패 |
| 검사 | 저장 직후 복호화 스트림을 `pg_restore --list`로 읽고, 암호문 SHA-256을 `.json` 매니페스트에 기록 |
| 로컬 보존 | 최근 7개(`/backups`) |
| 외부 보존 | 외부 디렉터리에 복사 후 체크섬 확인, 일간 7개 + 그보다 오래된 것 중 ISO 주마다 1개씩 4주 |
| 복원 시험 | 같은 PostgreSQL에 임시 DB(`ao_restore_test_<임의>`)를 만들어 복원, 마이그레이션 버전·표 수·사용자 수 확인 후 삭제. 운영 DB는 건드리지 않음 |
| 일정 | 서버가 기동 10분 뒤부터 매시간 확인해 하루 1회 백업, 30일마다 복원 시험(advisory lock으로 프로세스 하나만) |

결과(시각·성공 여부·크기·오류 코드)는 `instance_settings`의 `backup.status`에 남고 `GET /api/v1/admin/metrics`의 `backup`에 나옵니다. 오류는 코드(`pg_dump_failed`, `dump_unreadable`, `offsite_copy_mismatch`, `restore_test_failed` 등)만 남기며 경로·접속 정보는 남기지 않습니다. 실패 알림은 EP-12 알림(#70)과 연결합니다.

## 설정

```sh
# deploy/.env.production
AGENT_OFFICE_BACKUP_KEY=<32바이트 URL-safe base64>   # 생성: python3 -c "import base64,os;print(base64.urlsafe_b64encode(os.urandom(32)).decode())"
AGENT_OFFICE_BACKUP_PATH=/mnt/ssd/agent-office/backups      # 선택. 비우면 Docker named volume
AGENT_OFFICE_BACKUP_OFFSITE_PATH=/mnt/nas/agent-office      # 다른 디스크·NAS·rclone 마운트
AGENT_OFFICE_BACKUP_OFFSITE_DIR=/backups-offsite            # 위 경로를 쓸 때만
```

- 백업 키는 관리 화면 비밀 키(`AGENT_OFFICE_SECRET_KEY`)와 **따로** 둡니다. 키를 잃으면 백업을 복원할 수 없으므로 서버 밖(비밀번호 관리자 등)에 보관합니다.
- 외부 경로를 지정하지 않으면 백업이 같은 디스크에만 남습니다. 사양상 금지 상태이므로 운영에서는 반드시 지정합니다. 클라우드 저장소는 rclone mount나 별도 rclone/restic 동기화로 이 디렉터리를 올립니다(이 부분은 미검증).
- 서버 컨테이너는 비루트(65532)로 실행되므로 호스트 경로는 그 UID가 쓸 수 있어야 합니다(`sudo chown 65532:65532 <경로>`).

## 명령

```sh
C="docker compose --env-file deploy/.env.production -f deploy/compose.production.yaml exec server"
$C agent-office-backup create         # 지금 백업
$C agent-office-backup list
$C agent-office-backup verify         # 최신 백업 체크섬·목차 확인(파일 이름을 줄 수 있음)
$C agent-office-backup restore-test   # 임시 DB 복원 시험
```

## 수동 복원(장애 복구)

1. 서버를 멈춥니다: `docker compose ... stop server`.
2. 복호화: `docker compose ... run --rm --entrypoint agent-office-backup server decrypt <백업 파일> /backups/restore.dump`
3. 새 DB에 복원하고 이름을 바꾸거나, 기존 DB를 비운 뒤 복원합니다.
   `docker compose ... exec postgres pg_restore --clean --if-exists --no-owner -U agent_office -d agent_office /path/restore.dump`(덤프 파일을 postgres 컨테이너에 전달한 경우)
4. `alembic upgrade head`, 서버 시작, `GET /api/v1/system/ready` 확인.
5. 복원 직후 `agent-office-retention`을 실행해 삭제 기록(`deleted_users`) 기준 재삭제를 적용합니다(#39).
6. 복호화한 `restore.dump`는 지웁니다.

## 검증

- PostgreSQL 17.11(Pi arm64) 통합 테스트: 실제 `pg_dump`→암호화→`verify`→임시 DB 복원 시험, 손상 파일 체크섬 실패, 외부 복사.
- 단위 테스트: 암호화 왕복(빈 파일·청크 경계), 변조·잘림·덧붙임·다른 키 거부, 로컬 7개·외부 일7/주4 보존, 암호가 명령줄에 나오지 않음.
- CI 러너에는 PostgreSQL 17 클라이언트가 없어 실제 덤프 테스트는 건너뜁니다(테스트 이름과 이유가 출력됨).
- 운영 이미지 실측(Pi 5, USB SSD, 2026-10-24): 로컬 빌드 서버 이미지 + 운영 Compose에서 `create`(로컬·외부 복제, 둘 다 0600) → `verify` → `restore-test` 3회 연속 성공(마이그레이션 0006, 표 29개), 서버 예약 실행이 백업·복원 시험을 한 번 하고 바로 다음 실행에서는 건너뜀, 상태가 `backup.status`에 기록됨.
- 실측 중 발견해 고친 것: 외부 복제 마운트 지점이 이미지에 없어 root 소유 볼륨에 쓰지 못함(이미지에 `/backups-offsite` 생성), 외부 복제 실패가 traceback으로 끝남(`offsite_unwritable` 코드로, 로컬 백업은 유지), 외부 복제본 권한 0644(0600으로). 첫 복원 시험 1회가 원인 불명으로 실패했으나 재현되지 않았고, 단계별 코드(`restore_test_create/restore/check/drop_failed`)로 나눠 다음에 원인을 알 수 있게 했습니다.
- 미검증: 클라우드 저장소 동기화, 장기 실행 시 일·월 일정, 대용량 DB 소요 시간.
