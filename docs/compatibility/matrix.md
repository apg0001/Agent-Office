# 호환성 행렬

최종 갱신: 2026-10-22 (KST)

| 대상 | 관측 환경 | 상태 | 근거/다음 작업 |
|---|---|---|---|
| Codex CLI | Windows, `codex-cli 0.162.0-alpha.17.2` | 설치 확인, 이벤트 미실측 | #17 |
| Codex Desktop | 현재 Codex 작업 환경 | 로컬 상태 디렉터리 존재, 이벤트 미실측 | #18 |
| Codex lifecycle hooks | 공식 문서 확인 | PoC 필요 | #17, #18 |
| Codex app-server | CLI 명령 제공 여부 미실측 | 미검증 | #21 |
| Claude Code CLI | Windows | 실행 파일 미설치 | #19 |
| Go toolchain | Windows | 미설치 | #13 검증 제한, #29~#34 구현 전 필요 |
| Docker CLI | 29.8.1 | 설치됨 | 엔진 미실행으로 build/up 미실행 |
| Docker Compose | v5.5.1 | 설치됨 | `config` 검사 가능 |
| Python | 3.13.9 | 설치·실행 가능 | #13 서버 검사 |
| Node/npm | 24.19.0 / 11.17.0 | 설치·실행 가능 | #13 웹 검사 |
| Raspberry Pi 5 | 없음 | 미실측 | #56, #63 |
| macOS/Linux Collector | 없음 | 미실측 | #42, #62 |

`미실측`과 `미검증`은 지원 선언이 아닙니다. 실제 로그 원문은 이 문서나 저장소에 복사하지 않습니다.
