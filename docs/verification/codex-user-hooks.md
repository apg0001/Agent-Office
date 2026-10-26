# Codex 사용자 범위 Hook 실측(#170)

관련 이슈: #170. 선행: #17(CLI inline Hook), #18(Desktop 프로젝트 `.codex/hooks.json`), ADR-0002(Codex는 Hook만 수집).

실측일: 2026-10-26 (KST). 환경: Raspberry Pi 5 arm64, Ubuntu 24.04.5, `codex-cli 0.163.0`(`aarch64-unknown-linux-musl` standalone, 사용자 설치).

## 결론

| 항목 | 결과 |
|---|---|
| 사용자 범위 위치 | `$CODEX_HOME/hooks.json`(기본 `~/.codex/hooks.json`). 같은 폴더 `config.toml`의 `[hooks]` TOML도 읽음(둘 다 있으면 경고) |
| 형식 | `{"description"?: 문자열, "hooks": {"<Event>": [{"matcher"?: 문자열, "hooks": [{"type": "command", "command": …, "timeout"?: 초}]}]}}`. 최상위에 다른 키가 있으면 파일 전체를 거부(`deny_unknown_fields`) |
| 신뢰 | 관리(managed)가 아닌 Hook은 신뢰해야 실행됨. 신뢰 기록은 `$CODEX_HOME/config.toml`의 `[hooks.state."<hooks.json 절대 경로>:<event_snake>:<그룹 번호>:<핸들러 번호>"] trusted_hash = "sha256:…"` |
| 신뢰 전 | `codex exec`에서 Hook 0회 실행, 경고 없음(조용히 건너뜀) |
| 신뢰 방법 | 대화형 `codex` 시작 시 "Hooks need review / N hooks are new or changed" 화면 → `1. Review hooks` 또는 `2. Trust all and continue`(실행 중에는 `/hooks`). 이 Pi에서는 TUI에서 2를 골라 9개 `trusted_hash`가 기록됨 |
| 핸들러 변경 | 명령을 바꾼 핸들러 1개만 실행 안 됨(해시 불일치), 나머지는 계속 실행 |
| 타임아웃 | 생략 시 600초. `SessionEnd`·`Interrupt`는 기본 1초, 최대 3초(넘으면 3초로 자르고 경고) |
| 관측 이벤트(4회 반복, 매회 1번) | `SessionStart`(source `startup`), `UserPromptSubmit`, `PreToolUse`·`PostToolUse`(tool_name `Bash`, `tool_use_id`), `Stop`, `SessionEnd`(reason `other`) |
| 미관측 | `PermissionRequest`, `SubagentStart`, `SubagentStop`(시나리오에 승인·하위 에이전트 없음) |
| Hook 환경 변수 | Hook 프로세스가 `codex` 실행 환경을 물려받음(`AGENT_OFFICE_COLLECTOR_HOME`이 Hook까지 전달됨, 종단 실측) |

macOS·Windows CLI에서는 실행하지 않았습니다. 소스상 위치 규칙은 OS와 무관(`CODEX_HOME`, 기본 `~/.codex`)하지만 실기 검증 전까지 macOS·Windows는 미검증입니다.

## 1차 출처

`openai/codex` 태그 `rust-v0.163.0`(커밋 `83086188ae0614bd882818ff1aa23f3194962012`)의 소스를 읽었습니다.

- `codex-rs/hooks/src/engine/discovery.rs`: 설정 계층마다 `hooks_config_folder()`의 `hooks.json`(`load_hooks_json`)과 계층 TOML `[hooks]`(`load_toml_hooks_from_layer`)를 읽음. 사용자 계층(`ConfigLayerSource::User`)은 `HookSource::User`, 비관리. 핸들러는 `enabled`이고 `bypass_hook_trust`이거나 신뢰 상태가 `Managed`·`Trusted`일 때만 실행 목록에 들어감. 해시는 정규화한 `{event_name, matcher, hooks:[handler]}`의 TOML 표현(`version_for_toml`)이라 실행 파일 경로·타임아웃이 바뀌면 다시 신뢰해야 함. `normalize_command_hook`이 SessionEnd·Interrupt 타임아웃을 1~3초로 자름
- `codex-rs/hooks/src/lib.rs` `hook_key`: `"{key_source}:{event_snake}:{group_index}:{handler_index}"`. 위치 번호가 키에 들어가므로 앞쪽 그룹이 지워지면 뒤 항목의 신뢰가 풀릴 수 있음
- `codex-rs/hooks/src/config_rules.rs` `hook_states_from_stack`: 신뢰·사용 여부는 사용자 계층과 세션 플래그(`-c`) 계층의 `hooks.state`만 읽음
- `codex-rs/config/src/hook_config.rs`: `HooksFile`(`deny_unknown_fields`, `description`·`hooks`), 이벤트 이름 `PreToolUse`·`PermissionRequest`·`PostToolUse`·`PreCompact`·`PostCompact`·`SessionStart`·`SessionEnd`·`UserPromptSubmit`·`SubagentStart`·`SubagentStop`·`Stop`·`Interrupt`, 핸들러 `command`(`commandWindows`, `timeout`, `async`, `statusMessage`)
- `codex-rs/exec/tests/suite/hooks.rs`: `CODEX_HOME/hooks.json`의 SessionStart가 `--dangerously-bypass-hook-trust`로 실행되는 테스트(사용자 범위 위치 확인)
- `codex-rs/tui/src/startup_hooks_review.rs`: 시작 시 검토 화면(Review / Trust all and continue / Continue without trusting)과 `write_hook_trusts`

