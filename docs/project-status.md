# 프로젝트 상태와 세션 인수인계

최종 갱신: 2026-10-23 (KST)

기준 문서: Agent Office 마스터 개발 프롬프트 v0.8

저장소: `apg0001/Agent-Office`

이 문서는 세션 한도, 담당자 변경, 도구 장애가 발생해도 다음 에이전트가 작업을 재구성하지 않고 이어가기 위한 단일 인수인계 기준입니다. 작업 시작과 종료 시 반드시 갱신합니다.

## 현재 위치

- 운영 방식(2026-10-23 소유자 지시): 별도 지시 전까지 승인 자동, 병렬 작업 최대 2개, 토큰은 이 프로젝트에 약 70%만 사용
- 활성 작업 A(Pi, 직접): #37 `EP-05/ISS-03 WebSocket·스냅샷·증분 복원` (`feat/#37-live-updates`)
- 활성 작업 B(Pi, 서브에이전트·별도 worktree): #30 `EP-04/ISS-02 파일 감지·tail·오프셋·로테이션·재시작` (`feat/#30-collector-tail`). `docs/project-status.md`는 작업 A가 통합 관리
- 직전 완료(Pi): #29(PR #95, `019faae`, 서브에이전트), #36(PR #96, `01e4aed`). 그 전: #25(PR #94, `21f2d61`), #35(PR #93, `f3feac8`), #26(PR #91, `7e2df6b`), #88 수정 PR #90(`e2deaa7`), #22(PR #89, `9914373`)
- 직전 완료(Windows PC): #88 Windows 실검증 실패 0으로 종료(2026-10-23), 상태 기록 PR #92(`docs/#88-windows-verification`)
- EP-02: 완료. 세부 #17~#21 종료, Epic #2는 소유자 승인으로 종료. 후속 독립 Issue #87(Desktop PC 필요). #88 종료
- 직전 완료(Windows PC, 병렬): #18, PR https://github.com/apg0001/Agent-Office/pull/86, merge commit `16b55d5`. 소유자가 Codex Desktop `26.1007.2314.0` 합성 세션 1회를 직접 조작해 Hook 8종 관측, `SessionStart` 미관측. 상세 [Codex Desktop 실측](poc/codex-desktop.md). 후속 #87(반복 실측), #88(#19·#21 테스트 Windows 비호환)
- 기준 브랜치: `develop` (`019faae`, PR #95 병합 결과)
- 병렬 작업 주의: Pi(Claude Code)와 Windows PC(데스크톱 에이전트)가 동시에 진행한다. `docs/project-status.md`는 PR 직전에 최신 `develop`을 병합하고 충돌을 해결한다
- 배포/릴리스: 수행하지 않음. GHCR 이미지 게시 없음
- 실제 사용자 수집: 수행하지 않음. 합성 Codex CLI 세션(#17)과 합성 Claude Code `-p` 세션(#19, #20)만 실행
- 호스트 전환: 이 Raspberry Pi가 이후 서비스 호스팅 서버가 될 예정(소유자 안내). 아래 환경 기준선 참고

## GitHub 연결과 백로그

- 로컬 GitHub CLI: `apg0001` 인증, 저장소 `ADMIN`, `repo`/`workflow` 범위 확인.
- 원격: `https://github.com/apg0001/Agent-Office.git`.
- 로컬 `gh`로 라벨 생성과 Issue 쓰기 성공.
- 별도 GitHub 앱 커넥터의 이슈 생성은 `403 Resource not accessible by integration`; 이후 쓰기는 검증된 로컬 `gh`만 사용.
- 등록 이슈: 75개 = Epic 12개(#1~#12) + 최초 세부 작업 62개(#13~#74) + 기준선 보존 #80. Epic #1과 #13~#17은 완료·종료됨.
- 각 Epic 본문에 실제 하위 이슈 체크리스트와 의존성을 연결함.

### 실제 번호 매핑

| Epic | 번호 | 세부 이슈 번호 |
|---|---:|---|
| EP-01 저장소·하네스·공통 계약 | #1 | #13~#16 |
| EP-02 수집 가능성 PoC | #2 | #17~#21 |
| EP-03 인증·데이터·권한 | #3 | #22~#28 |
| EP-04 Go Collector 코어 | #4 | #29~#34 |
| EP-05 서버·상태 엔진·실시간 | #5 | #35~#39 |
| EP-06 Collector GUI·설치·업데이트 | #6 | #40~#44 |
| EP-07 아이소메트릭 사무실 | #7 | #45~#51 |
| EP-08 팀 사무실·협업 | #8 | #52~#55 |
| EP-09 공개 배포·운영 | #9 | #56~#61 |
| EP-10 E2E·성능·릴리스 | #10 | #62~#65 |
| EP-11 오픈소스 Setup Wizard | #11 | #66~#69 |
| EP-12 Notification Providers | #12 | #70~#74 |

## 환경 기준선

### 현재 호스트: Raspberry Pi 5 (2026-10-23 확인, 서버 호스팅 예정)

| 도구 | 확인 결과 |
|---|---|
| 하드웨어/OS | Raspberry Pi 5, RAM 7.7GiB, Ubuntu 24.04.5 LTS, kernel 6.8.0-1065-raspi, aarch64 |
| 저장소 | 루트가 SD 카드(`mmcblk0p2`, 29G 중 14G 여유). 사양의 PostgreSQL SSD 권장 미충족, #56·#58·#63에서 결정 필요 |
| Git | 2.43.0 |
| GitHub CLI | 2.103.0, `apg0001` 재인증 완료(scope `repo`·`workflow`·`read:org`·`gist`) |
| Python | 3.12.3 (CI·이전 기준선 3.13과 다름) |
| Node/npm | 호스트 미설치 |
| Go | 호스트 미설치; 이전처럼 공식 Go 컨테이너로 검증 가능 |
| Docker/Compose | 29.9.2 / v5.6.1, 사용자 `docker` 그룹 소속, 실행 중인 컨테이너 없음 |
| Codex CLI | 0.163.0 (Linux arm64, 미실측) |
| Codex Desktop | 미설치 |
| Claude Code | 2.1.308 (Linux arm64, 이벤트 미실측. #19 대상) |

### 이전 호스트: Windows 개발 PC (#13~#17, #80 수행 환경)

| 도구 | 확인 결과 |
|---|---|
| OS | Windows, PowerShell |
| Git | 2.54.0.windows.1 |
| GitHub CLI | 2.98.0, 인증 정상 |
| Python | 3.13.9 |
| Node/npm | 24.19.0 / 11.17.0 |
| Docker/Compose CLI | 29.8.1 / v5.5.1 |
| Docker 엔진 | 29.8.1, Linux x86_64; 2026-10-23 기동·검증 완료 |
| Go | 호스트 미설치; `golang:1.24-bookworm` 컨테이너로 검증 |
| Codex CLI | 0.162.0-alpha.17.2 |
| Claude CLI | 미설치(로컬 `.claude` 상태 디렉터리는 존재) |

## 이번 세션에서 완료한 작업

1. 마스터 프롬프트 전체를 UTF-8로 확인하고 첫 실행 범위를 EP-01/EP-02로 확정.
2. 공식 OpenAI 문서에서 Codex lifecycle Hook과 app-server 계약을 확인. Hook은 신뢰 검토가 필요하며 transcript 형식은 안정 인터페이스가 아니라는 전제로 PoC 설계.
3. 저장소·권한·도구·로컬 상태를 읽기 전용 점검.
4. 추적 라벨 10개와 전체 74개 이슈 생성, Epic 체크리스트 연결 및 개수 검증.
5. `develop` 원격 브랜치와 `feat/#13-monorepo-bootstrap` 작업 브랜치 생성.
6. #13용 Python/Node/Go 최소 진입점, 개발 Compose, 개발·아키텍처·운영 기준 문서 작성.
7. 구현 커밋 `fe8cadd` push 및 `develop` 대상 Draft PR #75 생성.
8. Docker Desktop 기동 후 Go 포맷·vet·race test, Compose image build와 실제 서비스 health 검증.
9. 8000 포트 충돌을 확인해 테스트에만 18000/15173 포트를 사용하고, 종료 후 컨테이너·네트워크·테스트 DB 볼륨 정리.
10. PR #75를 `develop`에 병합하고 #13 종료.
11. #14 브랜치 생성, 구조화 Issue 폼·PR 템플릿·GitHub 운영 문서·메타데이터 테스트 작성.
12. `main`과 `develop`에 관리자 포함 PR·대화 해결 필수, force-push·삭제 금지 보호 규칙을 적용하고 GitHub API로 재검증.
13. #14 변경에 대해 pytest 4개, Ruff, mypy, Node typecheck·Vitest·production build를 실행해 통과 확인.
14. PR #76을 보호된 `develop`에 병합하고 #14 종료.
15. #15 브랜치에서 닫힌 필드 집합의 이벤트 v1, 배치 요청·응답 JSON Schema와 공통 TypeScript 타입 작성.
16. 유효 fixture 4개와 무효 fixture 9개, Draft 2020-12·화이트리스트·타입 동기화 계약 테스트와 API 의미 문서 작성.
17. 첫 검사에서 `date-time` 검사기가 시간대 없는 값을 허용하는 문제를 발견해 `Z` 또는 `±HH:MM` 패턴을 추가하고 재검사 통과.
18. TypeScript 테스트까지 정적 검사 범위에 포함하면서 Node 내장 타입 설정 누락을 발견해 `@types/node`와 전용 build tsconfig를 추가하고 재검사 통과.
19. 상위 Epic #1 체크리스트에서 실제 병합 완료된 #13·#14를 완료 상태로 동기화.
20. PR #77을 보호된 `develop`에 병합하고 #15 종료·Epic 체크리스트 동기화.
21. #16 브랜치에서 최소 권한, 동시 실행 취소, 릴리스 commit SHA 고정, Python·Node·Go·Compose·Secret Scan과 집계 `CI Gate` workflow 작성.
22. FastAPI 생성 OpenAPI 규격 검사와 workflow 구조·액션 SHA 고정 테스트를 추가.
23. 첫 로컬 재현에서 import 정렬, 기존 Python 포맷, production Compose overlay 조합 오류를 발견해 수정. Windows의 Go 컨테이너 인용 문제는 CI 문제가 아니라 로컬 명령 래핑 문제로 확인하고 분리 실행해 통과.
24. Gitleaks 8.30.1로 전체 Git 이력 6개 commit을 스캔해 비밀 0건 확인.
25. actionlint 1.7.12로 workflow 문법·표현식을 검사하고 CI에서는 동일 이미지를 digest로 고정.
26. 병렬 로컬 검사 직후 Vite `dist/index.html` 잠금으로 `EPERM`이 1회 발생했으나 잔류 Vite 프로세스가 없음을 확인하고 단독 build 재실행에 성공. GitHub Linux runner에는 해당 Windows 파일 잠금이 적용되지 않음.
27. PR #78 GitHub Actions run `39887244474`에서 Python·Node·Go·Compose·Secret Scan과 집계 `CI Gate` 모두 통과.
28. `main`·`develop` 보호 규칙에 strict 필수 상태 검사 `CI Gate`를 추가하고 GitHub API로 두 브랜치 설정을 재조회해 확인.
29. PR #78 최신 run `39887399508`도 전체 통과 후 `develop` 병합, #16과 Epic #1 종료.
30. #17 브랜치에서 Hook 입력·transcript·`codex exec --json`을 원문 없이 구조 통계로 축약하는 합성 PoC 하네스 작성.
31. 공식 OpenAI Hooks 문서와 로컬 `codex-cli 0.162.0-alpha.17.2` help·stable Hook feature를 확인.
32. 첫 실행의 Windows CP949/UTF-8 디코딩 실패를 수정하고, 임시 프로젝트 Hook 탐색과 inline config 주입을 분리 검증.
33. inline Hook 합성 실행 4회에서 시작·프롬프트 제출·도구 전·Stop·세션 종료를 매회 1번 관측. `PostToolUse`는 파싱/크기 실패가 아닌 0회로 반복 확인.
34. 합성 세션 transcript가 `sessions/<number>/<number>/<number>/<redacted>.jsonl`에 append되고 종료 시 21개 유효 JSON 줄인 것을 원문 없이 확인.
35. #17 변경 포함 Python 24개, Node, Go, Compose, actionlint 전체 회귀 검사를 로컬에서 통과.
36. #17 작업 트리 약 60.94MB를 Gitleaks 8.30.1로 검사하고 비밀 0건 확인.
37. PR #79 생성 후 GitHub Actions run `39888754773`의 Python·Node·Go·Compose·Secret Scan과 필수 `CI Gate` 모두 통과.
38. PR #79 최신 run `39888846155`도 전체 통과 후 `develop` 병합, #17 종료와 Epic #2 체크리스트 동기화.
39. #18 `feat/#18-codex-desktop-poc` 브랜치를 최신 `develop`에서 생성.
40. `computer-use` 스킬과 안전 지침상 ChatGPT/Codex Desktop UI·확장 자동화가 금지됨을 확인하고 앱 입력 없이 중단, 수동 운영 또는 비-UI 관측 경로 결정 대기 상태를 기록.
41. 저장소만으로 최초 사양을 복원할 수 없는 공백을 확인하고 문서 Issue #80 생성.
42. #18 작업 브랜치를 원격 `3f39495`에 보존한 채 최신 `develop`에서 `docs/#80-master-prompt-baseline` 생성.
43. 외부 마스터 프롬프트에 대표 비밀·개인 절대경로 패턴이 없음을 확인하고 44,535바이트 원문을 변경 없이 `docs/specifications`에 복사.
44. 원본·저장소 사본 SHA-256 `76bf1febe43afa60072e11f232885709e24f4a4fb619067935426adfafd4c17b` 일치, 14개 요구사항 범위와 문서 링크를 검사하는 테스트 3개 추가.
45. PR #81 최신 GitHub Actions run `39890099009`의 Python·Node·Go·Compose·Secret Scan과 필수 `CI Gate` 전체 통과 후 `develop` 병합, #80 종료.
46. (Claude Code, Raspberry Pi 5) 작업 호스트 전환을 확인하고 OS·도구·Docker·GitHub 인증 상태를 읽기 전용으로 점검. `gh` 토큰 무효 확인 후 소유자가 재인증.
47. 소유자 결정에 따라 #18을 보류로 전환. Pi에는 Codex Desktop이 없고 UI 자동화가 금지돼 있으므로 app-server 비-UI 관측은 #21로 분리. 테스트·실측은 수행하지 않음.
48. #18 브랜치 push와 Issue 댓글로 보류 사유·재개 조건 기록. 데스크톱 운영자용 재개 지시문을 소유자에게 전달.
49. #19 `feat/#19-claude-code-hook-poc` 생성, #18의 Pi 기준선 커밋 cherry-pick(`project-status` 충돌은 #18 쪽 내용으로 해결).
50. `tests/poc/claude` 하네스 작성: 임시 작업공간, `--setting-sources project` + 임시 `--settings` Hook, `stream-json` 구조 축약, 부모 세션 `CLAUDE*` 환경 변수 제거, Hook 장애 주입 옵션.
51. 합성 실행 10회(basic 3, 장애 주입 2, 병렬 하위 에이전트 3, 권한 거부 2). 전부 exit 0, 사용자 설정 SHA-256 무변경, 결과 원문 패턴 0건, 총 비용 약 $0.20.
52. 공식 Hooks 문서와 대조해 `prompt_id`·`effort`·`background_tasks`·`duration_ms`가 문서화된 필드임을 확인하고 보고서·프로브 허용 필드 목록을 보정.
53. 최종 프로브로 `basic` 1회 재실행해 같은 결과·미등록 필드 0 확인.
54. 프로브 회귀 테스트 7개 추가, Python 3.13 컨테이너에서 pytest 34개·Ruff·mypy 통과.
55. PR #82 CI 전체 통과 후 소유자 승인으로 `develop` 병합(`3644486`), #19 종료. 병합 권한은 소유자가 로컬 `.claude/settings.local.json`에 `Bash(gh pr merge:*)`로 허용(Git 제외).
56. Epic #1·#2 본문의 한글이 `?`로 손상된 것을 발견(이전 Windows 체크리스트 갱신 시 인코딩 손상 추정). GitHub 편집 이력의 마지막 정상본(2026-10-22)에서 복원하고 실제 완료 상태(#13~#17, #19)를 체크.
57. #56 `feat/#56-multiarch-images` 생성. 기존 Dockerfile로 Pi baseline 콜드 빌드 18분 27초, server 260MB·web 76.2MB 측정.
58. server: wheel multi-stage, bind mount 설치(258MB), `python:3.13.17-slim` digest 고정, 비루트, `HEALTHCHECK`. web: 루트 lockfile `npm ci`, `nginx-unprivileged:1.30.6` 비루트 8080, digest 고정, `HEALTHCHECK`, 전용 dockerignore.
59. 개발 Compose(소스 빌드)와 독립 운영 Compose(`태그@digest`, 루프백 바인딩, read-only, `cap_drop ALL`, 로그 회전, PostgreSQL 포트 비공개·SSD 경로 옵션), `deploy/.env.production.example` 추가.
60. CI `Images (amd64/arm64)` 네이티브 러너 job을 `CI Gate`에 추가, 수동·승인 전용 `publish-images.yml` 작성(미실행), 회귀 테스트 3개 추가.
61. Pi 실측: 콜드 빌드 11분 43초, 첫 기동 5분 59초(PostgreSQL pull 포함)·재기동 20.9초, 유휴 메모리 합계 약 63MiB, 운영 구성 hardening·루프백·자동 재시작(13초) 확인. 테스트 컨테이너·볼륨·baseline 이미지 정리.
62. actionlint, Python 3.13 컨테이너 pytest 37개·Ruff, Compose 2종 config 통과.
63. PR #83 생성, run `39905138626`에서 `Images (amd64/arm64)` 포함 전체 CI와 `CI Gate` 통과.
64. 소유자 승인으로 PR #83 병합(`f6fedff`), #56 종료, Epic #9 체크리스트를 UTF-8 파일로 갱신·재조회 확인.
65. 다음 작업을 #20으로 선택(EP-02 최우선 기술 위험, EP-04 선행). `feat/#20-dedup-tail-poc` 생성.
66. #19 프로브에 파일 동일성·앞부분 지문(256B/1KiB/4KiB)·BOM·개행·`uuid`·`message.id`·`promptId` 지문·`tool_use` 줄 유형·미등록 키 이름 통계와 `resume` 시나리오(`--session-id`→`--resume`) 추가.
67. 합성 실측 7회(basic 3, resume 4, 약 $0.14): resume 시 같은 세션·같은 파일 append-only, `promptId`=`prompt_id`, `tool_use` 중복은 `api-request-blob` 내부 줄 때문, resumed 턴 `Stop` 1/4 누락. 결과 원문 패턴 0건, 사용자 설정 무변경.
68. `tests/poc/collector` tail·중복 제거 참조 구현, 합성 dual-source fixture, 테스트 18개(비root 권한 거부 포함) 추가. 의존성 설치 시간을 줄이려고 로컬 테스트 이미지 `agent-office-test:py313`을 만들어 사용(저장소 미포함).
69. Python 3.13 컨테이너 pytest 56개, Ruff lint·format, server mypy, PoC mypy strict 통과.
70. PR #84 CI 전체 통과 후 병합(`fa8d321`), #20 종료, Epic #2 체크 갱신.
71. #21 `feat/#21-codex-app-server-poc` 생성. Pi Codex CLI 0.163.0이 비로그인임을 확인하고, 모델 호출 없는 범위로 실측을 설계.
72. `generate-json-schema`로 프로토콜 표면 확인(요청 105, 알림 85, 서버 요청 10, 클라이언트 알림 1). unix socket 제어 채널이 WebSocket임을 101 응답으로 확인.
73. `tests/poc/codex/app_server_probe.py`(stdio·unix WebSocket 클라이언트, 원문 없는 요약) 작성. 같은 서버·다른 서버 2클라이언트 실험 4회 재현: 같은 프로세스에서만 목록 노출·첫 턴 후 `thread/resume` 합류·`turn/*`/`item/*` 수신, 다른 프로세스는 부착 불가.
74. 격리 daemon 실험: 로그인 없는 `codex exec`는 인증 실패로 daemon에 thread를 만들지 않아 CLI·Desktop의 daemon 공유는 판정 불가. daemon `stop` 후 남은 `pid-update-loop`을 경로 확인 후 종료. `pkill -f` 패턴이 자기 셸과 일치해 명령이 중단된 사고 1회(이후 `[c]odex` 패턴 사용).
75. ADR-0002(Hook 1차·JSONL 2차·app-server 기본 미사용) 제안, 테스트 4개 추가, pytest 60개·Ruff·mypy strict 통과.
76. #18 재개(Windows PC): Desktop 패키지·번들 CLI 버전과 Hook 기능 플래그를 읽기 전용으로 확인. 사용자 Codex 로그·DB 원문 미열람.
77. #18 `desktop_session.py`(prepare/mark/summarize/cleanup)와 Hook 프로브 하위 에이전트·승인 필드 확장. 소유자가 Desktop UI에서 6단계 합성 시나리오 수행.
78. #18 Hook 미신뢰 상태 0건 → Desktop 설정에서 신뢰 후 21건 기록. 전역 `config.toml`의 소유 섹션 13개(`projects` 1, `hooks.state` 12)만 제거하고 앱 자체 변경 1건은 보존. 읽기 전용 `.git` 삭제 실패를 수정·회귀 테스트화.
79. #18 PR #86 생성, run `39920836824` 전체 통과. 소유자 결정으로 후속 Issue #87(Desktop 반복 실측)·#88(#19·#21 테스트 Windows 비호환) 생성, #21로 대체된 app-server 프로브 stash 삭제.
80. PR #85 CI 통과 후 병합(`8dec412`), #21 종료, Epic #2 체크 갱신(#18만 미완).
81. #22 `feat/#22-postgres-schema` 생성. SQLAlchemy 2.1.6·Alembic 1.20.1·psycopg 3.3.6을 서버 의존성에 추가.
82. `agent_office/db/schema.py`에 마스터 사양 §5 엔티티 25테이블 작성. 이벤트 장치·세션·에이전트 일치를 복합 FK로, 원문 비저장을 컬럼 설계로 보장.
83. 빈 PostgreSQL 17.11에 autogenerate로 `0001_initial_schema` 생성·검토 후 마지막 SUPER_ADMIN 보호(advisory lock)·팀 OWNER 존재(지연 제약) 트리거 추가.
84. `apps/server/tests/test_db_schema.py` 10개: 메타데이터 차이 0, downgrade·재-upgrade, 무결성 규칙 7종, 원문 컬럼 부재·이벤트 v1 컬럼 일치. 로컬 PostgreSQL 컨테이너 `ao-pg-test`(127.0.0.1:55432)로 실행.
85. CI Python job에 PostgreSQL 서비스(trust 인증)와 `AGENT_OFFICE_REQUIRE_DB_TESTS=1`, Images job에 이미지 내 `alembic upgrade head` 추가. Compose에 `AGENT_OFFICE_DATABASE_URL` 조합, 서버 이미지에 `alembic.ini` 포함.
86. Pi E2E 첫 시도에서 SD 카드 initdb가 healthcheck 재시도(약 50초)를 넘겨 PostgreSQL unhealthy로 실패. 두 Compose에 `start_period: 120s`를 추가해 재실행: `up --wait` 143초, upgrade 후 26테이블, downgrade 후 1, 재-upgrade·health 성공.
87. pytest 70개(비root, DB 포함, 건너뜀 0), Ruff, mypy, Compose 2종, actionlint 통과.
88. PR #89 생성 후 Windows PC 쪽 #18 병합(`16b55d5`)으로 `develop`이 앞서 나가 최신 `develop`을 병합. `docs/project-status.md` 충돌 3곳을 양쪽 기록 보존으로 해결하고 전체 테스트 재실행.
89. PR #89 최신 커밋 CI 전체 통과 후 병합(`9914373`), #22 종료, Epic #3 체크 갱신.
90. 소유자 안내 반영: #18(PR #86, `16b55d5`)과 Epic #2 종료 확인. #88 착수: Claude 프로브 테스트가 `HOME` 대신 `CLAUDE_CONFIG_DIR`로 Claude 홈을 지정하도록 수정(HOME 미설정·존재하지 않는 HOME에서 Linux 통과), WebSocket 테스트는 `socket.AF_UNIX`가 없으면 사유와 함께 skip(AF_UNIX 제거 시뮬레이션으로 skip 사유 출력 확인). Windows 실검증은 미실행.
91. #88 Windows 실검증(Windows PC): `develop` `e2deaa7`, `pip install -e "./apps/server[dev]"` 후 `pytest -rs apps/server/tests tests` 65 통과·10 skip·실패 0. Ruff·server mypy 통과. #88에 결과 댓글 후 종료.
92. 소유자 승인으로 PR #90 병합(`e2deaa7`). Windows PC용 #88 검증 지시문을 소유자에게 전달(Issue에는 게시하지 않음).
93. #26 `feat/#26-device-auth` 생성. 마이그레이션 `0002`(`device_registration_codes`), DB 엔진 의존성(`scope="function"`으로 커밋 후 응답), 토큰·코드 해시, 감사 로그, 세션 검증·CSRF 헤더·장치 Bearer 인증 의존성, 장치 API 7개 구현.
94. DB fixture를 `apps/server/tests/conftest.py`로 이동, 장치 API 통합 테스트 10개 추가. pytest 85개(DB 포함, 비root), Ruff, mypy strict 통과.
95. 소유자 승인으로 PR #91 병합(`7e2df6b`), #26 종료. Windows PC 쪽 상태 PR #92를 소유자 위임으로 최신 `develop` 병합·충돌 해결 후 병합.
96. PR #92(Windows #88 기록)를 소유자 위임으로 최신 `develop` 병합·충돌 해결 후 CI 통과 확인하고 병합(`3fd20d3`).
97. #35 `feat/#35-event-ingestion` 생성. `POST /api/v1/events/batch`: 장치 Bearer 인증, 이벤트 v1 엄격 검증(Pydantic), `payload_digest` 멱등·충돌 판정, 장치 범위 세션, 부모 우선 에이전트 생성(배치 내 순서 무관·부모 고정), 이벤트별 savepoint, 보존 창(30일)·미래 1일 제한, 장치별 속도 제한, 커밋 후 응답.
98. 응답 v1 거부 코드 4개(`event_id_conflict`, `parent_agent_unknown`, `parent_agent_conflict`, `occurred_at_out_of_range`)를 JSON Schema·TypeScript에 추가. 소비자 없음.
99. FastAPI가 본문을 읽은 뒤에야 의존성을 실행해 크기 제한이 늦는 문제를 발견, `Content-Length` 기반 ASGI 미들웨어로 본문 읽기 전 413/411 처리.
100. 기본 422 응답이 실패한 입력 값을 되돌려 보내 원문이 노출될 수 있음을 테스트로 발견, 전역 검증 오류 핸들러로 `loc`(정의 필드·인덱스만)·`type`만 반환하도록 수정.
101. SD 카드 Pi에서 테스트당 임시 DB 생성이 I/O 대기로 매우 느려(이벤트 테스트 194초, 부하 평균 7) 로컬 테스트 PostgreSQL을 tmpfs·fsync off로 재생성(22초). 개발 문서에 기록.
102. 이벤트 테스트 26개(계약 11, API 15) 추가, 공통 헬퍼 `tests/helpers.py`. pytest 111개(DB 포함, 비root), Ruff, mypy, TypeScript typecheck·test·build 통과.
103. PR #93 CI 통과 후 소유자 사전 승인대로 병합(`f3feac8`), #35 종료, Epic #5 체크 갱신.
104. #25 `feat/#25-browser-session` 생성. 세션 서비스(`issue_session`: 이전 세션 폐기·활성 20개 상한, 절대 30일·유휴 7일 만료, 5분 간격 사용 시각 갱신), 쿠키 속성(HttpOnly·Secure 기본·SameSite=Lax·Path=/), 세션 유도 CSRF 토큰(`X-CSRF-Token`, 상수 시간 비교)으로 #26 임시 헤더 대체.
105. API: 현재 세션(CSRF 토큰 전달), 로그아웃, 전체 로그아웃(장치 토큰 유지), 세션 목록·개별 폐기. 감사 로그 4종. 테스트 헬퍼가 실제 `issue_session`을 쓰도록 변경.
106. 세션 테스트 11개 추가, 서버 테스트 58개(PostgreSQL), Ruff, mypy strict 통과.
107. 소유자 승인으로 PR #94 병합(`21f2d61`), #25 종료. 진행률: 세부 Issue 62개 중 14개, Epic 12개 중 2개(EP-01·EP-02) 완료.
108. 소유자가 자율 진행(승인 자동, 병렬 최대 2, 토큰 70%)을 지시. #29 Go Collector 어댑터를 서브에이전트(별도 worktree)에 위임하고 #36을 직접 진행.
109. #36 상태 엔진: 순수 전이 규칙(`state/engine.py`)과 트랜잭션 내 반영(`state/store.py`, 행 잠금), 수집 경로 연결, 장치 목록에 `connection` 필드. 테스트 17개 추가.
110. Pi SD 카드 I/O 포화(PSI io 약 90%, 부하 평균 8~11) 발견. 원인은 저장소에 생성된 `node_modules`(197MB)를 테스트 컨테이너가 매번 복사한 것과 Go race 빌드의 디스크 캐시. `node_modules` 삭제, 컨테이너 작업 디렉터리·Go 캐시를 tmpfs로 전환.
111. PR #96 CI 통과 후 병합(`01e4aed`), #36 종료. 서브에이전트의 #29 PR #95(Go 어댑터: Claude·Codex Hook·transcript 후보, 버전 지원 수준 verified/partial/unverified, #20과 동일한 중복 제거 키·UUIDv5 parity, fuzz)를 검토 후 최신 `develop` 병합·추적표 충돌 해결·CI 통과 확인하고 병합(`019faae`), #29 종료. 서브에이전트 사용량 약 15만 토큰.
112. #30 Go tail을 새 서브에이전트에 위임(tmpfs·Go 컨테이너 1개 제한 지침).
113. #37: 마이그레이션 `0003`(전역 `state_change_seq`, 세션·에이전트 상태 `change_seq`, 변경 시각 인덱스), 상태 반영 시 화면에 보이는 변경에만 증가. `GET /api/v1/office` 개인 스냅샷, `WS /api/v1/live`(Origin 검사, 첫 스냅샷, 30초 재조회+연결별 change_seq 중복 제거 증분, 25초 ping, 매 주기 세션 재검증으로 즉시 철회). 테스트 8개.

## 이번 변경 요약(#22)

- [데이터 모델과 마이그레이션](architecture/data-model.md): ERD, 무결성 규칙, 원문 비저장 설계, 보존·삭제 연쇄, 운영 롤백은 백업 복원 원칙.
- 서버는 아직 DB를 읽고 쓰지 않는다. 수집 API(#35)·인증(#23~#26)이 이 스키마를 사용한다.
- 미실행: 대량 데이터 보존 삭제 성능, 파티셔닝, 운영 DB 백업·복원 연동(#58).

## 이전 완료 변경 요약(#18)

- [Codex Desktop Hook 실측](poc/codex-desktop.md)과 [`tests/poc/codex`](../tests/poc/codex/README.md) Desktop 수동 세션 하네스.
- 관측: `UserPromptSubmit`·`PreToolUse`·`PostToolUse`·`PermissionRequest`·`SubagentStart/Stop`·`Stop`·`SessionEnd`. `PostToolUse`는 Desktop에서 관측(CLI `exec`는 미관측), `PermissionRequest`에는 `tool_use_id` 없음.
- 미관측·미실행: `SessionStart`, compact, `Interrupt`, 승인 허용, 반복 실측. Desktop은 1회 부분 관측으로만 표시.

## 이전 완료 변경 요약(#21)

- [Codex app-server 관측 범위와 제한](poc/codex-app-server.md)과 [ADR-0002](decisions/0002-collector-source-priority.md).
- app-server는 같은 프로세스의 thread만 관측 가능하고 제어 소켓이 실행·쓰기 권한 채널이므로 Collector 기본 경로에서 제외.
- 미실행: 로그인한 CLI·Desktop의 daemon 공유, 모델 턴·승인 요청 알림, Windows·macOS 소켓.

## 이전 완료 변경 요약(#20)

- [Hook·JSONL 중복 제거와 tail 규칙](poc/collector-dedup-tail.md): 결정적 키 `device|provider|session|kind|ref`와 UUIDv5 `event_id`, transcript는 `assistant`·`user` 줄만, tail 체크포인트 `{device, inode, offset, prefix_digest}`.
- 언어 중립 fixture `tests/fixtures/collector/claude/dual-source/`를 Go Collector(#30·#32·#33)가 재사용한다.
- 미실행: Codex 교차 키, compact·fork·삭제, SIGKILL, Windows·macOS 파일 ID, 대화형 TUI.

## 이전 완료 변경 요약(#56)

- [Docker 이미지와 Compose](installation/docker-images.md): 이미지 구성, 설치 방식 A/B, 게시 절차, 베이스 갱신, Pi 실측.
- 운영 Compose는 base+overlay가 아니라 독립 파일이다. CI와 문서의 검증 명령도 바뀌었다.
- 미실행: GHCR 게시, Pi 재부팅 자동 기동, QEMU 교차 빌드, SSD 구성.

## 이전 완료 변경 요약(#19)

- [Claude Code Hook·JSONL 실측 보고서](poc/claude-code-hooks.md)와 [`tests/poc/claude`](../tests/poc/claude/README.md) 하네스.
- 관측: 시작·턴·도구 전/후·하위 에이전트 시작/종료·권한 요청·Stop·종료. 미관측: `Notification`, `PostToolUseFailure`, compact, `PermissionDenied`, `StopFailure`.
- 핵심 결정: `tool_use_id`가 Hook·stream·transcript에서 일치. 하위 에이전트는 `agent_id`로만 생성. `Stop`·`Agent` `PostToolUse`는 유휴·하위 종료 근거가 아님. Hook timeout은 짧게 명시하고 exit 2를 쓰지 않음.
- 호환성 범위: Linux arm64, `-p`, 2.1.308 한정. 대화형 TUI·승인 대기·Windows/macOS는 미실측.

## 이전 완료 변경 요약(#17)

- 원문 없이 Hook 입력·Codex JSONL·합성 transcript 구조만 축약하는 PoC 하네스.
- 민감 값·경로 미보존, fail-open, transcript 범위 제한 회귀 테스트 3개.
- Codex CLI inline 합성 실행 4회와 이벤트·중복·종료 의미 실측 보고서.
- `PostToolUse` 미관측을 숨기지 않은 호환성·정규화 제한과 후속 범위.

## 검증 상태

| 검사 | 상태 | 근거 |
|---|---|---|
| Python 설치/pytest/Ruff/mypy | 통과 | #80 포함 pytest 27개 통과(Starlette TestClient deprecation 경고 1개), Ruff lint·format·mypy 오류 0 |
| npm install/typecheck/test/build | 통과 | 이벤트 계약 3개+웹 1개 Vitest 통과, TypeScript 오류 0, 두 workspace build 성공 |
| 이벤트 JSON Schema 계약 | 통과 | Draft 2020-12 스키마 3개, 유효 4개·무효 9개 fixture, 닫힌 허용 필드 집합 검증 |
| FastAPI OpenAPI 계약 | 통과 | `openapi-spec-validator` 0.9.0, health path 포함 문서 검증 |
| Go fmt/vet/race test | 통과 | `golang:1.24-bookworm`; 패키지는 현재 테스트 파일 없음 |
| Compose config | 통과 | 개발 구성과 base+production overlay `config --quiet` 성공 |
| GitHub Actions 문법 | 통과 | actionlint 1.7.12, 오류 0; CI 이미지는 SHA-256 digest 고정 |
| Docker image build | 통과 | FastAPI server와 React/nginx web 이미지 빌드 성공 |
| Compose 실제 기동/health | 통과 | PostgreSQL·server healthy, API health JSON 확인, web HTTP 200·title 확인 |
| 저장소 메타데이터 검사 | 통과 | Issue 폼 4개·보안 문의 링크·PR 필수 섹션을 pytest로 검증 |
| GitHub 브랜치 보호 | 통과 | `main`·`develop`: 관리자·대화 해결·strict `CI Gate` 활성, 승인 0, force-push·삭제 비활성 |
| GitHub Actions 원격 CI | 통과 | PR #81 최신 run `39890099009`: Python·Node·Go·Compose·Secret Scan과 `CI Gate` 모두 성공 |
| Gitleaks 전체 이력 | 통과 | 8.30.1 컨테이너로 6 commit·약 136KB 검사, leak 0 |
| 실제 Codex CLI Hook 이벤트 | 부분 통과 | inline 합성 실행 4회: 5종 관측, 중복 0, `PostToolUse` 미관측; 상세 #17 보고서 |
| 실제 Codex Desktop Hook 이벤트 | 부분 통과 | #18: Windows Desktop 합성 1회, 8종 관측·`SessionStart` 미관측, Pre/Post `tool_use_id` 4/4·Subagent `agent_id` 1/1 일치 |
| Windows 호스트 Python 회귀(#18) | 부분 통과 | `develop` `8dec412` 병합 후 pytest 62 통과·2 실패·1 skip. 실패는 #19 transcript 템플릿·#21 `socket.AF_UNIX`의 Windows 비호환(#18 변경 무관, Linux CI 대상). Ruff·server mypy·Codex 프로브 mypy strict 통과, Gitleaks 작업 트리 61.12MB leak 0. 후속 #88 |
| Windows 호스트 Python 회귀(#88) | 통과 | Windows 11 10.0.26200, Python 3.13.9, `develop` `e2deaa7`: pytest 65 통과·10 skip·실패 0. skip 사유: PostgreSQL URL 미설정 8, `socket.AF_UNIX` 없음 1, POSIX 권한 거부 재현 불가 1. Ruff·server mypy 오류 0. Windows Collector 지원 의미 아님 |
| GitHub Actions 원격 CI(#18) | 통과 | PR #86 run `39920836824`: Python·Node·Go·Compose·Images(amd64·arm64)·Secret Scan과 `CI Gate` 모두 성공 |
| 실제 Claude Code Hook 이벤트 | 부분 통과 | Pi Linux arm64, 2.1.308 `-p` 합성 10회: 9종 관측, 반복 재현, `tool_use_id`·`agent_id` 상관관계 일치; 대화형·승인 대기 미실측. 상세 #19 보고서 |
| Pi 호스트 Python 회귀 | 통과 | `python:3.13-slim`(arm64) 컨테이너: #19 포함 pytest 34개 통과(Starlette deprecation 경고 1개), Ruff lint·format, server mypy, Claude 프로브 mypy strict 통과. 호스트에는 pytest·`python3-venv` 없음 |
| Pi 호스트 Node·Go 회귀 | 미실행 | Node·Go 호스트 미설치. CI 원격 결과로 대체 확인 |
| Pi arm64 이미지·Compose | 통과 | #56: 빌드, 개발/운영 Compose `up --wait`, health·HTTP 200, 비루트·read-only·루프백·자동 재시작 실측 |
| PostgreSQL 스키마·마이그레이션 | 통과 | #22: PostgreSQL 17.11(Pi arm64)에서 DB 테스트 10개, 서버 이미지 내 upgrade/downgrade/재-upgrade E2E |
| Codex app-server 부착 범위 | 부분 통과 | #21: 같은 서버 합류·다른 서버 부착 불가 4/4, daemon 공유는 로그인 필요로 미판정 |
| Hook·JSONL 중복 제거·tail 규칙 | 통과(참조 구현) | #20: 합성 fixture 순서 무관·재시작 재처리 0, tail 경계 10종, Claude 실측 7회 |
| amd64·arm64 이미지 CI | 통과 | PR #83 run `39905138626`: `Images (amd64)` 41초, `Images (arm64)` 39초, 나머지 job과 `CI Gate` 모두 성공 |
| GHCR 게시 | 미실행 | 외부 공개, 관리자 승인과 `image-publish` Environment 설정 필요 |
| npm audit / pip check | 통과 | 취약점 0, 깨진 Python 의존성 0 |
| diff 공백·비밀·개인 경로 검사 | 통과 | `git diff --check`, 대표 비밀/절대경로 패턴 일치 0 |
| 마스터 사양 기준선 | 통과 | 외부 원본과 저장소 사본 44,535바이트·SHA-256 일치, 요구사항 범위·문서 링크 테스트 3개 통과 |

## 다음 에이전트가 바로 수행할 작업

1. 완료: PR #89 CI 통과·병합(`9914373`).
2. 완료: #22 종료, Epic #3 체크리스트(#22) 갱신.
3. 로컬 테스트 DB 컨테이너는 `docker rm -f ao-pg-test`로 정리한다(다음 DB 작업에 다시 쓰려면 남겨도 된다).
4. 진행 순서(자율): #36 → #37 WebSocket·스냅샷 / 병렬 #29 → #30 tail → #32 정규화 → #33 큐·전송. 이후 #45~ 사무실 렌더러, #23 로그인, #27 팀. 각 PR은 CI 통과 후 병합(승인 자동)하고 Issue·Epic 체크리스트를 갱신한다.
5. ADR-0002는 "제안됨" 상태다. 소유자 검토 후 상태를 갱신한다. #18 Desktop 결과(`PostToolUse` 관측)도 ADR 근거에 반영할지 검토한다.
6. GHCR 게시는 소유자가 `image-publish` Environment를 만들고 승인하기 전까지 실행하지 않는다.
7. 완료: PR #86 병합(16b55d5), #18·Epic #2 종료.
8. #87(Windows PC 전용, Pi에서 실행 불가·진행 금지): `python tests/poc/codex/desktop_session.py prepare`부터 소유자 조작으로 반복 실측·`SessionStart`·승인 허용·Interrupt 확인. 로그인한 CLI·Desktop의 app-server daemon 공유 확인도 함께 한다(#21 후속).
9. #88: 완료. PR #90 병합(`e2deaa7`), Windows 실검증 실패 0, Issue 종료. Windows에서 PostgreSQL 연동 테스트는 `AGENT_OFFICE_TEST_DATABASE_URL` 설정 시에만 실행된다.
10. #19·#20 후속 후보: 대화형 TUI의 승인 대기·`Notification`·중단·compact·fork 관측(소유자 조작 필요).

## 알려진 위험과 결정 대기

- 라이선스는 소유자 결정 전까지 미정이며 `LICENSE`를 만들지 않았다.
- 마스터 프롬프트 v0.8 저장소 사본은 불변 기준선이다. 수정이 필요하면 기존 파일을 고치지 않고 Issue·ADR과 새 버전 파일을 추가한다.
- 호스트 Go는 없지만 공식 Go 컨테이너로 현재 모듈 검증을 완료했다.
- 로컬 8000 포트가 이미 사용 중이어서 Compose smoke test는 18000/15173 포트로 수행했다.
- Windows PC에는 Claude CLI가 없었다. #19 결과는 Pi(Linux arm64) `-p` 모드에 한정되며 Windows/macOS·대화형 지원으로 확장하지 않는다.
- #19 실행으로 Claude Code가 관리하는 합성 프로젝트 transcript 디렉터리 11개가 `~/.claude/projects/`에 생성됐다. 원본 도구 기록 삭제 금지 원칙에 따라 그대로 두었으며 실제 사용자 내용은 없다.
- Claude Code Hook 입력의 `background_tasks`·`session_crons`에는 작업 설명·명령·프롬프트 원문이 들어갈 수 있다. #31·#32 어댑터에서 상태·개수 외 값은 폐기해야 한다.
- Claude Hook 기본 타임아웃은 600초이며 동기 Hook 지연이 그대로 세션 지연이 된다. #31 Hook 설치 시 짧은 `timeout` 명시가 필수다.
- Pi 루트 파일시스템이 SD 카드다. 운영 PostgreSQL 데이터와 백업 위치는 SSD 또는 외부 저장소 결정 전까지 확정하지 않는다.
- 기존 사용자 Codex DB/JSONL 원문은 읽지 않았다. #17에서는 Hook이 직접 제공한 합성 세션 transcript만 구조 키·개수로 축약했으며 원문은 저장소에 남기지 않았다.
- #17 실행으로 Codex가 관리하는 합성 세션 transcript 8개가 사용자 Codex 상태에 생성됐다. 원본 도구 기록을 임의 삭제하지 않는 원칙에 따라 그대로 두었으며 실제 사용자 내용은 없다.
- Codex CLI 0.162.0-alpha.17.2에서 inline `PostToolUse`가 네 번 모두 미관측이었다. 공식 문서와 차이가 있으므로 이 버전의 Hook만으로 `tool.completed`를 만들지 않는다.
- 임시 프로젝트 `hooks.json`은 `--ignore-user-config` 비대화형 조합에서 발견되지 않았고 inline `-c` Hook은 동작했다. 대화형 신뢰 UI와 일반 프로젝트 탐색 지원은 아직 주장하지 않는다.
- #18 Desktop 실측은 1회이고 `SessionStart`가 미관측이다. Hook 신뢰 전에 세션이 시작됐을 가능성이 있어 미지원으로 단정하지 않으며, 반복 실측(#87) 전 Desktop 지원을 표시하지 않는다.
- #18 실행으로 Codex가 관리하는 합성 Desktop 세션 transcript가 사용자 Codex 상태에 남았다. 원본 기록 삭제 금지 원칙에 따라 그대로 두었다.
- #19·#21 테스트의 Windows 실패는 #88에서 해소됐다. Windows에서 AF_UNIX 제어 소켓·POSIX 권한 거부·PostgreSQL 연동 테스트는 skip(미실행)이다.
- Computer Use 안전 지침은 ChatGPT/Codex Desktop UI 자동화를 금지한다. #18의 Desktop 상호작용은 사용자/수동 운영자가 수행해야 하며, 자동화 우회나 보안·신뢰 프롬프트 조작을 시도하지 않는다.
- 이벤트 v1은 payload 계약만 확정했으며 수집 엔드포인트·인증·DB 영속화는 아직 없다. 부모 참조 존재·순환, 자격증명과 `device_id` 일치, 같은 `event_id`의 내용 충돌은 서버 구현에서 검증해야 한다.
- 현재 provider/event enum은 마스터 프롬프트 기반 기준선이다. #17~#21 실제 PoC에서 새 값이 필요하면 원문을 통과시키지 말고 `unknown`을 사용한 뒤 버전형 계약 변경으로 심사한다.
- PR #76~#79 본문의 한글이 `?`로 손상돼 있고 GitHub 편집 이력에 원본이 없다. 커밋 메시지·문서가 원본 기록이며, 필요하면 소유자 승인 후 PR 본문을 다시 작성한다. Epic #3~#12와 세부 Issue는 손상 없음을 확인했다.
- 운영 DB에서 `alembic downgrade`(특히 `0001`)는 모든 테이블을 삭제한다. 운영 롤백은 백업 복원(#58)으로만 한다.
- 부모 에이전트 순환은 DB FK로 막을 수 없다. 수집 API(#35)가 부모 존재·불변을 검증해야 한다.
- 이벤트 수집 속도 제한은 서버 프로세스 메모리 기준이다. 다중 인스턴스·재시작 시 초기화되므로 #63에서 재검토한다.
- 쿠키 인증 쓰기 요청은 #25부터 세션 유도 CSRF 토큰(`X-CSRF-Token`)을 요구한다. 로그인 엔드포인트(#23·#24)가 아직 없어 실제 브라우저 로그인 흐름은 미검증이다. 등록 코드·로그인 IP 속도 제한은 #28.
- Pi SD 카드는 I/O가 병목이다. 테스트 컨테이너는 `--tmpfs`로 작업 디렉터리·빌드 캐시를 RAM에 두고, 저장소에 `node_modules`를 남기지 않는다(필요 시 컨테이너 안 tmpfs에서 `npm ci`). Go 컨테이너는 한 번에 하나만 돌린다.
- Pi(SD 카드)에서 PostgreSQL 첫 기동이 2분 이상 걸릴 수 있다. healthcheck `start_period`를 줄이지 않는다.
- Codex app-server 제어 소켓은 `command/exec`·`fs/writeFile`·샌드박스 밖 `thread/shellCommand`를 허용한다. Collector가 연결하면 실행 권한을 갖게 되므로 ADR-0002 개정 없이 연결하지 않는다.
- `/tmp/codex-daemon-<uid>/`에 #21 실험용 해시 항목이 남았을 수 있다. 사용자 실제 daemon 항목과 구분할 수 없어 삭제하지 않았다.
- Claude Code `Stop`은 누락될 수 있다(#20, resume 1/4). 상태 엔진(#36)은 `Stop` 하나에 의존해 IDLE을 만들지 않는다.
- 로컬 테스트 이미지 `agent-office-test:py313`은 Pi에만 있는 캐시다. 의존성이 바뀌면 다시 만든다(`docs/project-status.md` 68번 참고).
- Windows PowerShell에서 `gh`로 한글 본문을 쓸 때 인코딩이 깨질 수 있다. 본문은 UTF-8 파일과 `--body-file`로만 전달하고 쓰기 후 다시 조회한다.
- Pi 콜드 빌드는 10분 이상 걸리고 첫 기동은 이미지 pull로 수 분이 걸린다. 운영 배포는 사전 빌드 이미지(GHCR) pull을 전제로 한다.
- Wails, Pi 실장비, macOS/Linux, 코드 서명·공증, SMTP/OAuth, 외부 Heartbeat는 각각 등록된 후속 이슈에서 결정한다.
