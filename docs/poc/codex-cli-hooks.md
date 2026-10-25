# Codex CLI Hook·JSONL 실측

관련 이슈: #17

실측일: 2026-10-23 (KST)

## 결론

Windows의 `codex-cli 0.162.0-alpha.17.2`에서 합성 `codex exec` 세션을 네 번 유효 실행했습니다. inline Hook 설정에서는 `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `Stop`, `SessionEnd`를 매 실행 정확히 한 번 관측했습니다. `PostToolUse`는 네 번 모두 관측하지 못했으므로 이 버전에서 지원된다고 표시하지 않습니다.

Codex `--json` 출력과 로컬 transcript는 모두 JSONL이지만 서로 같은 계약이 아닙니다. `--json`은 한 실행당 7줄로 `thread.started`, `turn.started`, `item.completed`, `turn.completed`을 관측했고, transcript는 종료 시점에 21줄이며 최상위 키가 `timestamp`, `type`, `payload`인 내부 기록이었습니다. transcript 내부 형식은 Collector의 안정 API로 간주하지 않습니다.

## 공식 기준

- [OpenAI Hooks 문서](https://learn.chatgpt.com/docs/hooks)는 Hook 신뢰 검토, 탐색 위치, 공통 입력과 이벤트별 필드를 정의합니다.
- 같은 문서는 `PostToolUse`가 지원 도구의 성공·실패 출력 뒤에 실행되고, `SessionEnd`가 정상 종료 등에서 실행된다고 설명합니다.
- 비관리 Hook은 정확한 정의를 검토·신뢰해야 하며 여러 일치 Hook은 함께 실행될 수 있습니다.

실측 결과가 공식 설명과 다른 `PostToolUse` 누락은 현재 환경의 관측 사실로 남기며, 문서만 근거로 지원 처리하지 않습니다.

## 안전한 실측 방법

하네스는 [`tests/poc/codex`](../../tests/poc/codex/README.md)에 있습니다.

- 임시 Git 작업공간만 사용하고 종료 시 삭제
- 사용자 전역 config 무시, Hook feature 명시 활성화
- 검토된 inline Hook을 CLI override로만 주입
- workspace-write sandbox, 승인 자동 허용 없음
- 합성 프롬프트와 읽기 전용 상태 명령 한 번만 사용
- 기존 사용자 transcript를 열지 않음
- 새 합성 세션이 Hook에 제공한 자기 transcript만 최대 1MiB·2,000줄까지 구조 검사
- Hook stdin과 Codex stdout 원문을 저장하지 않음
- 실행별 HMAC salt는 임시 Hook 명령 인자로만 전달하고 전체 사용자 환경 상속을 강제하지 않음

첫 실행은 Windows 기본 CP949가 UTF-8 JSONL을 읽지 못해 요약 단계에서 실패했습니다. subprocess 디코딩을 UTF-8과 대체 문자 정책으로 고정한 뒤 해결했습니다. 임시 `.codex/hooks.json` 탐색 방식은 해당 비대화형·전역 config 무시 조합에서 이벤트 0건이어서, 공식 문서의 동등한 inline `[hooks]` 구조를 `-c`로 주입해 Hook 실행 자체를 분리 검증했습니다.

하네스 수정·탐색을 포함해 총 8개의 합성 Codex 세션이 로컬 Codex 상태에 생성됐습니다. 그중 최종 inline 방식 4회만 결과 표에 포함합니다. 이 transcript는 Codex가 관리하는 원본 기록이므로 하네스가 삭제하지 않았으며 실제 사용자 내용은 포함하지 않습니다. 유효 실행은 모두 exit code 0이었지만 stderr 3줄이 반복됐고 안전 분류상 warning/error 토큰을 포함했습니다. 원문 재노출을 피하기 위해 stderr 본문은 보존하지 않았으므로 무경고 실행으로 주장하지 않습니다.

## 반복 관측 결과

| 항목 | 4회 결과 | 판정 |
|---|---:|---|
| `SessionStart` | 매회 1 | 관측됨 |
| `UserPromptSubmit` | 매회 1 | 관측됨; `prompt` 원문 필드가 있으므로 폐기 필수 |
| `PreToolUse` | 매회 1 | `Bash`, `tool_input`, `tool_use_id` 관측 |
| `PostToolUse` | 매회 0 | 미지원/미관측 처리 |
| `Stop` | 매회 1 | 턴 ID와 함께 관측 |
| `SessionEnd` | 매회 1 | 정상 `codex exec` 종료 직후, `reason: other` |
| Hook 중복 | 매회 0 | 한 설정·한 합성 도구 호출 기준. 다중 소스 중복은 #20에서 별도 검증 |
| 세션 상관관계 | 매회 HMAC 1종 | 같은 실행의 이벤트가 같은 세션 ID를 공유 |

`SessionStart` source는 `startup`이었습니다. `UserPromptSubmit`, `PreToolUse`, `Stop`은 동일한 turn ID를 공유했고 `SessionStart`·`SessionEnd`에는 turn ID가 없었습니다. 원본 ID는 저장하지 않고 실행별 무작위 salt HMAC으로만 비교했습니다.

## 로컬 transcript 구조

합성 세션 Hook이 받은 경로는 사용자 Codex 상태 아래 다음 형태였습니다.

```text
sessions/<number>/<number>/<number>/<redacted>.jsonl
```

확장자는 `.jsonl`, 최상위 키는 `timestamp`, `type`, `payload`였습니다. 종료 시점의 21개 유효 JSON 줄에서 구조 타입으로 `session_meta`, `turn_context`, `response_item`, `event_msg`와 그 밖의 값이 관측됐습니다. 원문 값, 실제 경로, 모델 출력, 프롬프트, 명령은 기록하지 않았습니다.

파일은 실행 중 줄 수가 8→14→20→21로 증가했습니다. 따라서 tail 구현은 partial write와 동시 append를 전제로 해야 하며, 내부 타입과 payload는 Codex 버전별 비안정 입력으로 취급해야 합니다.

## 정규화 결정

| 관측 입력 | 이벤트 v1 후보 | 신뢰도/제약 |
|---|---|---|
| `SessionStart` | `session.started` | `observed`; source `hook` |
| `UserPromptSubmit` | `turn.started` | `observed`; prompt 값은 즉시 폐기 |
| `PreToolUse` + 안전한 도구 분류 | `tool.started` | `observed`; 전체 입력·명령은 폐기 |
| `Stop` | `turn.completed` 후보 | 턴 정지 근거일 뿐 세션 종료가 아님 |
| `SessionEnd` | `session.ended` | `observed`; main thread 정상 종료에서만 |
| transcript/`--json` 보조 관측 | 위 이벤트 보강 | Hook과 중복 가능, #20 멱등·중복 검증 전 사용 금지 |
| 미관측 `PostToolUse` | `tool.completed` | 이 버전에서는 Hook 단독 생성 금지, `unknown`/보조 소스 필요 |

`Stop`을 세션 종료로 매핑하지 않습니다. `SessionEnd`만 실제 종료 근거로 사용하며, CLI 창 전환이나 단순 무응답을 종료로 추정하지 않습니다.

## 미실행·후속 범위

- `PermissionRequest`, `Interrupt`, compact, resume/fork, subagent Hook은 이번 합성 시나리오에서 미실행
- 사용자 승인 대기와 비정상 도구 종료는 미실행
- Linux/macOS와 다른 Codex 버전은 미실행
- 프로젝트 `hooks.json`의 대화형 `/hooks` 신뢰 UI는 미실행
- Hook·`--json`·transcript 사이 중복과 재시작/로테이션은 #20에서 검증
- Desktop과 하위 에이전트는 #18, app-server는 #21에서 검증
