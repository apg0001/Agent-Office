# 기여 가이드

## 기본 흐름

1. 기존 Issue를 선택하거나 한국어로 새 Issue를 작성한다.
2. `develop`에서 실제 Issue 번호를 포함한 브랜치를 만든다.
3. 작은 커밋으로 구현·테스트·문서를 함께 변경한다.
4. 커밋과 PR은 무엇을, 왜, 어떻게 바꿨고 무엇을 검증했는지 한국어로 기록한다.
5. `develop` 대상 PR을 만들고 CI·리뷰가 끝난 후 병합한다.

커밋 예: `[feat] 개발용 Compose 기반 구성 (#13)`

## 브랜치와 보호 규칙

- 기능: `feat/#번호-영문-설명`
- 버그: `fix/#번호-영문-설명`
- 문서: `docs/#번호-영문-설명`
- 테스트/하네스: `test/#번호-영문-설명` 또는 `chore/#번호-영문-설명`

`main`과 `develop`에는 직접 push하지 않습니다. force-push와 브랜치 삭제도 금지됩니다. 모든 변경은 연결된 Issue와 PR을 거치며, 리뷰 대화를 해결한 뒤 병합합니다. 현재 단일 관리자 단계에서는 보호 규칙의 필수 승인 수가 0이고 상태 검사는 #16에서 추가됩니다. 그렇더라도 PR 본문의 자체 검토·검증 근거를 생략할 수 없습니다.

`main` PR은 Beta·보안·백업·호환성 검증과 관리자 명시적 릴리스 승인이 있을 때만 만듭니다.

## 제출 전 확인

- `python -m pytest apps/server/tests tests`
- `python -m ruff check apps/server`
- `python -m mypy apps/server/src`
- `npm run typecheck && npm run test && npm run build`
- `go test ./collector/...`
- `docker compose --env-file deploy/.env.example -f deploy/compose.yaml config`
- `.\.venv\Scripts\python -m pytest tests/test_repository_metadata.py`

환경상 실행하지 못한 검사는 PR과 `docs/project-status.md`에 이유를 적습니다.

## 개인정보와 fixture

실제 세션 로그, 이메일, 토큰, 프롬프트, 코드, 명령 또는 사용자 경로를 커밋하지 마세요. 테스트 데이터는 합성하고 식별 불가능해야 합니다.

## 라이선스

현재 라이선스는 미정입니다. 저장소 소유자의 결정 전에는 외부 코드의 라이선스 호환성이나 이 저장소의 재배포 권리를 추정하지 마세요.
