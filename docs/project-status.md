# 프로젝트 상태와 세션 인수인계

최종 갱신: 2026-10-23 (KST)

기준 문서: Agent Office 마스터 개발 프롬프트 v0.8

저장소: `apg0001/Agent-Office`

이 문서는 세션 한도, 담당자 변경, 도구 장애가 발생해도 다음 에이전트가 작업을 재구성하지 않고 이어가기 위한 단일 인수인계 기준입니다. 작업 시작과 종료 시 반드시 갱신합니다.

## 현재 위치

- 활성 작업: #18 `EP-02/ISS-02 Codex Desktop 세션·도구·하위 에이전트 이벤트 실측`
- 브랜치: `feat/#18-codex-desktop-poc`
- 기준 브랜치: `develop` (`f14668a`, PR #79 병합 결과)
- 상태: Issue·브랜치 준비와 #17 결과 인수인계 완료, Desktop 안전 실측 설계 전
- 직전 완료: #17, PR https://github.com/apg0001/Agent-Office/pull/79, merge commit `f14668a`
- 배포/릴리스: 수행하지 않음
- 실제 사용자 수집: 수행하지 않음. 합성 Codex CLI 세션만 실행

## GitHub 연결과 백로그

- 로컬 GitHub CLI: `apg0001` 인증, 저장소 `ADMIN`, `repo`/`workflow` 범위 확인.
- 원격: `https://github.com/apg0001/Agent-Office.git`.
- 로컬 `gh`로 라벨 생성과 Issue 쓰기 성공.
- 별도 GitHub 앱 커넥터의 이슈 생성은 `403 Resource not accessible by integration`; 이후 쓰기는 검증된 로컬 `gh`만 사용.
- 등록 이슈: 74개 = Epic 12개(#1~#12) + 세부 작업 62개(#13~#74). Epic #1과 #13~#17은 완료·종료됨.
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

## 직전 완료 변경 요약(#17)

- 원문 없이 Hook 입력·Codex JSONL·합성 transcript 구조만 축약하는 PoC 하네스.
- 민감 값·경로 미보존, fail-open, transcript 범위 제한 회귀 테스트 3개.
- Codex CLI inline 합성 실행 4회와 이벤트·중복·종료 의미 실측 보고서.
- `PostToolUse` 미관측을 숨기지 않은 호환성·정규화 제한과 후속 범위.

## 검증 상태

| 검사 | 상태 | 근거 |
|---|---|---|
| Python 설치/pytest/Ruff/mypy | 통과 | pytest 24개 통과(Starlette TestClient deprecation 경고 1개), Ruff lint·format·mypy 오류 0 |
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
| GitHub Actions 원격 CI | 통과 | PR #79 최신 run `39888846155`: 하위 5개 job과 `CI Gate` 모두 성공 |
| Gitleaks 전체 이력 | 통과 | 8.30.1 컨테이너로 6 commit·약 136KB 검사, leak 0 |
| 실제 Codex CLI Hook 이벤트 | 부분 통과 | inline 합성 실행 4회: 5종 관측, 중복 0, `PostToolUse` 미관측; 상세 #17 보고서 |
| Claude 이벤트 | 미실행 | Claude CLI 미설치, #19 |
| npm audit / pip check | 통과 | 취약점 0, 깨진 Python 의존성 0 |
| diff 공백·비밀·개인 경로 검사 | 통과 | `git diff --check`, 대표 비밀/절대경로 패턴 일치 0 |

## 다음 에이전트가 바로 수행할 작업

1. `computer-use` 스킬 지침을 읽고 현재 Codex Desktop을 안전하게 관측할 수 있는 UI 범위와 중단 조건을 정한다.
2. 기존 대화·사용자 transcript 원문을 열지 않고 Desktop 버전, 합성 세션 생성 가능 여부, Hook 신뢰 흐름을 확인한다.
3. 합성 세션으로 시작·턴·도구·승인·하위 에이전트·종료 이벤트를 각각 시도하고 관측/미관측을 구조 통계로만 기록한다.
4. CLI #17과 Desktop 결과가 같은 세션 저장소를 공유하는지 값이 아닌 경로 템플릿·HMAC 상관관계로 검증한다.
5. 개인정보 회귀 테스트·호환성 보고서·인수인계를 갱신하고 PR의 필수 `CI Gate`를 통과시킨다.

## 알려진 위험과 결정 대기

- 라이선스는 소유자 결정 전까지 미정이며 `LICENSE`를 만들지 않았다.
- 호스트 Go는 없지만 공식 Go 컨테이너로 현재 모듈 검증을 완료했다.
- 로컬 8000 포트가 이미 사용 중이어서 Compose smoke test는 18000/15173 포트로 수행했다.
- Claude CLI가 없어 관련 검증은 수행할 수 없다.
- 기존 사용자 Codex DB/JSONL 원문은 읽지 않았다. #17에서는 Hook이 직접 제공한 합성 세션 transcript만 구조 키·개수로 축약했으며 원문은 저장소에 남기지 않았다.
- #17 실행으로 Codex가 관리하는 합성 세션 transcript 8개가 사용자 Codex 상태에 생성됐다. 원본 도구 기록을 임의 삭제하지 않는 원칙에 따라 그대로 두었으며 실제 사용자 내용은 없다.
- Codex CLI 0.162.0-alpha.17.2에서 inline `PostToolUse`가 네 번 모두 미관측이었다. 공식 문서와 차이가 있으므로 이 버전의 Hook만으로 `tool.completed`를 만들지 않는다.
- 임시 프로젝트 `hooks.json`은 `--ignore-user-config` 비대화형 조합에서 발견되지 않았고 inline `-c` Hook은 동작했다. 대화형 신뢰 UI와 일반 프로젝트 탐색 지원은 아직 주장하지 않는다.
- 이벤트 v1은 payload 계약만 확정했으며 수집 엔드포인트·인증·DB 영속화는 아직 없다. 부모 참조 존재·순환, 자격증명과 `device_id` 일치, 같은 `event_id`의 내용 충돌은 서버 구현에서 검증해야 한다.
- 현재 provider/event enum은 마스터 프롬프트 기반 기준선이다. #17~#21 실제 PoC에서 새 값이 필요하면 원문을 통과시키지 말고 `unknown`을 사용한 뒤 버전형 계약 변경으로 심사한다.
- Wails, Pi 실장비, macOS/Linux, 코드 서명·공증, SMTP/OAuth, 외부 Heartbeat는 각각 등록된 후속 이슈에서 결정한다.
