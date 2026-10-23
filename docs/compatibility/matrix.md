# 호환성 행렬

최종 갱신: 2026-10-23 (KST)

| 대상 | 관측 환경 | 상태 | 근거/다음 작업 |
|---|---|---|---|
| Codex CLI | Windows, `codex-cli 0.162.0-alpha.17.2` | 부분 지원 실측 | 합성 exec 4회; 시작·턴 제출·도구 전·Stop·종료 관측, `PostToolUse` 미관측. #17 |
| Codex Desktop | Windows 11 x64, Store `OpenAI.Codex 26.1007.2314.0`(번들 `codex-cli 0.162.0-alpha.17.2`) | 부분 관측(1회) | 프로젝트 Hook 신뢰 후 `UserPromptSubmit`·`PreToolUse`·`PostToolUse`·`PermissionRequest`·`SubagentStart/Stop`·`Stop`·`SessionEnd` 관측, `SessionStart`·compact·`Interrupt` 미관측/미실행. 반복 실측 전 지원 표시 금지. #18 |
| Codex Desktop | Raspberry Pi 5, Ubuntu 24.04 arm64 | 미설치 | 실측 불가 |
| Codex CLI | Raspberry Pi 5, `codex-cli 0.163.0` | 설치 확인, 이벤트 미실측 | 필요 시 #17 프로브로 재실측 |
| Codex lifecycle hooks | CLI 합성 세션, Desktop 합성 세션 | 부분 지원 실측 | CLI inline 4회(#17), Desktop 프로젝트 Hook 1회(#18). 승인 거부·subagent는 Desktop만 관측; interrupt·compact 미실측 |
| Codex app-server | CLI 명령 제공 여부 미실측 | 미검증 | #21 |
| Claude Code CLI | Windows | 실행 파일 미설치 | #19 |
| Claude Code CLI | Raspberry Pi 5, `2.1.308` | 설치 확인, Hook/JSONL 미실측 | #19 |
| Go toolchain | Windows | 호스트 미설치 | 공식 Go 1.24 컨테이너로 fmt/vet/race test 통과 |
| Docker CLI | 29.8.1 | 엔진 기동·검증 | server/web build와 개발 Compose smoke test 통과 |
| Docker Compose | v5.5.1 | 구성·기동 검증 | 개발 및 production overlay config 통과 |
| Python | 3.13.9 | 설치·실행 가능 | #13 서버 검사 |
| Node/npm | 24.19.0 / 11.17.0 | 설치·실행 가능 | #13 웹 검사 |
| Raspberry Pi 5 | 8GB, Ubuntu 24.04.5, aarch64, Docker 29.9.2/Compose v5.6.1 | 호스트 확인, 이미지 빌드·기동 미실측. 루트가 SD 카드 | #56, #63 |
| macOS/Linux Collector | 없음 | 미실측 | #42, #62 |

`부분 지원`, `미실측`, `미검증`은 전체 지원 선언이 아닙니다. 실제 로그 원문은 이 문서나 저장소에 복사하지 않습니다.
