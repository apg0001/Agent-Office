# Collector adapters

Codex와 Claude 제공자별 어댑터가 위치할 디렉터리입니다. EP-02 PoC에서 실제로 검증된 이벤트와 버전만 구현합니다. 미검증 형식은 `unknown` 또는 `unsupported`로 처리하며 원본 로그를 수정하거나 삭제하지 않습니다.

Codex CLI 0.162.0-alpha.17.2의 현재 관측 기준은 [PoC 보고서](../../docs/poc/codex-cli-hooks.md)를 따릅니다. 이 버전에서 `PostToolUse`를 관측하지 못했으므로 Hook만으로 `tool.completed`를 만들지 않습니다. `UserPromptSubmit.prompt`, 도구 입력·응답, cwd와 transcript 경로 원문은 어댑터 허용 필드가 아닙니다.
