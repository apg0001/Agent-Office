# Codex CLI Hook 합성 PoC 하네스

관련 이슈: #17

이 하네스는 임시 Git 작업공간을 만들고 CLI `-c`에 inline Hook 설정을 주입해, 사용자 전역 설정을 무시한 합성 `codex exec` 세션을 한 번 실행합니다. 실제 사용자 세션이나 기존 transcript는 열지 않습니다.

Hook 프로브는 stdin 원문을 저장하지 않습니다. 다음 정보만 stdout 요약으로 남깁니다.

- 알려진 Hook 이벤트 이름과 안전한 enum
- 허용된 필드의 존재와 JSON 타입
- 한 실행 안에서만 비교 가능한 HMAC 식별자
- transcript의 경로 템플릿·확장자와 JSONL 구조 통계
- Codex `--json` 출력의 구조 키와 이벤트 타입 개수

프롬프트, 명령, 도구 입력·응답, cwd, 원본 ID와 실제 transcript 경로는 출력하지 않습니다. 임시 작업공간은 실행 종료 시 삭제됩니다.

실행별 HMAC salt는 임시 Hook 명령 인자로만 전달합니다. 사용자 환경 전체 상속을 강제하지 않습니다.

```powershell
.\.venv\Scripts\python tests/poc/codex/run_probe.py --confirm-synthetic-run
```

`--dangerously-bypass-hook-trust`는 이 스크립트가 방금 생성하고 내용이 고정된 임시 Hook만 대상으로 사용합니다. 일반 사용자·프로젝트 Hook에는 이 옵션을 사용하지 않습니다.

## Codex Desktop 수동 합성 세션 (#18)

`desktop_session.py`는 Desktop UI를 자동화하지 않습니다. 운영자가 직접 Desktop에서
임시 작업공간을 열고 프로젝트·Hook을 신뢰한 뒤 합성 프롬프트를 보냅니다.

```powershell
# 임시 작업공간, 프로젝트 .codex/hooks.json, 전역 config.toml 백업 생성
.\.venv\Scripts\python tests/poc/codex/desktop_session.py prepare
# 운영자가 단계를 마칠 때마다 경계 표시
.\.venv\Scripts\python tests/poc/codex/desktop_session.py mark <workspace> <label>
# 이벤트 횟수·필드 이름·HMAC 별칭 상관관계만 출력
.\.venv\Scripts\python tests/poc/codex/desktop_session.py summarize <workspace>
# Desktop 완전 종료 후: 이 작업공간 키의 [projects.*]/[hooks.state.*]만 제거, 임시 파일 삭제
.\.venv\Scripts\python tests/poc/codex/desktop_session.py cleanup <workspace>
```

Hook 레코드는 작업공간 밖 `<workspace>-state`에 기록됩니다. 요약은 HMAC 값 대신
`S1`/`T1`/`A1` 별칭만 출력합니다. `cleanup`은 앱이나 사용자가 바꾼 다른 설정을
되돌리지 않고 개수만 보고합니다. 실측 결과는
[`docs/poc/codex-desktop.md`](../../../docs/poc/codex-desktop.md)에 있습니다.
