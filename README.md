# Agent Office

여러 컴퓨터에서 실행되는 AI 코딩 에이전트의 **관측된 활동 메타데이터**를 아이소메트릭 가상 사무실로 표현하는 멀티유저·멀티팀 셀프 호스팅 플랫폼입니다.

> 현재 상태: 기반 구축 단계입니다. 실제 사용자 데이터 수집, 프로덕션 배포, Stable 릴리스는 시작하지 않았습니다.

## 핵심 원칙

- 원본 프롬프트, 소스 코드, 파일 본문, 도구 입출력, 전체 명령은 서버로 전송하지 않습니다.
- Hook과 Collector는 fail-open, 짧은 타임아웃, 비차단을 기본으로 합니다.
- 관측된 상태와 추정 상태, 순수 애니메이션 연출을 명시적으로 구분합니다.
- 지원 여부는 실제 PoC로 검증하며 미지원/미검증 기능을 숨기지 않습니다.
- 프로덕션 배포, 외부 공개, 비가역 마이그레이션과 Stable 릴리스는 관리자 승인 후에만 수행합니다.

## 저장소 구조

```text
apps/server/             FastAPI API와 상태 엔진
apps/web/                React + TypeScript + Phaser 웹 UI
collector/core/          Go Collector 코어
collector/adapters/      제공자별 검증된 어댑터
packages/event-schema/   버전형 공통 이벤트 계약과 합성 fixture
deploy/                  개발/운영 Compose와 환경 변수 예시
tests/                   저장소 수준 통합/E2E 테스트
docs/                    아키텍처, 호환성, 운영, 의사결정, 인수인계
```

## 빠른 시작

필수 도구는 Python 3.13+, Node.js 24+, npm 11+, Go 1.24+, Docker Compose입니다.

```powershell
# 서버
python -m venv .venv
.\.venv\Scripts\python -m pip install -e ".\apps\server[dev]"
.\.venv\Scripts\python -m pytest apps/server/tests tests

# 웹
npm install
npm run test
npm run build

# Collector
go test ./collector/...

# Compose 계약 검사
docker compose --env-file deploy/.env.example -f deploy/compose.yaml config
```

상세 절차는 [개발 환경 문서](docs/installation/development.md), 현재 진행 상황과 다음 작업은 [프로젝트 상태](docs/project-status.md)를 참고하세요.

## 이슈와 기여

- 전체 Epic과 세부 작업: [GitHub Issues](https://github.com/apg0001/Agent-Office/issues)
- 기여 절차: [CONTRIBUTING.md](CONTRIBUTING.md)
- 보안 정책: [SECURITY.md](SECURITY.md)

라이선스는 저장소 소유자의 결정 전까지 미정입니다. 라이선스가 확정되기 전에는 재배포 조건을 추정하지 마세요.
