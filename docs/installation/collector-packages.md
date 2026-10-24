# Collector 설치 패키지

관련 이슈: #42. 대상은 headless Core(`agent-office-collector`)입니다. GUI(#40)는 OS별 Wails 빌드가 필요해 아직 패키지에 넣지 않습니다.

## 배포물

`collector/packaging/build.sh <버전>`(Docker만 필요, 호스트 Go 불필요)이 `dist/collector-<버전>/`에 만듭니다.

| OS | 아키텍처 | 형식 | 내용 |
|---|---|---|---|
| Linux | amd64·arm64 | `.deb`, `.rpm`, `.tar.gz` | `/usr/bin/agent-office-collector`, `/usr/bin/agent-office-collector-uninstall`, README |
| macOS | amd64·arm64 | `.tar.gz` | 바이너리, `agent-office-collector-uninstall`, README |
| Windows | amd64·arm64 | `.zip` | `agent-office-collector.exe`, `uninstall.ps1`, README |

- 정적 바이너리(`CGO_ENABLED=0`, `-trimpath`), 버전은 `-ldflags -X main.version`.
- `SHA256SUMS`에 모든 배포물의 해시. 받은 쪽은 `sha256sum -c SHA256SUMS`(Windows `Get-FileHash`)로 확인합니다.
- deb·rpm은 nfpm 2.41.1로 만듭니다. 버전의 `-beta.1`은 패키지 규칙상 `~beta.1`로 바뀝니다(정식 버전보다 낮게 정렬).

## 설치 뒤

패키지는 시스템 단위로 아무것도 켜지 않습니다. Hook·자동 시작은 사용자 계정 단위이고 사용자가 직접 승인해야 하기 때문입니다.

```sh
agent-office-collector register --server https://<서버> --code <웹 /devices에서 받은 코드> --name <PC 이름>
agent-office-collector hooks install --tool claude      # 미리보기 확인 후 --yes
agent-office-collector service install --activate
```

## 제거

| 범위 | 명령 | 지우는 것 |
|---|---|---|
| 기본 | `agent-office-collector-uninstall`(Windows `uninstall.ps1`) | 우리가 설치한 Hook과 자동 시작(소유 표시가 있는 것만, GUI 자동 시작 `.desktop`·LaunchAgent·HKCU Run 값 포함 — #43, Windows 미검증). 설정·장치 토큰·큐는 남김(재설치 시 그대로 이어감) |
| 완전 삭제 | `… --purge`(Windows `-Purge`) | 위 + OS 보안 저장소의 장치 토큰 항목(`logout --local-only`, 서버 폐기는 하지 않음, #41) + Core 설정·상태·큐 폴더(`~/.config/agent-office/collector` 등, Core 경로 규칙과 같음) |
| 패키지 | `dpkg -r` / `rpm -e` | 바이너리만. 다른 사용자 홈은 건드리지 않음 |

Claude·Codex의 원본 기록(`~/.claude/projects` 등)은 어떤 경우에도 지우지 않습니다. 장치를 서버에서도 끊으려면 웹 `/devices`에서 그 PC를 폐기합니다.

## 서명·공증(미구현·미검증)

| 대상 | 필요한 것 | 상태 |
|---|---|---|
| 해시 | `SHA256SUMS` | 구현 |
| 모든 배포물 서명 | GitHub OIDC keyless(cosign) 등 | 미구현(게시 워크플로에 `id-token: write`만 준비) |
| macOS | Developer ID 서명·notarization, `.pkg` | 미구현(Apple 계정·macOS 필요) |
| Windows | Authenticode 서명, MSI/MSIX | 미구현(인증서·Windows 필요) |

## 게시

`.github/workflows/publish-collector.yml`(수동 실행, `collector-publish` Environment 승인)이 같은 스크립트로 빌드해 GitHub Release **초안**으로 올립니다. 관리자가 확인 후 공개합니다. 아직 실행하지 않았습니다.

## 검증(Pi 5 arm64, 2026-10-24)

- 6개 대상 빌드와 deb·rpm 4개 생성, `SHA256SUMS` 확인 통과.
- Debian 12 컨테이너: deb 설치 → `version` → 제거 도우미 기본(설정 유지)·`--purge`(설정 삭제, Claude 기록 유지) → `dpkg -r`로 바이너리 제거.
- Fedora 41 컨테이너: rpm 설치 → `version` → `rpm -e`로 제거.
- Linux tar.gz 실행, Windows zip 구성(PE 실행 파일) 확인.
- 미검증: amd64 패키지 실행(교차 빌드만), macOS·Windows에서 실행·제거, 서명.
