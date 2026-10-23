# Claude Code Hook 합성 PoC 하네스

관련 이슈: #19

이 하네스는 임시 Git 작업공간과 합성 파일 `notes.txt`를 만든 뒤, `claude -p` 합성 세션을 한 번 실행합니다. Hook은 작업공간 밖의 임시 설정 파일을 `--settings`로만 주입하고, `--setting-sources project`로 사용자·로컬 설정을 불러오지 않습니다. 임시 작업공간에는 `.claude/` 설정이 없습니다. `~/.claude/settings.json`은 실행 전후 SHA-256이 같은지 확인만 하고 수정하지 않습니다.

Hook 프로브는 stdin 원문을 저장하지 않고 stdout에 아무것도 쓰지 않습니다(`SessionStart`·`UserPromptSubmit` stdout은 모델 컨텍스트에 추가되기 때문). 남기는 정보는 다음과 같습니다.

- 알려진 Hook 이벤트 이름과 안전한 enum 값
- 알려진 필드의 존재·JSON 타입, 식별자 형태인 미등록 필드의 이름
- 실행별 무작위 salt HMAC으로만 비교 가능한 session·prompt·tool_use·agent 식별자
- transcript·하위 에이전트 transcript의 경로 템플릿과 JSONL 구조 통계
- `--output-format stream-json` 출력의 type·subtype·Hook 이벤트·도구 이름 개수

프롬프트, 명령, 도구 입력·응답, 모델 출력, cwd, 원본 ID와 실제 경로는 남기지 않습니다. 임시 작업공간은 종료 시 삭제됩니다. Claude Code가 관리하는 합성 세션 transcript는 원본 도구 기록이므로 삭제하지 않습니다.

부모 Claude Code 세션 안에서 실행해도 섞이지 않도록 자식 환경에서 `CLAUDE*` 변수를 제거합니다(`CLAUDE_CONFIG_DIR` 제외).

```bash
python3 tests/poc/claude/run_probe.py --confirm-synthetic-run --scenario basic
python3 tests/poc/claude/run_probe.py --confirm-synthetic-run --scenario subagents
python3 tests/poc/claude/run_probe.py --confirm-synthetic-run --scenario denied --extra-events
python3 tests/poc/claude/run_probe.py --confirm-synthetic-run --scenario basic --hook-fault timeout
```

| 시나리오 | 내용 |
|---|---|
| `basic` | `Read` 1회, `Bash(git status:*)` 1회 |
| `subagents` | `--agents`로 정의한 읽기 전용 `poc-reader` 하위 에이전트 2개를 병렬 실행 |
| `denied` | 허용되지 않은 `Write` 1회, `--permission-prompts none`으로 자동 거부 |

`--hook-fault timeout`은 기록 후 2초 Hook 타임아웃을 넘기고, `--hook-fault error`는 기록 후 종료 코드 1을 냅니다. Claude Code에서 종료 코드 2는 차단 의미이므로 사용하지 않습니다.

실행마다 사용자의 Claude 사용량이 발생합니다(2026-10-23 실측 기준 1회 약 $0.012~$0.039). `--max-budget-usd 1`로 상한을 둡니다.
