# Collector adapters

Codex와 Claude 제공자별 어댑터가 위치할 디렉터리입니다. EP-02 PoC에서 실제로 검증된 이벤트와 버전만 구현합니다. 미검증 형식은 `unknown` 또는 `unsupported`로 처리하며 원본 로그를 수정하거나 삭제하지 않습니다.

Codex app-server는 기본 수집 경로가 아닙니다([ADR-0002](../../docs/decisions/0002-collector-source-priority.md), [실측](../../docs/poc/codex-app-server.md)). 제어 소켓은 실행·쓰기 권한 채널이므로 연결하지 않습니다.

Codex CLI 0.162.0-alpha.17.2의 현재 관측 기준은 [PoC 보고서](../../docs/poc/codex-cli-hooks.md)를 따릅니다. 이 버전에서 `PostToolUse`를 관측하지 못했으므로 Hook만으로 `tool.completed`를 만들지 않습니다. `UserPromptSubmit.prompt`, 도구 입력·응답, cwd와 transcript 경로 원문은 어댑터 허용 필드가 아닙니다.

Claude Code 2.1.308(Linux arm64, `-p`)의 현재 관측 기준은 [PoC 보고서](../../docs/poc/claude-code-hooks.md)를 따릅니다. `PreToolUse`·`PostToolUse`가 같은 `tool_use_id`로 관측되므로 Hook으로 `tool.started`·`tool.completed`를 만들 수 있습니다. 하위 에이전트는 `SubagentStart`·`SubagentStop`의 `agent_id`로만 생성합니다. `Agent` 도구의 `PostToolUse`와 `Stop`은 하위 에이전트 종료나 유휴의 근거가 아닙니다. `prompt`, `tool_input`, `tool_response`, `last_assistant_message`, `background_tasks`·`session_crons` 항목의 설명·명령·프롬프트, cwd와 transcript 경로 원문은 어댑터 허용 필드가 아닙니다. Hook은 짧은 `timeout`을 명시하고 종료 코드 2를 쓰지 않습니다.

Hook과 transcript를 함께 읽을 때는 [중복 제거와 tail 규칙](../../docs/poc/collector-dedup-tail.md)을 따릅니다. transcript에서는 `assistant`·`user` 줄만 활동으로 읽고, `tool_use_id`·`prompt_id` 기반 결정적 키로 중복을 제거합니다. `Stop`이 오지 않을 수 있으므로 턴 종료는 다음 `UserPromptSubmit`이나 `SessionEnd`로도 닫습니다.

## Go 구현(#29)

어댑터 코드는 `collector/core/internal/adapter`(공통 후보·중복 제거), `.../adapter/claude`(Hook·transcript 줄), `.../adapter/codex`(Hook)에 있습니다. 패키지 구조, 화이트리스트, 버전 검증 수준(`verified`/`partial`/`unverified`), 활동 분류와 참조 구현 일치 검증은 [Collector 입력 어댑터](../../docs/architecture/collector-adapters.md)에 정리했습니다. 이 디렉터리는 문서 위치로 유지합니다.
