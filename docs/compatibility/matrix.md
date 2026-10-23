# 호환성 행렬

최종 갱신: 2026-10-23 (KST)

| 대상 | 관측 환경 | 상태 | 근거/다음 작업 |
|---|---|---|---|
| Codex CLI | Windows, `codex-cli 0.162.0-alpha.17.2` | 부분 지원 실측 | 합성 exec 4회; 시작·턴 제출·도구 전·Stop·종료 관측, `PostToolUse` 미관측. #17 |
| Codex Desktop | Windows 개발 PC | 로컬 상태 디렉터리 존재, 이벤트 미실측 | #18 보류(수동 조작 필요) |
| Codex Desktop | Raspberry Pi 5, Ubuntu 24.04 arm64 | 미설치 | 실측 불가 |
| Codex CLI | Raspberry Pi 5, `codex-cli 0.163.0` | 설치 확인, 이벤트 미실측 | 필요 시 #17 프로브로 재실측 |
| Codex lifecycle hooks | CLI 합성 세션 | 부분 지원 실측 | inline Hook만 확인; Desktop·승인·interrupt·subagent 미실측. #17, #18 |
| Codex app-server | CLI 명령 제공 여부 미실측 | 미검증 | #21 |
| Claude Code CLI | Windows | 실행 파일 미설치 | #19 |
| Claude Code CLI | Raspberry Pi 5 Linux arm64, `2.1.308`, `-p` | 부분 지원 실측 | 합성 10회; 시작·턴·도구 전/후·하위 에이전트 시작/종료·권한 요청·Stop·종료 관측. 대화형 TUI·승인 대기·`Notification`·compact 미실측. #19 |
| Claude Code CLI | Windows·macOS | 미실측 | #19 결과를 확장 적용하지 않음 |
| Claude Code Hook·JSONL 교차 중복 | Pi Linux arm64, `2.1.308`, `-p` | 규칙 도출·참조 구현 | `tool_use_id`·`prompt_id` 키 일치, resume append-only, `Stop` 1/4 누락. compact·fork·삭제 미실측. #20 |
| Go toolchain | Windows | 호스트 미설치 | 공식 Go 1.24 컨테이너로 fmt/vet/race test 통과 |
| Docker CLI | 29.8.1 | 엔진 기동·검증 | server/web build와 개발 Compose smoke test 통과 |
| Docker Compose | v5.5.1 | 구성·기동 검증 | 개발 및 production overlay config 통과 |
| Python | 3.13.9 | 설치·실행 가능 | #13 서버 검사 |
| Node/npm | 24.19.0 / 11.17.0 | 설치·실행 가능 | #13 웹 검사 |
| Raspberry Pi 5 | 8GB, Ubuntu 24.04.5, aarch64, Docker 29.9.2/Compose v5.6.1 | arm64 이미지 빌드·개발/운영 Compose 기동·health·자동 재시작 실측 | 재부팅·장시간·SSD 미실측. 루트가 SD 카드. #56, #63 |
| linux/amd64·arm64 이미지 | GitHub `ubuntu-24.04`·`ubuntu-24.04-arm` 러너 | CI 네이티브 빌드·기동·health 통과 | PR #83 run `39905138626` (amd64 41초, arm64 39초). #56 |
| macOS/Linux Collector | 없음 | 미실측 | #42, #62 |

`부분 지원`, `미실측`, `미검증`은 전체 지원 선언이 아닙니다. 실제 로그 원문은 이 문서나 저장소에 복사하지 않습니다.
