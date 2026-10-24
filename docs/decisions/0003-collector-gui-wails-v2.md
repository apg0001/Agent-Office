# ADR-0003: Collector GUI는 Wails v2.12.0으로 Core를 하위 프로세스로 감싼다

- 상태: 제안됨(#40 PoC 근거, 소유자 검토 대기)
- 날짜: 2026-10-24
- 관련 이슈: #40, #41, #42~#44

## 맥락

마스터 사양 10장은 GUI Collector를 Wails 우선으로 PoC하고, Core 수명을 GUI 창과 분리하며, Wails v3 같은 베타 프레임워크 기능은 PoC로 확정하라고 합니다. 저장소의 Go 기준은 1.24(`collector/core`)입니다.

#40 PoC에서 확인한 사실(Linux arm64, Docker `golang:1.24-bookworm`, 2026-10-24):

- Go 모듈 프록시의 Wails v2 최신은 v2.17.0이지만 v2.13.0부터 `go 1.25.0`을 요구합니다. Go 1.24에서 쓸 수 있는 최신은 **v2.12.0**(`go 1.22.0`)입니다.
- v2.12.0은 Debian bookworm의 `libgtk-3-dev 3.24.38`·`libwebkit2gtk-4.0-dev 2.50.6`으로 `go build -tags desktop,production`이 되고, Xvfb에서 창이 뜨며 Go 바인딩 호출(설정 읽기, Core `status`·`hooks status`)이 동작했습니다.
- 빌드 태그 없이 컴파일하면 Wails가 데스크톱 프런트엔드(cgo·webkit)를 끌어오지 않아, 화면·webkit 없이 `go vet`·`go test`가 됩니다.
- Wails v3는 이 PoC에서 평가하지 않았습니다(알파·베타 단계, 트레이·자동 시작 API가 바뀌는 중).

## 결정

1. GUI는 별도 Go 모듈 `collector/gui`, Wails **v2.12.0**(정확한 버전 고정)으로 만듭니다. Go 1.25로 올리는 결정이 나기 전에는 v2.13.0 이상으로 올리지 않습니다.
2. GUI는 수집 루프를 품지 않습니다. 등록·상태·일시중지·진단·Hook·자동 시작을 모두 Core 실행 파일(`agent-office-collector`) 명령과 JSON 출력으로 처리합니다. 창을 닫거나 GUI가 죽어도 자동 시작된 Core는 계속 수집합니다.
3. GUI는 장치 토큰을 읽거나 저장하거나 표시하지 않습니다. 등록 코드는 명령 인자가 아니라 표준 입력(`register --code-stdin`)으로만 Core에 넘깁니다.
4. 프런트엔드는 빌드 도구·npm 의존성 없는 정적 HTML·JS·CSS로 둡니다(루트 npm 작업 공간·잠금 파일을 건드리지 않음). 화면이 커지면 별도 Issue에서 React+Vite로 옮깁니다.
5. 트레이·OS 알림·업데이트(#42~#44)는 v2 기능으로 가능한지 각 Issue에서 다시 PoC하고, 불가하면 이 ADR을 개정합니다. 트레이(#43)와 장치 토큰 보안 저장소(#41)는 [ADR-0004](0004-collector-keychain-tray.md)로 정했습니다(트레이는 Linux만).

## 결과

- Core CLI 출력 형식이 GUI 계약이 됩니다. `status --json`·`diagnose --json`·`hooks status --json`을 바꾸면 `collector/gui/internal/app/integration_test.go`(실제 Core로 실행, CI `Go GUI` 작업)가 깨집니다.
- 리눅스 빌드에는 webkit2gtk 4.0 개발 패키지가 필요합니다. Ubuntu 24.04처럼 4.1만 있는 배포판은 `-tags webkit2_41`이 필요할 수 있으나 검증하지 않았습니다.
- macOS·Windows 빌드와 실행은 검증하지 않았습니다(`docs/installation/collector-gui.md`).
