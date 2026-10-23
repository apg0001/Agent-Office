# Claude Code 실행 안내

Claude Code는 먼저 [AGENTS.md](AGENTS.md)와 [docs/project-status.md](docs/project-status.md)를 읽는다. 두 파일의 공통 규칙을 복제하지 말고 그대로 따른다.

- 현재 세부 Issue 하나만 맡고 실제 Issue 번호로 브랜치를 만든다.
- Claude Hook/JSONL 호환성은 설치 버전과 OS에서 실제 검증한 경우에만 지원으로 표시한다.
- 기존 Claude 설정과 Hook을 덮어쓰지 않는다. 변경 전 백업, 소유 항목 표시, 원자적 쓰기, 제거 시 소유 항목만 복원한다.
- 대화·프롬프트·파일 본문·도구 전체 출력이 fixture나 서버 이벤트에 들어가지 않게 한다.
- 작업 종료 전 `docs/project-status.md`의 “다음 작업”을 명령 수준으로 갱신한다.
