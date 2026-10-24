# 프로젝트 상태와 세션 인수인계

최종 갱신: 2026-10-23 (KST)

기준 문서: Agent Office 마스터 개발 프롬프트 v0.8

저장소: `apg0001/Agent-Office`

이 문서는 세션 한도, 담당자 변경, 도구 장애가 발생해도 다음 에이전트가 작업을 재구성하지 않고 이어가기 위한 단일 인수인계 기준입니다. 작업 시작과 종료 시 반드시 갱신합니다.

## 현재 위치

- 운영 방식(2026-10-23 소유자 지시): 별도 지시 전까지 승인 자동, 병렬 작업 최대 2개, 토큰은 이 프로젝트에 약 70%만 사용
- 활성 작업 A(Pi, 직접): #62 Linux 실제 흐름 (`test/#62-linux-claude-flow`)
- 활성 작업 B(Pi, 서브에이전트·별도 worktree): #41 나머지·#43 키체인·트레이 (`feat/#41-keychain-tray`)
- 직전 완료(Pi): #24(PR #132, `2e754ba`, 서브에이전트, Epic #3 종료), #60(PR #131, `221fb8e`, Epic #9 종료), #38(PR #126 서버 `4eeb9a0`, PR #128 Collector `2d14891`, Epic #5 종료), #61(PR #129, `3fb1982`), #70~#74(PR #127, `41446c7`, Epic #12 종료), #28(PR #125, `e7e2ce2`), #68(PR #124, `8153abb`), #58(PR #123, `9af0f4c`), #31(PR #120, `7346425`, 서브에이전트, Epic #4 종료), #59(PR #121, `5881ba5`), #39(PR #119, `53682b7`), #53(PR #118, `ac09293`, Epic #8 종료), #52(PR #117, `9db2315`), #54·#55(PR #116, `8c74cb1`), #34(PR #113, `5b85ade`, 서브에이전트), #57(PR #114, `985c59f`), #27(PR #112, `b111d67`), #67(PR #110 서버 `b101c9a`, PR #111 웹 `217531e`), #66(PR #109, `c7722b1`), #33(PR #108, `113905c`, 서브에이전트), #23(PR #107, `e008e28`), #46(PR #106, `119ae9d`, Epic #7 종료), #51(PR #105, `fe6592d`), #32(PR #103, `4ed866c`, 서브에이전트), #50(PR #104, `bd5b25a`), #49(PR #102, `74d70c3`), #48(PR #101, `8a1d885`), #30(PR #98, `a589e62`, 서브에이전트), #47(PR #100, `09ea7b7`), #45(PR #99, `f1b7d7b`), #37(PR #97, `95bac53`), #29(PR #95, `019faae`, 서브에이전트), #36(PR #96, `01e4aed`). 그 전: #25(PR #94, `21f2d61`), #35(PR #93, `f3feac8`), #26(PR #91, `7e2df6b`), #88 수정 PR #90(`e2deaa7`), #22(PR #89, `9914373`)
- 직전 완료(Windows PC): #88 Windows 실검증 실패 0으로 종료(2026-10-23), 상태 기록 PR #92(`docs/#88-windows-verification`)
- EP-02: 완료. 세부 #17~#21 종료, Epic #2는 소유자 승인으로 종료. 후속 독립 Issue #87(Desktop PC 필요). #88 종료
- 직전 완료(Windows PC, 병렬): #18, PR https://github.com/apg0001/Agent-Office/pull/86, merge commit `16b55d5`. 소유자가 Codex Desktop `26.1007.2314.0` 합성 세션 1회를 직접 조작해 Hook 8종 관측, `SessionStart` 미관측. 상세 [Codex Desktop 실측](poc/codex-desktop.md). 후속 #87(반복 실측), #88(#19·#21 테스트 Windows 비호환)
- 기준 브랜치: `develop` (`5b85ade`, PR #113 병합 결과)
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
114. PR #97 CI 통과 후 병합(`95bac53`), #37 종료. 서버 데이터 흐름(수집→상태→실시간) 완성.
115. #45: `node_modules`를 SD 카드 작업 트리 대신 Docker 볼륨 `ao-node-modules`에 두는 node 실행 방식 도입. `apps/web/src/office`(사실 상태 타입, 논리 그리드·구역·가구·자리, 2:1 투영·깊이, 활동→구역, 결정적 합성 어댑터, Phaser 장면, 지연 로드 React 뷰, 합성 데이터 배지). Vitest 13개·typecheck·build 통과. 실제 브라우저 렌더링은 헤드리스 브라우저가 없어 미검증.
116. PR #99 CI 통과 후 병합(`f1b7d7b`), #45 종료. #30 서브에이전트가 PR #98을 생성(보고 대기).
117. #47: 마이그레이션 `0004`(`agent_current_states.confidence`), 상태 반영·스냅샷·증분에 신뢰도 포함. 웹: 스냅샷·증분 병합(`change_seq`), 폭주 병합(1.5초 체류, 긴급 전환 즉시), 로그인 시 실시간 연결·재접속, 미로그인 시 합성, 추정 상태 반투명·`?`, 승인 대기 `!`, Vite `/api` 프록시. 서버 테스트 84개·웹 테스트 19개 통과.
118. PR #100 병합(`09ea7b7`), #47 종료. #30 서브에이전트 PR #98(Go tail: 체크포인트 v1, 로테이션 시 이전 파일 끝까지 읽기, 심볼릭 링크 미추적 탐색, 읽기 전용 보장, fuzz)을 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`a589e62`), #30 종료. #32 정규화를 새 서브에이전트에 위임.
119. #48: 서버 책상 배정(`state/desks.py`: 개인 레이아웃 행 잠금, 가장 낮은 빈 번호 재사용, 종료 30초 후 반납, 없을 때만 확장, 배정 시 세션 change_seq 증가), 스냅샷·증분 `desk_slot`. 웹: 서버 번호로 자리 결정, 필요할 때만 업무석 줄 증가·바닥만 재그리기. #45 합성 ID가 37자를 36자로 잘라 16개 미만 세션 ID가 모두 같아지던 버그 수정. 서버 89개·웹 21개 테스트 통과.
120. PR #101 병합(`8a1d885`), #48 종료. 진행률: 세부 Issue 62개 중 21개.
121. #49: A* 경로(가구·벽 회피, 막힌 목적지 보정), 이동 중 목적지 변경 시 현재 칸에서 재탐색, 카메라(드래그·방향키·휠·+/−, 수동 조작 시 자동 초점 일시정지, F·버튼으로 재활성화, 활동 캐릭터 중심 추적). 웹 테스트 28개 통과.
122. PR #102 병합(`74d70c3`), #49 종료.
123. #50: 존재 방식 규칙(퇴근은 세션 종료·하위 완료·상태 이탈일 때만, 연결 끊김은 흐림+표시로 유지, UNKNOWN은 옅게+표시, 추정은 반투명), 퇴근 시 출입구까지 경로 이동 후 제거, 유휴 휴게실 생활 연출. 연속 ID에서 해시가 거의 같아 엇갈림이 안 되던 문제를 발견해 FNV-1a+섞기로 교체. 웹 테스트 33개 통과.
124. PR #104 병합(`bd5b25a`), #50 종료. #32 서브에이전트 PR #103(Go 정규화: v1 13필드 고정, 세션·에이전트 UUIDv5 매핑, 어댑터→중복 제거→정규화 파이프라인, 재시작 시 저장된 키로 재방출 없음, 원문 sentinel 누출 검사)을 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`4ed866c`), #32 종료. 서브에이전트 사용량 약 15만 토큰. 미검증: Claude transcript `timestamp` 실제 형식(잘못된 값은 관측 시각으로 대체).
125. #33 큐·전송을 새 서브에이전트에 위임. SQLite는 Windows·macOS 교차 빌드를 위해 cgo 없는 `modernc.org/sqlite`로 지정.
126. #51: `GET /api/v1/office/agents/{id}`(본인 장치만, 남의 것·없는 ID 모두 404, 최근 이벤트 50개 허용 필드만, 계획은 번호·상태만), 웹 상세 패널(클릭 선택·드래그 제외, 상위·하위 이동, Esc, 선택 대상 `change_seq` 변경 시 재조회, 합성 데이터는 타임라인 없음·합성 표시, 추정 표시). 서버 office 테스트 10개·웹 테스트 38개 통과. 계획 단계 수집 경로는 이벤트 스키마에 필드가 없어 #38로 남김.
127. PR #105 병합(`fe6592d`), #51 종료. 진행률: 세부 Issue 62개 중 25개.
128. #46: 외부 에셋 없는 픽셀 캐릭터(문자 격자→텍스처 캐시, 리드 왕관·하위 넥타이로 모양 구분, 장치별 옷 8색, 걷기 2프레임), 리드 라벨은 PC 이름 정리·10자 축약, 하위는 "하위". 클릭 판정을 캐릭터 이미지로 옮김. 웹 테스트 44개 통과.
129. PR #106 병합(`119ae9d`), #46 종료. Epic #7(아이소메트릭 사무실) 세부 7개 모두 완료해 종료.
130. #23: Argon2id(`argon2-cffi` 25.1) 비밀번호, 가입 정책(`auth.signup_policy`, 기본 CLOSED), 가입·이메일 확인·로그인·복구 API, 일회용 토큰 원자적 소비, 계정 존재 여부 숨김(동일 응답·더미 해시), 로그인 전 요청 Origin·JSON 검사, 메모리 속도 제한, SMTP 미설정 시 503·계정 미생성. Origin 검사를 `origin.py`로 분리해 WebSocket과 공유. 서버 테스트 173개 통과. 로컬 테스트 이미지 `agent-office-test:py313`에 argon2-cffi 추가(이미지 재생성 시 `pip install argon2-cffi==25.1.0`).
131. PR #107 병합(`e008e28`), #23 종료. #33 서브에이전트 PR #108(Go SQLite 큐 `modernc.org/sqlite` v1.46.1 — v1.47+는 Go 1.25 필요, WAL·synchronous FULL·스키마 버전, 중복 제거→시퀀스→큐 행→체크포인트 한 트랜잭션, 거부는 코드별 집계만, 넘치면 오래된 것부터 버림; 전송: 500건·1MiB 배치, full jitter 백오프·Retry-After, 401/403 시 큐 보존 후 중단, 422/413 배치 반분, 응답 엄격 파싱. #32 normalize에 `SeenEventIDs` 추가해 디스크에 공급자 ID를 남기지 않음)을 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`113905c`), #33 종료. 서브에이전트 사용량 약 16만 토큰. 미검증: 실제 Windows·macOS, 실제 서버 종단 간, 전원 차단 내구성.
132. #34 실행·진단을 새 서브에이전트에 위임.
133. #66: `agent-office-bootstrap` 콘솔 명령(토큰은 출력으로만, DB에는 해시·만료, 1시간, 재발급 시 이전 무효), `GET /api/v1/setup/status`, `POST /api/v1/setup/admin`(행 잠금으로 동시 요청 중 1건만, 입력 오류·중복 이메일은 토큰 미소비), `POST /api/v1/setup/complete`(SUPER_ADMIN, 설치 완료 잠금). 로그인 세션 발급을 `start_session`으로 공유. 테스트 30개(bootstrap 11개 신규) 통과.
134. PR #109 병합(`c7722b1`), #66 종료. 진행률: 세부 Issue 62개 중 29개.
135. #67 서버: 마이그레이션 `0005`(`instance_secrets`), `secret_store.py`(AES-256-GCM, 키 `AGENT_OFFICE_SECRET_KEY`, 비밀 이름 AAD, 키 없으면 저장 거부), `instance.py` 설정 헬퍼, `/api/v1/admin/settings`(사이트·가입 정책·SMTP·SMTP 시험·OAuth, 관리자+CSRF, 비밀은 설정 여부만 응답), 메일 발송·링크가 관리 화면 설정을 먼저 쓰도록 변경. `cryptography` 50.0.2 추가(로컬 테스트 이미지에도 설치). 서버 테스트 203개 통과.
136. PR #110 병합(`b101c9a`), #67 서버 부분.
137. #67 웹: 경로 기반 화면(`/setup` 마법사: bootstrap→관리자→사이트→가입 정책→SMTP 저장·시험→완료 잠금, `/login`·`/signup`·`/verify-email`·`/reset-password`), 메일 토큰은 URL 조각에서 읽고 즉시 제거, 오류 코드 한국어 문구, 사무실 화면 상단 로그인·로그아웃. 운영 nginx에 SPA 대체 경로(`apps/web/nginx.conf`). 웹 테스트 52개 통과. 실제 브라우저 흐름은 미검증.
138. PR #111 병합(`217531e`), #67 종료. 진행률: 세부 Issue 62개 중 30개.
139. #27: `/api/v1/teams`(생성·조회·수정·삭제, 역할 변경, 내보내기·탈퇴, 소유권 이전), 초대(7일·일회용·재초대 시 이전 무효·초대받은 이메일 계정만 수락·메일 없으면 링크를 초대자에게 반환), 프로젝트 생성·소유자 동의 공유·해제·팀별 공유 목록, `INVITE_ONLY` 가입 시 초대 토큰·이메일 일치 검사. 팀 권한은 팀 역할로만(SUPER_ADMIN도 없음), 남의 팀은 404. 웹 `/invite` 수락 화면. 서버 212개·웹 52개 테스트 통과.
140. PR #112 병합(`b111d67`), #27 종료. 진행률: 세부 Issue 62개 중 31개.
141. #57: 운영 Compose에 Caddy `proxy`(2.10.2, digest 고정)를 유일한 호스트 포트로 추가하고 서버·웹 포트 제거, 서버 `FORWARDED_ALLOW_IPS`로 실제 클라이언트 IP 사용, 보안 헤더, `docs/installation/remote-access.md`(Tailscale 권장·Cloudflare Tunnel·도메인 HTTPS·Nginx·위협 모델). Pi에서 로컬 빌드 이미지로 운영 Compose 실기동 검증. Caddy 실행 파일의 파일 권한 때문에 `NET_BIND_SERVICE`가 필요함을 실측으로 발견.
142. PR #114 병합(`985c59f`), #57 종료. 기존 저장소 메타데이터 테스트가 서버·웹 호스트 포트를 가정해 CI 실패 → proxy 단일 진입점 검사로 갱신.
143. #34 서브에이전트 PR #113(`agent-office-collector` CLI: register(0600 원자적 설정, 토큰 미출력, https 강제·루프백 예외), run(탐색·tail·정규화·큐·전송·60초 heartbeat·일일 정리, 단일 인스턴스, 401/403 시 큐 보존·재등록 안내), hook(항상 종료 코드 0·무출력, 필터된 결과만 로컬 spool), pause/resume, status/diagnose(코드·개수만), service install/uninstall(systemd user·launchd·Windows 로그온 작업, 소유 표시 없는 파일 미변경); 최초 설치 시 기존 transcript는 끝에서 시작)을 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`5b85ade`), #34 종료. 서브에이전트 사용량 약 26만 토큰. 미검증: macOS·Windows·실제 systemd·실제 서버·실제 Hook 호출.
144. 체크포인트: 세부 Issue 62개 중 33개 완료. Epic #7 종료, Epic #4는 #31만 남음.
145. 소유자가 재개 지시. #54·#55: 세션→프로젝트 지정 API, 팀 스냅샷(공유·소유자 구성원·소유자 장치 조건, PC 이름 대신 표시 이름, 팀용 책상 순번), 팀 상세, `WS /api/v1/live?team=`(매 주기 구성원 재검사·팀 스냅샷 재계산, 항목 제거 시 새 스냅샷, 추방 시 4404), 팀 이탈 시 그 사람의 팀 공유 자동 해제, 웹 사무실 범위 선택. 서버 218개·웹 54개 테스트 통과.
146. PR #116 병합(`8c74cb1`), #54·#55 종료. 진행률 35/62. #31 Hook 설치를 서브에이전트에 위임.
147. #52: `/teams` 화면(팀 만들기, 역할별 구성원 관리·초대 링크 복사·초대 취소·나가기·소유권 넘기기·삭제, 확인 창, 내 프로젝트 만들기·팀별 공유·세션 지정). 웹 테스트 57개 통과.
148. PR #117 병합(`9db2315`), #52 종료.
149. #53: 서버 범위 스냅샷 공통화(`_scoped_snapshot`), 통합 사무실(`GET /api/v1/office/combined`, 상세, `WS ?view=combined`, 여러 팀 공유 세션도 `UNION`으로 한 번), 웹 범위 4종(내·통합·나란히·팀, `OfficeScope`), 나란히 보기(개인+팀 최대 3개 작은 화면). 서버 220개·웹 58개 테스트 통과.
150. PR #118 병합(`ac09293`), #53 종료, Epic #8 종료. 진행률 38/62.
151. #39: `retention.py`(이벤트·종료 세션·heartbeat 30일, 토큰·초대·로그인 세션 정리, 감사 로그 365일, advisory lock, 서버 lifespan에서 기동 5분 뒤부터 매시간, `agent-office-retention`), 계정 삭제 API(확인 입력·마지막 관리자·공동 팀 소유자 보호), 마이그레이션 `0006`(`deleted_users`)으로 백업 복원 후 재삭제. 테스트 중 배정된 책상이 있는 세션 삭제가 배정 쌍 제약에 걸리는 문제를 발견해 삭제 전 책상 해제. 서버 테스트 227개 통과. 문서 `docs/operations/data-retention.md`.
152. PR #119 병합(`53682b7`), #39 종료(#28에 계정 삭제 부분 구현 댓글).
153. #59: `GET /api/v1/system/ready`(DB·마이그레이션 head 비교, 503), `GET /api/v1/admin/metrics`(호스트·DB·수집 지표, 식별자 없음), 외부 Heartbeat(`AGENT_OFFICE_HEARTBEAT_URL`, 준비 정상일 때만 push). 문서 `docs/operations/monitoring.md`. 서버 테스트 232개 통과.
154. PR #121 병합(`5881ba5`), #59 종료. #31 서브에이전트 PR #120(`agent-office-collector hooks install|uninstall|status|print`: Claude `settings.json` hooks에 #19 검증 9개 이벤트 등록, 미리보기 후 `--yes`로만 쓰기, 0600 타임스탬프 백업·원자적 쓰기·읽은 뒤 변경 감지, 파싱 오류·읽기 전용·심볼릭 링크 거부, 소유 항목만 제거, Claude 버전 감지로 `claude_version` 기록; Codex는 사용자 설정 위치가 PoC로 확정되지 않아 print 전용)을 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`7346425`), #31 종료, Epic #4 종료. 서브에이전트 사용량 약 18만 토큰. 미검증: 실제 Claude·Codex가 설치된 Hook 호출, macOS·Windows 인용 규칙 실기기.
155. 체크포인트: 세부 Issue 62개 중 41개. 종료 Epic: #2·#4·#7·#8.
156. 소유자 재개 지시. #58: 서버 이미지에 `postgresql-client-17`(Debian 13, 17.11) 추가, `backup.py`(`pg_dump -Fc` 출력을 바로 AES-256-GCM 청크 암호화, `pg_restore --list` 검사, SHA-256 매니페스트, 로컬 7개·외부 일7/주4, 임시 DB 복원 시험, 서버가 하루 1회 백업·30일마다 복원 시험, `agent-office-backup`), 키 `AGENT_OFFICE_BACKUP_KEY`(비밀 키와 별개), 지표에 백업 상태. 운영 이미지 실측에서 외부 복제 마운트 지점이 root 소유라 쓰기 실패하는 문제를 발견해 이미지에 마운트 지점 생성, 오류를 코드로 정리. 로컬 테스트 이미지에 PostgreSQL 17 클라이언트 추가(CI 러너에는 없어 실제 덤프 테스트는 이유를 표시하고 건너뜀). 서버 테스트 242개 통과.
157. Pi를 SD 카드에서 USB SSD(`sda`, PARTUUID `ceae211c-01`/`-02`)로 이전(소유자 요청). SD 원본은 손대지 않고 예비 보관, EEPROM 부팅 순서는 바꾸지 않음(SD를 뽑으면 USB로 부팅). 순차 쓰기 약 208MB/s·읽기 327MB/s(SD 약 28MB/s). 이전 중 2차 동기화가 SSD 부팅 후 다시 실행돼 같은 디스크에 중복본(95G)이 생겼고 소유자가 삭제. 무거운 I/O 중 저전압 경고가 기록돼 공식 5V 5A 전원 또는 별도 전원 허브를 권고. 재부팅으로 `/tmp` 스크래치패드가 지워져 node 실행 스크립트를 다시 만듦(`node:24.22.1-alpine` + `ao-node-modules` 볼륨).
158. PR #123 병합(`9af0f4c`), #58 종료. 운영 이미지에서 백업·검사·복원 시험 3회·예약 실행 실측 성공.
159. #68: `POST /api/v1/admin/backup/run|restore-test`(감사 로그 액션 형식 제약 때문에 `backup.restore_test`), 웹 `/admin`(경고 목록·지표·백업 버튼·사이트·가입 정책·SMTP·OAuth·설정 경계 안내). 서버 설정 테스트 20개·웹 63개 통과.
160. PR #124 병합(`8153abb`), #68 종료. 진행률 43/62.
161. #28: 관리자 사용자 API(목록·승인·비활성화(세션 폐기)·재활성화·역할 변경, 마지막 SUPER_ADMIN 트리거 409, 자기 비활성화 금지), 감사 로그 조회(커서·접두어), 남용 방지(인스턴스 전체 가입 한도·숨은 입력란·차단 도메인), 웹 `/admin` 사용자·감사 로그·차단 도메인. 서버 249개·웹 66개 테스트 통과.
162. PR #125 병합(`e7e2ce2`), #28 종료. 진행률 44/62.
163. #38 서버·계약: 이벤트 선택 필드 `plan_steps`(plan.updated에서만, 상태 enum 1~50개, 필드 없으면 digest 불변), 세션 계획 교체·늦은 계획 무시·상세 패널 갱신용 change_seq, 타임라인 커서 페이지 API(개인·팀). 서버 258개·스키마 패키지 테스트 통과. Collector 쪽 `plan_steps` 생성은 서브에이전트에 위임 예정.
164. PR #126 병합(`4eeb9a0`, #38 서버·계약). Go 정규화 테스트가 스키마 속성과 Event 키 일치를 검사해 CI 실패 → Go `Event.PlanSteps`(omitempty) 추가·검사 갱신. Collector 쪽 생성은 서브에이전트에 위임.
165. #70~#74: `notifications/`(고정 템플릿·지우기, 심각도 라우팅, 1시간 억제·복구 알림·열린 장애 유지, 채널 시간당 20건, 재시도 3회, 감사 로그 코드만), 공급자 Discord·Telegram·Teams Workflows·Email(공식 도메인·https만, 비밀 암호화), 백업·복원 시험·보존 정리·Heartbeat 연결, 관리자 채널 API·화면. 서버 276개·웹 68개 테스트 통과. 실제 외부 공급자 전송은 미검증. 문서 `docs/operations/notifications.md`.
166. PR #127 병합(`41446c7`), #70~#74 종료, Epic #12 종료. 진행률 49/62.
167. #61: 지표에 배포 상태(이미지 버전·리비전·기대 마이그레이션)와 보안 점검 목록(값 없이 코드·수준·안내), 관리 화면 표시(긴급·높음은 맨 위), 로그 보존 정책 문서화. 서버 277개·웹 69개 테스트 통과.
168. PR #129 병합(`3fb1982`), #61 종료. #38 서브에이전트 PR #128(Claude `TodoWrite` Hook·transcript와 Codex `update_plan` Hook에서 상태값만 읽어 `plan.updated` 추가 기록, 중복 제거 키 `plan.updated|tool_use_id`, 입력 형식 미실측이라 항상 `inferred`, 할 일·계획 문장 sentinel 비누출, fuzz)를 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`2d14891`), #38 종료, Epic #5 종료. 서브에이전트 사용량 약 12만 토큰. 미검증: 실제 Claude·Codex 계획 갱신 관측(#62), Codex Hook이 `update_plan`을 넘기는지.
169. 체크포인트: 종료 Epic #2·#4·#5·#7·#8·#12. 남은 Issue: #24 OAuth, #40~#44 Collector GUI, #60 배포·롤백, #62~#65 E2E·성능·보안·릴리스, #69 설치 방식 동일성, #87(Windows 전용).
170. 소유자가 사용량 27%라며 계속 진행을 지시(메모리에 반영: 내 추정으로 멈추지 않음). #24 OAuth를 서브에이전트에 위임.
171. #60: `deploy/upgrade.sh`(계획·`deploy` 입력 승인·새 이미지로 백업·`migrate-check` 사전 실행·교체·마이그레이션·점검·실패 시 DB 다운그레이드 후 이전 이미지 롤백·이력), `agent-office-backup migrate-check`·`expected-revision`, 문서 `docs/operations/deploy.md`. Pi에서 0005 이미지→0006 업그레이드(24초)와 점검 실패 롤백(0006→0005) 실측. 이전 이미지의 root 소유 백업 볼륨과, 준비 상태 엔드포인트가 없는 예전 이미지 롤백 점검 문제를 실측으로 발견해 처리.
172. PR #131 병합(`221fb8e`), #60 종료, Epic #9 종료. #24 서브에이전트 PR #132(OAuth Google·GitHub: PKCE S256, state 해시·10분·일회용+HttpOnly 쿠키 바인딩, 마이그레이션 `0007` `oauth_states`, 기존 identity 로그인·로그인 상태 연결·가입 정책 따른 신규 가입, 이메일 일치로 자동 연결하지 않음, 마지막 로그인 수단 해제 거부, `/account` 화면)를 최신 `develop` 병합·추적표 충돌 해결·CI 통과 후 병합(`2e754ba`), #24 종료, Epic #3 종료. 서브에이전트 약 16만 토큰. 미검증: 실제 Google·GitHub 로그인, id_token 서명 대신 userinfo 사용, callback `code`·`state`가 프록시 접근 로그에 남을 수 있음(문서화). #64 보안 시나리오를 서브에이전트에 위임.
173. #69: `deploy/compose.build.yaml`(운영 Compose에 build만 겹침), `tests/install/probe.py`·`compare.sh`(두 방식 새 볼륨 설치·외부 동작 비교), `docs/installation/install-methods.md`. Pi 실측에서 두 방식 동일(인증 이후 흐름 포함). 기존 개발 Compose가 프록시·보안 설정이 없어 운영 경로와 다르다는 점을 문서화.
174. PR #133 병합(`b64cc15`), #69 종료, Epic #11 종료. Collector 종단 테스트가 ACK 반영 전 종료로 CI에서 간헐 실패하던 문제를 같은 PR에서 수정. #64 서브에이전트 PR #134(보안 시나리오 38개: 모든 라우트 열거 멀티테넌시·권한 상승·CSRF·토큰·유출·입력 남용·세션 철회. 취약점 2건 수정: ADMIN이 SUPER_ADMIN 상태 변경 가능, NUL 문자 500·로그에 SQL 파라미터 노출)를 병합(`ef5ebb3`), #64 종료. 서브에이전트 약 26만 토큰. #40·#41 서브에이전트 PR #135(Wails v2.12 GUI, Core는 하위 프로세스로만, 웹 `/devices` 등록 코드, CI `Go GUI`)를 병합(`4425db5`), #40·#41은 macOS·Windows·키체인이 남아 열어 둠. 서브에이전트 약 24만 토큰.
175. #63: `tests/load`(부하 생성기·실행기·장애 주입·Playwright FPS), `tests/e2e`(실제 브라우저 화면 흐름, CI `E2E (browser)` 추가), WebSocket 주기 1초→0.5초. 실측(Pi 5+SSD): 표시 지연 P95 1.45초, 재시작 회복 3.4초, FPS 평균 39.8, E2E 7단계 통과, 1시간 실행 실패 0이나 경합 하 P95 2.73초. 부하 생성기가 처음엔 세션별로 보내 장치 속도 제한에 걸린 것을 실제 Collector 방식(장치당 배치)으로 수정. 72시간·경합 없는 장시간 재측정은 남음.
176. PR #136 병합(`1f6798e`, #63은 72시간 남아 열어 둠), #65 릴리스 노트 초안·절차 PR #137 병합(`2cc4c65`, 게시·main·태그는 승인 대기로 열어 둠).
177. #42: `collector/packaging`(6개 대상 정적 빌드, nfpm deb·rpm, tar.gz·zip, SHA256SUMS, 사용자별 제거 도우미 기본 보존·`--purge`), 게시 워크플로 `publish-collector.yml`(수동·`collector-publish` 승인·Release 초안), `docs/installation/collector-packages.md`. Debian deb·Fedora rpm 설치·제거 실측. 서명·macOS·Windows 패키지는 미구현.
178. PR #138 병합(`df46ff3`), #42는 서명·macOS·Windows가 남아 열어 둠.
179. #44: `internal/update`(ed25519 서명 manifest, 채널·OS·아키텍처·SemVer, SHA-256, 안전한 압축 해제, 원자적 교체·`.bak`, 서비스 재기동 후 상태 확인·실패 시 되돌림, 패키지 설치 거부), CLI `update check|apply`, 릴리스 도구 `cmd/agent-office-release`(keygen·manifest·sign), 빌드 시 공개 키 주입, 게시 워크플로에서 manifest 서명. Go 전체 테스트 통과.
180. PR #139 병합(`7e5e060`), #44 종료.
181. #62 Linux 부분: `tests/e2e/claude_flow.sh`로 실제 Claude Code 2.1.308 `claude -p` → Hook → Collector → 서버 흐름 실측(세션 시작~종료, 턴·도구 이벤트, 표식 문자열 DB 0건, 사용자 설정 불변). macOS·Windows·Codex·대화형은 남음.
182. #38·#62: 실제 Claude 하위 에이전트·TodoWrite 시나리오(`SCENARIO=subagent`)로 hook → spool → run 경로의 계획 상태 유실 2건 수정(spool 레코드 `plan` 필드, `run` spool 처리를 `Pipeline.Candidate`로). 실측 결과 하위 에이전트 lead 아래, `plan_steps` `["completed","completed"]`, 표식 DB 0건.
183. PR #142 병합(`797219a`).
184. #41·#43(PR #141): Core 장치 토큰을 OS 보안 저장소(go-keyring, 없으면 0600 파일 대체)에 보관·`logout [--local-only]`, Linux 트레이(fyne systray)·GUI 자동 시작. Linux arm64 Docker에서 실제 Secret Service·D-Bus 트레이 메뉴 검증. macOS Keychain·Windows Credential Manager·실제 데스크톱 패널·로그인 후 자동 시작은 미검증(#43 열어 둠).

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
4. 진행 순서(자율, 2026-10-24 갱신): #41·#43 PR #141 병합 후 #41 종료, #43은 실제 데스크톱 로그인 자동 시작 확인까지 열어 둠 → 72시간 실행. 실제 Claude 흐름 재확인은 `SCENARIO=subagent tests/e2e/claude_flow.sh ao-release-server:test ao-release-web:test <Collector 실행 파일>`. 이전 순서: #63 PR 병합(이슈는 72시간 실행까지 열어 둠) → #65 릴리스 준비 PR(`docs/#65-release-prep` 브랜치에 초안 커밋 있음, 장시간 결과 반영 후 PR) → #41·#43 서브에이전트 PR 병합 → #42 설치 패키지·#44 업데이트 → 72시간 실행(`AGENT_OFFICE_LOAD_PORT=18480 tests/load/run.sh <서버> <웹> --rounds 129600 --interval 2 --timeout 30`, 다른 무거운 작업 없을 때) → #62·#87은 소유자 장비. 서브에이전트 병렬은 최대 1개, PR 생성 후 조정자가 `git merge origin/develop`·추적표 충돌 해결(양쪽 유지)·CI 확인 후 병합. 각 PR은 CI 통과 후 `gh pr merge N --merge`(승인 자동)하고 `gh issue close N`, Epic 체크리스트를 `gh issue edit --body-file`로 갱신한다.
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
- 웹 검사는 `docker run … -v ao-node-modules:/src/node_modules node:24…`로 실행해 저장소에 `node_modules`를 만들지 않는다. 저장소에 빈 `node_modules` 마운트 지점이 생길 수 있다(Git 제외).
- Pi SD 카드는 I/O가 병목이다. 테스트 컨테이너는 `--tmpfs`로 작업 디렉터리·빌드 캐시를 RAM에 두고, 저장소에 `node_modules`를 남기지 않는다(필요 시 컨테이너 안 tmpfs에서 `npm ci`). Go 컨테이너는 한 번에 하나만 돌린다.
- Pi(SD 카드)에서 PostgreSQL 첫 기동이 2분 이상 걸릴 수 있다. healthcheck `start_period`를 줄이지 않는다.
- Codex app-server 제어 소켓은 `command/exec`·`fs/writeFile`·샌드박스 밖 `thread/shellCommand`를 허용한다. Collector가 연결하면 실행 권한을 갖게 되므로 ADR-0002 개정 없이 연결하지 않는다.
- `/tmp/codex-daemon-<uid>/`에 #21 실험용 해시 항목이 남았을 수 있다. 사용자 실제 daemon 항목과 구분할 수 없어 삭제하지 않았다.
- Claude Code `Stop`은 누락될 수 있다(#20, resume 1/4). 상태 엔진(#36)은 `Stop` 하나에 의존해 IDLE을 만들지 않는다.
- 로컬 테스트 이미지 `agent-office-test:py313`은 Pi에만 있는 캐시다. 의존성이 바뀌면 다시 만든다(`docs/project-status.md` 68번 참고).
- Windows PowerShell에서 `gh`로 한글 본문을 쓸 때 인코딩이 깨질 수 있다. 본문은 UTF-8 파일과 `--body-file`로만 전달하고 쓰기 후 다시 조회한다.
- Pi 콜드 빌드는 10분 이상 걸리고 첫 기동은 이미지 pull로 수 분이 걸린다. 운영 배포는 사전 빌드 이미지(GHCR) pull을 전제로 한다.
- Wails, Pi 실장비, macOS/Linux, 코드 서명·공증, SMTP/OAuth, 외부 Heartbeat는 각각 등록된 후속 이슈에서 결정한다.
