# Codex Desktop Hook 실측

관련 이슈: #18 (선행: #17 [Codex CLI Hook·JSONL 실측](codex-cli-hooks.md))

실측일: 2026-10-23 (KST)

## 결론

Windows용 Codex Desktop에서 소유자가 직접 조작한 합성 세션 1회를 실측했습니다. 프로젝트 `.codex/hooks.json`을 Desktop 설정 화면에서 신뢰한 뒤 Hook이 실행됐습니다. 관측된 이벤트는 `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PermissionRequest`, `SubagentStart`, `SubagentStop`, `Stop`, `SessionEnd` 8종입니다.

- #17의 CLI `exec`에서 미관측이던 `PostToolUse`가 Desktop에서는 4회 관측됐고, `tool_use_id` HMAC으로 `PreToolUse`와 4쌍 모두 짝지어졌습니다.
- `SessionStart`는 미관측입니다. 세션이 Hook 신뢰 전에 시작됐을 가능성을 배제하지 못했으므로 미지원으로 단정하지 않고 재실측 대상으로 남깁니다.
- 1회 실측이므로 반복성은 검증하지 않았습니다. 이 결과만으로 Desktop 지원을 선언하지 않고 `부분 관측`으로 표시합니다.

## 관측 환경

| 항목 | 값 |
|---|---|
| OS | Windows 11 Education 10.0.26200, x64 |
| Desktop 패키지 | Microsoft Store(MSIX) `OpenAI.Codex` `26.1007.2314.0`, x64. 시작 메뉴 표시 이름 `ChatGPT` |
| 설치 경로 형태 | `WindowsApps/OpenAI.Codex_<version>_x64__<publisher>/app/…`, 번들 CLI `%LOCALAPPDATA%/OpenAI/Codex/bin/<hash>/codex.exe` |
| 번들 CLI 버전 | `codex-cli 0.162.0-alpha.17.2` (#17과 동일 문자열) |
| 기능 플래그 | `hooks` stable/true, `multi_agent` stable/true |
| 권한 모드 | 모든 이벤트 `permission_mode: default` |

버전·경로는 패키지 메타데이터와 프로세스 경로로만 확인했습니다. 사용자 Codex 로그·SQLite·세션 원문은 열지 않았습니다.

## 실측 방법

하네스: [`tests/poc/codex/desktop_session.py`](../../tests/poc/codex/README.md)

1. `prepare`: `%TEMP%` 아래 임시 Git 작업공간과 합성 파일 하나를 만듭니다. 12개 Hook 이벤트에 같은 `hook_probe.py` 핸들러(timeout 3초, matcher 없음)를 연결한 프로젝트 `.codex/hooks.json`을 원자적으로 씁니다. 실행 전 전역 `config.toml`의 해시와 백업도 남깁니다.
2. 소유자가 Desktop UI에서 프로젝트를 열고, 신뢰하고, Hook을 검토·신뢰하고, 고정 영문 합성 프롬프트를 보냈습니다. 에이전트는 UI를 자동화하지 않았습니다(Computer Use 미사용).
3. 단계가 끝날 때마다 `mark`로 그 시점의 레코드 수를 기록해 이벤트를 단계별로 나눴습니다.
4. `summarize`는 이벤트 이름, 필드 이름·타입, 허용 enum, 실행별 salt HMAC의 일치 여부만 출력합니다. HMAC 값 대신 `S1`, `T1`, `A1` 같은 별칭을 씁니다.
5. Desktop을 완전히 종료한 뒤 `cleanup`이 전역 `config.toml`에서 이 작업공간 경로를 키로 하는 섹션만 제거하고, 작업공간과 state를 삭제했습니다.

프롬프트, 응답, 명령, `tool_input`·`tool_response` 값, `last_assistant_message`, cwd, 원본 ID, 실제 경로는 저장하지 않았습니다.

## 단계별 관측

| 단계 | 소유자 조작 | 관측 이벤트(순서) | 판정 |
|---|---|---|---|
| 세션 시작 | 프로젝트 열기·신뢰, 새 스레드 | 없음 | `SessionStart` 미관측 |
| 프롬프트 턴(Hook 신뢰 전) | 합성 프롬프트 전송 | 없음 | 미신뢰 Hook은 건너뜀. 문서와 일치 |
| 프롬프트 턴(Hook 신뢰 후) | Desktop 설정에서 Hook 신뢰 후 재전송 | `UserPromptSubmit S1 T1` → `Stop S1 T1` | 관측 |
| 안전한 읽기 도구 | `git status --short` 1회 요청, 승인 창 없음 | `UserPromptSubmit` → `PreToolUse(Bash)` → `PostToolUse(Bash)` → `Stop` (S1 T2) | 관측 |
| 승인 대기 | 네트워크 명령 요청, 승인 창에서 거부 | `UserPromptSubmit` → `PreToolUse(Bash)` → `PermissionRequest(Bash)` → `Stop` (S1 T3) | 관측. 거부 후 `PostToolUse` 없음 |
| 하위 에이전트 | 하위 에이전트 1개 생성·대기·닫기 요청 | `UserPromptSubmit` → Pre/Post(other) → `SubagentStart S1 T5 A1` → Pre(other) → `SubagentStop S1 T5 A1` → Post(other) → Pre/Post(other) → `Stop` (부모 T4) | 관측 |
| 세션 종료 | 스레드 보관 + 앱 완전 종료 | `SessionEnd S1` (`reason: other`) | 관측. 보관과 종료 중 어느 쪽이 원인인지는 미구분 |

미관측: `SessionStart`, `PreCompact`, `PostCompact`, `Interrupt`. compact와 interrupt는 시나리오에 없었으므로 미실행입니다.

## 이벤트 횟수와 필드

| 이벤트 | 횟수 | 존재 필드(값 미보존) |
|---|---:|---|
| `UserPromptSubmit` | 4 | `cwd`, `hook_event_name`, `model`, `permission_mode`, `prompt`, `session_id`, `transcript_path`, `turn_id` |
| `PreToolUse` | 5 | 공통 + `tool_input`, `tool_name`, `tool_use_id`, `turn_id` |
| `PostToolUse` | 4 | `PreToolUse` 필드 + `tool_response` |
| `PermissionRequest` | 1 | 공통 + `tool_input`(키 `command`, `description`), `tool_name`, `turn_id`. `tool_use_id` 없음 |
| `SubagentStart` | 1 | 공통 + `agent_id`, `agent_type`, `turn_id` |
| `SubagentStop` | 1 | 공통 + `agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message`, `stop_hook_active`, `turn_id` |
| `Stop` | 4 | 공통 + `last_assistant_message`, `stop_hook_active`, `turn_id` |
| `SessionEnd` | 1 | `cwd`, `hook_event_name`, `reason`, `session_id`, `transcript_path` (`model`·`permission_mode` 없음) |

공통 필드: `cwd`, `hook_event_name`, `model`, `permission_mode`, `session_id`, `transcript_path`. 21개 레코드가 모두 `captured`였고 파싱·크기 실패는 0건입니다.

## HMAC 상관관계

| 비교 | 결과 |
|---|---|
| 고유 세션 | 1 (`S1`). 하위 에이전트 이벤트와 `SessionEnd`도 같은 세션 ID |
| 고유 턴 | 5 (부모 4 + 하위 에이전트 1) |
| `PreToolUse`↔`PostToolUse` `tool_use_id` 일치 | 4/4 완료 도구. 거부된 1건은 Pre만 있음 |
| `PermissionRequest`↔`PreToolUse` | 같은 턴 `T3` 공유. `tool_use_id`가 없어 턴 단위 연결만 가능 |
| `SubagentStart`↔`SubagentStop` `agent_id` 일치 | 1/1 |
| 하위 에이전트 턴 | 부모 턴 `T4`와 다른 `T5` |
| `agent_id` = 어떤 `session_id` | 0. 하위 에이전트 ID는 세션 ID와 별개 |
| 하위 에이전트 내부 `SessionStart`/`UserPromptSubmit`/`Stop` | 0 |

`transcript_path`와 `agent_transcript_path`의 형태는 모두 `sessions/<number>/<number>/<number>/<redacted>.jsonl`로, #17 CLI와 같습니다. 두 경로가 같은 파일인지는 비교하지 않았습니다.

## 도구 이름

- `Bash`: `PreToolUse` 2, `PostToolUse` 1, `PermissionRequest` 1. `tool_input` 키는 `command`(승인 요청은 `description` 추가)였습니다.
- `other`: 하위 에이전트 단계의 Pre/Post 3쌍. 허용 목록(`Bash`, `apply_patch`, `spawn_agent`, `update_plan`, `mcp__*`)에 없는 이름이라 실제 이름을 보존하지 않았습니다. 하위 에이전트 생성·대기·닫기 도구라는 해석은 `inferred`이며, 이름 확정은 후속 실측에서 허용 목록 후보를 추가해 확인해야 합니다.

## 정규화 결정(제안)

| 관측 입력 | 이벤트 v1 후보 | 신뢰도/제약 |
|---|---|---|
| `UserPromptSubmit` | `turn.started` | `observed`; `prompt` 즉시 폐기 |
| `PreToolUse` | `tool.started` | `observed`; `tool_input` 폐기, 안전 분류만 |
| `PostToolUse` | `tool.completed` | Desktop에서 `observed`; `tool_use_id`로 시작과 연결. CLI `exec`에서는 여전히 미관측 |
| `PermissionRequest` | 승인 대기 상태 | `observed`. 승인·거부 결정 Hook은 없음 → 대기 종료는 `inferred`(다음 `PostToolUse` 또는 `Stop`) |
| `SubagentStart`/`SubagentStop` | 하위 에이전트 시작/종료 | `observed`; `agent_id`로 1:1 연결, 부모는 같은 `session_id` |
| `Stop` | `turn.completed` 후보 | 세션 종료로 매핑하지 않음 |
| `SessionEnd` | `session.ended` | `observed` 1회. 보관/앱 종료 원인 미구분 |
| `SessionStart` | `session.started` | Desktop 미관측. 지원 표시 금지 |

## 설정 변경과 원복

- 프로젝트 신뢰로 전역 `config.toml`에 `[projects.'<임시 작업공간>']`(`trust_level`) 1개가 추가됐습니다.
- Desktop 설정 화면에서 Hook을 신뢰하자 `[hooks.state.'<임시 작업공간>/.codex/hooks.json:<event>:0:0']`(`trusted_hash`) 12개가 추가됐습니다. Desktop의 Hook 신뢰는 CLI와 같은 전역 `config.toml`에 저장됩니다.
- 실측 중 앱이 자체 UI 설정값 1개를 바꿨습니다. 이 값은 PoC 소유가 아니므로 되돌리지 않았습니다.
- `cleanup`은 작업공간 경로를 포함한 섹션 13개만 제거했습니다. 그 결과 섹션 구성이 백업과 같아졌고, 소유 섹션 잔존은 0개였습니다.
- 첫 `cleanup`에서는 Windows의 읽기 전용 `.git` 객체 때문에 작업공간 삭제가 조용히 실패했습니다. 쓰기 권한 복구 후 삭제하도록 고치고 회귀 테스트를 추가한 뒤 잔존 디렉터리를 지웠습니다.
- Codex가 관리하는 합성 세션 transcript는 사용자 Codex 상태에 남아 있습니다. 원본 도구 기록을 삭제하지 않는 원칙에 따라 그대로 두었으며, 실제 사용자 내용은 없습니다.

## 미실행·후속 범위

- 반복 실측(2회 이상)과 `SessionStart` 재확인: Hook을 신뢰한 뒤 Desktop을 재시작하고 새 스레드를 시작
- 승인 **허용** 경로, `Interrupt`, compact, resume/fork 미실행
- 스레드 보관과 앱 종료를 분리한 `SessionEnd` 원인 확인 미실행
- 하위 에이전트 관리 도구 이름 확정 미실행
- macOS·Linux Desktop과 다른 Desktop 버전 미실행
- Hook과 transcript 간 중복·멱등은 #20, app-server 비-UI 관측은 #21