설치된 바이너리: `codex --version` → `codex-cli 0.163.0`, `codex features list`에서 `hooks  stable  true`.

## 실측 방법(자격 증명·사용자 설정 미사용)

- 매 실행 새 임시 `CODEX_HOME`(+`CODEX_SQLITE_HOME`)과 빈 작업 폴더. 사용자 실제 `~/.codex`는 읽거나 쓰지 않았고 실행 뒤 `hooks.json` 없음·`config.toml` 변경 없음을 확인했습니다.
- Pi에 Codex 로그인이 없어 실제 모델을 쓰지 않았습니다. Codex 자체 테스트(`core/tests/common/test_codex_exec.rs`)와 같은 방식으로 `CODEX_API_KEY=dummy`, `-c openai_base_url=http://127.0.0.1:<port>/v1`을 주고 로컬 합성 Responses API 서버([`tests/e2e/codex/mock_responses.py`](../../tests/e2e/codex/mock_responses.py))가 `exec_command`로 `true`를 한 번 부른 뒤 `done`으로 끝냅니다. 대화형 TUI의 로그인 화면은 임시 홈에 가짜 API 키(`codex login --with-api-key`)로 넘겼습니다.
- 프로브 Hook은 이벤트 이름, 최상위 키 이름, 안전한 enum(`tool_name`·`source`·`reason`·`permission_mode`)과 ID의 JSON 타입만 기록했습니다. 프롬프트·도구 입출력·경로 값은 남기지 않았습니다.
- `--dangerously-bypass-hook-trust`는 위치·형식 확인 1회에만 썼고(내용이 고정된 임시 프로브), 신뢰 흐름 검증과 종단 시험은 우회 없이 TUI 신뢰로 했습니다.

## 종단 시험(`tests/e2e/codex_flow.sh`)

```sh
tests/e2e/codex_flow.sh <서버 이미지> <웹 이미지> <Collector 실행 파일>
```

일회용 Compose(`ao-codex-e2e`, 컨테이너마다 `--cpus 1`) → 관리자·등록 코드 → Collector 임시 홈 `register`·`run` → 임시 `CODEX_HOME`에 `hooks install --tool codex`(미리보기 → `--yes`) → 신뢰 전 `codex exec` → TUI "Trust all" → 신뢰 후 `codex exec` → 서버 확인 → `hooks uninstall --tool codex --yes`.

결과(2026-10-26, 서버·웹 이미지 `ao-release-server:test`·`ao-release-web:test`):

| 항목 | 결과 |
|---|---|
| `hooks status --tool codex` | `installed` 9/9, 버전 `0.163.0` → `verified`(실측 모드 exec) |
| 신뢰 전 | `enqueued_hook` 0, 서버 세션 0 |
| TUI 신뢰 | `trusted_hash` 9개 |
| 신뢰 후 서버 | 세션 2개(TUI 신뢰 세션의 종료 1 + exec 세션), 제공자 `codex`, 이벤트 `session.started`·`turn.started`·`tool.started`·`unknown`(CLI PostToolUse, 버전 미기록이라 보수적)·`turn.completed`·`session.ended` |
| 제거 후 `hooks.json` | `{}` |
| 표식 문자열(DB 전체 덤프) | 0건 |
| 사용자 `~/.codex` | 변경 없음 |

## 남은 범위

- macOS·Windows Codex CLI, Codex Desktop(#87)의 사용자 범위 `hooks.json` 실기 확인
- 대화형 TUI 세션 자체의 이벤트(이 시험에서 TUI는 신뢰 화면만 사용)
- `PermissionRequest`·하위 에이전트 이벤트(승인 정책·다중 에이전트 시나리오)
- `hook codex`에 Codex 버전을 기록해 CLI `PostToolUse`를 `tool.completed`로 올릴지 결정(현재는 버전·모드를 몰라 `unknown`)
