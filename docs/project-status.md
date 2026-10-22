# 프로젝트 상태와 세션 인수인계

최종 갱신: 2026-10-22 (KST)

기준 문서: Agent Office 마스터 개발 프롬프트 v0.8

저장소: `apg0001/Agent-Office`

이 문서는 세션 한도, 담당자 변경, 도구 장애가 발생해도 다음 에이전트가 작업을 재구성하지 않고 이어가기 위한 단일 인수인계 기준입니다. 작업 시작과 종료 시 반드시 갱신합니다.

## 현재 위치

- 활성 작업: #13 `EP-01/ISS-01 Monorepo 구조·개발 환경·Compose dev 구성`
- 브랜치: `feat/#13-monorepo-bootstrap`
- 기준 브랜치: `develop` (2026-10-22에 `main`에서 생성해 원격 push 완료)
- 상태: 구현·로컬 검증 완료, 아직 커밋·push·PR 없음
- 배포/릴리스: 수행하지 않음
- 실제 사용자 수집: 수행하지 않음

## GitHub 연결과 백로그

- 로컬 GitHub CLI: `apg0001` 인증, 저장소 `ADMIN`, `repo`/`workflow` 범위 확인.
- 원격: `https://github.com/apg0001/Agent-Office.git`.
- 로컬 `gh`로 라벨 생성과 Issue 쓰기 성공.
- 별도 GitHub 앱 커넥터의 이슈 생성은 `403 Resource not accessible by integration`; 이후 쓰기는 검증된 로컬 `gh`만 사용.
- 열린 이슈: 74개 = Epic 12개(#1~#12) + 세부 작업 62개(#13~#74).
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
| Docker 엔진 | 미실행: Linux engine named pipe 없음 |
| Go | 미설치 |
| Codex CLI | 0.162.0-alpha.17.2 |
| Claude CLI | 미설치(로컬 `.claude` 상태 디렉터리는 존재) |

## 이번 세션에서 완료한 작업

1. 마스터 프롬프트 전체를 UTF-8로 확인하고 첫 실행 범위를 EP-01/EP-02로 확정.
2. 공식 OpenAI 문서에서 Codex lifecycle Hook과 app-server 계약을 확인. Hook은 신뢰 검토가 필요하며 transcript 형식은 안정 인터페이스가 아니라는 전제로 PoC 설계.
3. 저장소·권한·도구·로컬 상태를 읽기 전용 점검.
4. 추적 라벨 10개와 전체 74개 이슈 생성, Epic 체크리스트 연결 및 개수 검증.
5. `develop` 원격 브랜치와 `feat/#13-monorepo-bootstrap` 작업 브랜치 생성.
6. #13용 Python/Node/Go 최소 진입점, 개발 Compose, 개발·아키텍처·운영 기준 문서 작성.

## 변경 파일 요약(#13)

- 루트: README, ignore/editor 설정, npm workspace, Python 개발 의존성.
- 서버: 최소 FastAPI health API, 테스트, 컨테이너 정의.
- 웹: React/Vite/TypeScript/Phaser 패키지, 출시 전 상태 화면, 테스트, 컨테이너 정의.
- Collector: 독립 Go 모듈과 수집 비활성 진입점.
- 배포: PostgreSQL/server/web 개발 Compose와 안전한 환경 예시.
- 문서: 아키텍처, 개발 설치, 호환성, 운영 placeholder, ADR, 요구사항 추적, 인수인계.

## 검증 상태

| 검사 | 상태 | 근거 |
|---|---|---|
| Python 설치/pytest/Ruff/mypy | 통과 | pytest 1개 통과(Starlette TestClient deprecation 경고 1개), Ruff·mypy 오류 0 |
| npm install/typecheck/test/build | 통과 | Vitest 1개 통과, TypeScript 오류 0, Vite production build 성공 |
| Go test | 미실행 | Go 미설치 |
| Compose config | 통과 | `docker compose ... config --quiet` 성공 |
| Docker image build/up | 미실행 | Docker 엔진 미실행 |
| 실제 Codex Hook 이벤트 | 미실행 | #17, #18 |
| Claude 이벤트 | 미실행 | Claude CLI 미설치, #19 |
| npm audit / pip check | 통과 | 취약점 0, 깨진 Python 의존성 0 |
| diff 공백·비밀·개인 경로 검사 | 통과 | `git diff --check`, 대표 비밀/절대경로 패턴 일치 0 |

## 다음 에이전트가 바로 수행할 작업

1. `git status --short --branch`로 이 문서의 브랜치와 변경 목록을 대조한다.
2. 변경 목록과 잠금 파일을 검토하고 `git diff --check`를 다시 실행한다.
3. 한국어 커밋 후 원격 브랜치에 push하고 `develop` 대상 PR을 생성한다. PR은 `Closes #13`을 포함한다.
4. CI와 리뷰가 완료되기 전 #13을 닫거나 완료로 표시하지 않는다.
5. Docker 엔진을 사용할 수 있는 환경에서는 image build/up/health를 추가 검증하고 PR에 결과를 남긴다.
6. #13 병합 후 다음 순서는 #14 → #15 → #16이며, EP-02 #17~#21은 계약 #15를 기준으로 PoC한다.

## 알려진 위험과 결정 대기

- 라이선스는 소유자 결정 전까지 미정이며 `LICENSE`를 만들지 않았다.
- Go와 Claude CLI가 없어 관련 검증은 수행할 수 없다.
- Docker 엔진이 꺼져 있어 실제 컨테이너 build/up은 확인하지 못했다.
- Codex 로컬 DB/JSONL 원문은 민감정보 보호를 위해 아직 읽지 않았다. #17/#18에서 스키마 키와 안전한 합성 fixture만 추출한다.
- Wails, Pi 실장비, macOS/Linux, 코드 서명·공증, SMTP/OAuth, 외부 Heartbeat는 각각 등록된 후속 이슈에서 결정한다.
