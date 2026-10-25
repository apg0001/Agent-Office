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

수동 게시 워크플로(`workflow_dispatch`)는 기본 브랜치(`main`)에 있어야 실행할 수 있으므로 `main` 병합을 먼저 합니다(0.1.0-beta.1에서 확인).

1. `develop` → `main` PR(`CI Gate` 필수)을 만들고 병합.
2. `gh workflow run publish-images.yml --ref main -f version=<버전>` → GitHub에서 `image-publish` Environment 승인(필수 리뷰어) → GHCR에 amd64·arm64 이미지 게시. 실행 결과의 digest를 기록.
3. 게시된 `tag@digest`로 `tests/install/compare.sh`를 한 번 더 실행.
4. `collector-publish` Environment에 변수 `COLLECTOR_UPDATE_PUBLIC_KEYS`, 비밀 `COLLECTOR_UPDATE_SIGNING_KEY`가 있는지 확인 → `gh workflow run publish-collector.yml --ref main -f version=<버전>` → 승인 → Release 초안 `collector-v<버전>` 확인 후 공개.
5. 운영 서버가 있으면 `deploy/upgrade.sh --server …@sha256:… --web …@sha256:…`.
6. `main` 병합 커밋에 `server-v<버전>` 태그, GitHub Release에 `docs/releases/<버전>.md` 내용을 붙임.

Environment 승인과 GitHub Secret 등록은 관리자가 직접 합니다(에이전트가 대신 승인하거나 비밀을 쓰지 않음).

## 하지 않는 것

- 승인 없는 `main` 병합·태그·게시, 검증하지 않은 OS·환경을 "지원"으로 표기, 실패한 검증을 지우고 내는 릴리스.
