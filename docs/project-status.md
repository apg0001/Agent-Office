# 프로젝트 상태와 세션 인수인계

최종 갱신: 2026-10-23 (KST)

기준 문서: Agent Office 마스터 개발 프롬프트 v0.8

저장소: `apg0001/Agent-Office`

이 문서는 세션 한도, 담당자 변경, 도구 장애가 발생해도 다음 에이전트가 작업을 재구성하지 않고 이어가기 위한 단일 인수인계 기준입니다. 작업 시작과 종료 시 반드시 갱신합니다.

## 현재 위치

- 활성 작업: #19 `EP-02/ISS-03 Claude Code Hook/JSONL 및 병렬 에이전트 실측`
- 브랜치: `feat/#19-claude-code-hook-poc` (`develop` `9145d8c`에서 생성, #18의 Pi 기준선 커밋을 cherry-pick)
- 상태: Pi Claude Code 2.1.308 합성 실측 10회와 보고서·회귀 테스트 완료, PR·CI 진행
- 다음 예정: #56 ARM64 Docker 이미지·Compose 실측(소유자 지시: #19 다음)
- 보류 작업: #18 `EP-02/ISS-02 Codex Desktop 세션·도구·하위 에이전트 이벤트 실측`
- #18 브랜치: `feat/#18-codex-desktop-poc` (`develop` `9145d8c` 병합 완료, PR 없음)
- 기준 브랜치: `develop` (`9145d8c`, PR #81 병합 결과)
- 상태: **#18 보류(소유자 결정, 2026-10-23).** 작업 호스트가 Windows PC에서 Raspberry Pi 5(Linux arm64)로 바뀌었고, Pi에는 Codex Desktop이 없으며 Desktop UI 자동화도 금지돼 있어 실측할 수 없음. app-server 비-UI 관측은 #18이 아니라 #21 범위로 둠
- 재개 조건: Codex Desktop이 설치된 PC에서 소유자 또는 수동 운영자가 합성 세션을 직접 조작할 수 있을 때
- 직전 완료: #80, PR https://github.com/apg0001/Agent-Office/pull/81, merge commit `9145d8c`
- 배포/릴리스: 수행하지 않음
- 실제 사용자 수집: 수행하지 않음. 합성 Codex CLI 세션(#17)과 합성 Claude Code `-p` 세션(#19)만 실행
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

## 이번 변경 요약(#19)

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
| 실제 Claude Code Hook 이벤트 | 부분 통과 | Pi Linux arm64, 2.1.308 `-p` 합성 10회: 9종 관측, 반복 재현, `tool_use_id`·`agent_id` 상관관계 일치; 대화형·승인 대기 미실측. 상세 #19 보고서 |
| Pi 호스트 Python 회귀 | 통과 | `python:3.13-slim`(arm64) 컨테이너: #19 포함 pytest 34개 통과(Starlette deprecation 경고 1개), Ruff lint·format, server mypy, Claude 프로브 mypy strict 통과. 호스트에는 pytest·`python3-venv` 없음 |
| Pi 호스트 Node·Go·Compose 회귀 | 미실행 | Node·Go 호스트 미설치, 컨테이너 재검증은 다음 Issue 착수 시 수행 |
| npm audit / pip check | 통과 | 취약점 0, 깨진 Python 의존성 0 |
| diff 공백·비밀·개인 경로 검사 | 통과 | `git diff --check`, 대표 비밀/절대경로 패턴 일치 0 |
| 마스터 사양 기준선 | 통과 | 외부 원본과 저장소 사본 44,535바이트·SHA-256 일치, 요구사항 범위·문서 링크 테스트 3개 통과 |

## 다음 에이전트가 바로 수행할 작업

1. #19 PR의 `CI Gate` 결과를 확인한다: `gh pr checks <PR 번호> --watch`. 실패하면 로그를 보고 수정 커밋을 올린다.
2. 소유자 승인 후 `gh pr merge <PR 번호> --merge`로 `develop`에 병합하고, `gh issue close 19`와 Epic #2 체크리스트(#19 체크)를 갱신한다.
3. #56 시작: `git switch develop && git pull --ff-only && git switch -c feat/#56-multiarch-images`.
4. #56 실측: Pi에서 `docker compose -f deploy/compose.yaml config --quiet`, `docker compose -f deploy/compose.yaml build`, 기동, `curl` health를 확인한다. 8000 포트 충돌은 이 Pi에서 다시 확인한다. 빌드 시간·메모리, SD 카드 볼륨 위치를 기록하고 테스트 컨테이너·볼륨은 정리한다.
5. #56 범위의 GHCR 멀티아키텍처 빌드(`docker buildx`)와 Actions 연동은 push·공개 영향이 있으므로 소유자 승인 전에는 workflow 작성과 로컬 검증까지만 한다.
6. #18 재개: Codex Desktop이 설치된 PC에서 소유자가 합성 세션을 직접 조작할 수 있을 때 #17 프로브(`tests/poc/codex`)로 시작·턴·도구·승인·하위 에이전트·종료를 구조 통계로만 기록한다. 소유자에게 전달한 데스크톱용 지시문은 #18 Issue 댓글의 재개 조건을 따른다.
7. #19 후속(별도 Issue 후보): 대화형 TUI 세션에서 승인 대기·`Notification`·중단·compact 관측. 자동화 금지 원칙상 소유자가 직접 조작해야 한다.

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
- Computer Use 안전 지침은 ChatGPT/Codex Desktop UI 자동화를 금지한다. #18의 Desktop 상호작용은 사용자/수동 운영자가 수행해야 하며, 자동화 우회나 보안·신뢰 프롬프트 조작을 시도하지 않는다.
- 이벤트 v1은 payload 계약만 확정했으며 수집 엔드포인트·인증·DB 영속화는 아직 없다. 부모 참조 존재·순환, 자격증명과 `device_id` 일치, 같은 `event_id`의 내용 충돌은 서버 구현에서 검증해야 한다.
- 현재 provider/event enum은 마스터 프롬프트 기반 기준선이다. #17~#21 실제 PoC에서 새 값이 필요하면 원문을 통과시키지 말고 `unknown`을 사용한 뒤 버전형 계약 변경으로 심사한다.
- Wails, Pi 실장비, macOS/Linux, 코드 서명·공증, SMTP/OAuth, 외부 Heartbeat는 각각 등록된 후속 이슈에서 결정한다.
