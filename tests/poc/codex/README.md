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

## app-server 부착 범위 프로브 (#21)

`app_server_probe.py`는 임시 `CODEX_HOME`에서 app-server를 띄우고 두 클라이언트가 같은 thread를 볼 수 있는지 확인합니다. 로그인이 필요 없도록 모델 턴 대신 `thread/shellCommand`(`true`)와 `thread/name/set`만 보냅니다. 출력에는 메서드 이름, 응답 키, 오류 코드, 실행별 HMAC thread 지문만 남깁니다.

```bash
python3 tests/poc/codex/app_server_probe.py --confirm-synthetic-run
```

- 같은 서버: `codex app-server --listen unix://<임시 경로>`에 unix socket WebSocket 클라이언트 2개
- 다른 서버: `codex app-server` stdio 프로세스 2개

unix socket 경로 길이 제한(약 108바이트) 때문에 임시 디렉터리는 시스템 기본 위치에 만듭니다. 결과와 판단은 [보고서](../../../docs/poc/codex-app-server.md)를 따릅니다.
