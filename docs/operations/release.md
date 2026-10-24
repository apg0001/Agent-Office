# 릴리스 절차

관련 이슈: #65. `main` 병합·태그·이미지 게시·Stable 표시는 **관리자 명시적 승인 후에만** 합니다. 에이전트는 아래 준비 단계까지만 수행합니다.

## 버전

- 서버·웹 이미지: `server-vX.Y.Z`(이미지 태그 `X.Y.Z`), Collector: `collector-vX.Y.Z`, 프로토콜 `protocol-v1`.
- Beta는 `-beta.N`. Stable 승격은 같은 커밋을 다시 태그하지 않고 Beta 검증 결과를 근거로 새 버전으로 냅니다.

## 준비(에이전트·관리자 누구나)

1. `develop`의 CI(`CI Gate`, 브라우저 E2E 포함) 통과 확인.
2. Pi에서 실측: `tests/install/compare.sh`, `tests/e2e/run.sh`, `tests/load/run.sh`(30세션), 백업 `restore-test`.
3. `docs/releases/<버전>.md` 초안에 실제 결과·미검증·위험·호환성을 적고 `CHANGELOG.md`를 갱신.
4. 초안 PR을 `develop`에 병합.

## 승인 후(관리자)

1. GitHub Actions `Publish Images`를 `version=<버전>`으로 실행 → `image-publish` Environment 승인 → GHCR에 amd64·arm64 이미지 게시. 실행 결과의 digest를 기록.
2. 게시된 `tag@digest`로 `tests/install/compare.sh`를 한 번 더 실행.
3. 운영 서버에 `deploy/upgrade.sh --server …@sha256:… --web …@sha256:…`.
4. `develop` → `main` PR을 만들고 관리자가 승인·병합.
5. `main` 병합 커밋에 `server-v<버전>` 태그, GitHub Release에 `docs/releases/<버전>.md` 내용을 붙임.

## 하지 않는 것

- 승인 없는 `main` 병합·태그·게시, 검증하지 않은 OS·환경을 "지원"으로 표기, 실패한 검증을 지우고 내는 릴리스.
