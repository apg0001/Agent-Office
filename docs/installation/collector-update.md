# Collector 자체 업데이트

관련 이슈: #44. 대상은 headless Core(`agent-office-collector`)입니다.

## 사용

```sh
agent-office-collector update check [--channel stable|beta]          # 새 버전 알림만
agent-office-collector update apply [--channel stable|beta] --yes    # 받아서 적용
```

- 기본 채널은 `stable`. 업데이트는 **알린 뒤 사용자가 적용**합니다(자동 적용은 하지 않음. GUI가 알림을 보여 주고 같은 명령을 부릅니다).
- manifest 위치: `https://github.com/apg0001/Agent-Office/releases/download/collector-<채널>/manifest.json`(+ `.sig`). `--manifest-url`로 바꿀 수 있고 https만 받습니다.
- 패키지(deb·rpm, `/usr/bin`)로 설치했다면 `managed_by_package`로 거부하고 패키지 관리자로 업데이트하라고 안내합니다.

## 검증 순서

| 단계 | 내용 | 실패 코드 |
|---|---|---|
| 서명 | manifest 바이트에 대한 ed25519 서명을 빌드에 넣은 공개 키로 확인(키 여러 개 허용 = 키 교체) | `no_trusted_key`, `bad_signature` |
| 형식·채널 | schema 1, 요청한 채널과 같은지 | `bad_manifest`, `channel_mismatch` |
| 버전 | SemVer 2.0 우선순위로 현재보다 높을 때만 | 같거나 낮으면 "최신 버전" |
| 대상 | 현재 OS·아키텍처 배포물이 있는지, URL https | `no_artifact_for_platform`, `insecure_url` |
| 다운로드 | 크기 상한(200MB)·manifest 크기 일치·SHA-256 | `checksum_mismatch`, `too_large` |
| 압축 해제 | Collector 실행 파일 하나만, 경로 이동·링크 거부 | `binary_not_found`, `bad_archive` |
| 실행 확인 | 새 실행 파일의 `version`이 manifest 버전과 같은지 | `new_binary_invalid` |
| 교체 | 같은 디렉터리에서 원자적 이름 바꾸기, 이전 파일은 `.bak` | `not_writable` |
| 재기동 | 자동 시작 서비스가 있으면 재시작(systemd `restart`, launchd `kickstart -k`, 작업 스케줄러 End/Run), 30초 안에 새 버전이 `status.json`을 쓰는지 | 실패 시 이전 파일로 되돌리고 다시 시작 → `rolled_back` |

큐는 SQLite에 남아 있으므로 서비스가 SIGTERM으로 정상 종료한 뒤 새 버전이 체크포인트에서 이어 갑니다(#33·#34). 오류는 코드만 출력하고 URL·경로는 출력하지 않습니다.

## 키와 서명(관리자)

```sh
cd collector/core
go run ./cmd/agent-office-release keygen update.key          # 공개 키 출력, 비밀 키 0600
go run ./cmd/agent-office-release manifest ../../dist/collector-<버전> <버전> <채널> <배포물 기준 URL>
go run ./cmd/agent-office-release sign update.key ../../dist/collector-<버전>/manifest.json
```

- 공개 키는 빌드 때 넣습니다: `AGENT_OFFICE_UPDATE_KEYS=<공개 키> collector/packaging/build.sh <버전>`(내부적으로 `-ldflags -X …/update.TrustedKeys`). 키가 없는 빌드는 업데이트를 거부합니다.
- 게시 워크플로(`publish-collector.yml`)는 `collector-publish` Environment의 변수 `COLLECTOR_UPDATE_PUBLIC_KEYS`와 비밀 `COLLECTOR_UPDATE_SIGNING_KEY`로 빌드·서명해 Release 초안에 `manifest.json`·`.sig`를 함께 올립니다. 비밀 키는 저장소에 두지 않습니다.
- 초안을 공개한 뒤 채널 manifest를 갱신합니다: `gh release upload collector-<채널> dist/manifest.json dist/manifest.json.sig --clobber`(채널용 Release `collector-stable`·`collector-beta`가 없으면 먼저 만듭니다).

## 검증

- 단위·통합 테스트(Linux arm64 컨테이너, `go test -race`): SemVer 우선순위, 서명(다른 키·변조·키 없음), 채널·대상·http 거부, SHA-256 불일치, tar.gz·zip 추출·경로 이동 거부, 교체·`.bak`, 재기동 후 비정상 시 되돌림, 패키지 설치 거부, CLI `check`·`apply`(`--yes` 없으면 적용 안 함)·키 없는 빌드 거부.
- 릴리스 도구 `keygen`·`manifest`·`sign` 실행과 서명 검증.
- 미검증: 실제 GitHub Release에서 받기, macOS·Windows에서 교체·서비스 재시작, GUI 알림 연결.
