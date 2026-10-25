# Collector 데스크톱 검증 절차(#41, #43)

운영 코드가 아니며 CI에서 실행하지 않습니다. 화면·세션 D-Bus·키링이 필요한 확인을 Docker 컨테이너에서 재현하는 스크립트입니다. 결과와 패키지 버전은 [Collector GUI](../../../../docs/installation/collector-gui.md#트레이-검증linux-컨테이너)에 있습니다. 토큰은 모두 합성 sentinel이고 대화·파일 내용은 쓰지 않습니다.

| 파일 | 내용 |
|---|---|
| `keyring-secret-service.sh` | gnome-keyring(Secret Service)을 `dbus-run-session`에서 잠금 해제하고 `AGENT_OFFICE_TEST_REAL_KEYRING=1`로 Core 실제 저장소 테스트 실행 |
| `autostart-validate.sh` | GUI 자동 시작 `.desktop`을 `desktop-file-validate`, plist를 `xmllint`로 검사 |
| `tray-e2e.sh` | Xvfb·최소 StatusNotifierWatcher·합성 서버로 트레이 메뉴(일시중지·재개·창 열기·GUI 종료), 숨김 시작, 단일 실행, Core 재시작 회복, GUI 종료 뒤 수집 계속 확인 |
| `tray-close.sh` | openbox·`wmctrl -c`로 창 닫기: Watcher 없으면 종료, 있으면 트레이로 숨김 |
| `sni_watcher.py`, `sni_client.py` | `org.kde.StatusNotifierWatcher` 최소 구현과 dbusmenu 클라이언트(메뉴 목록·클릭·툴팁) |
| `fake_office.py` | 등록·heartbeat·배치·폐기만 있는 합성 서버(127.0.0.1) |

저장소 루트에서(Raspberry Pi 5 arm64에서 확인):

```bash
docker run --rm --tmpfs /root/.cache:exec,size=2g --tmpfs /go/pkg:exec,size=2g --tmpfs /w:exec,size=1g \
  -v "$PWD:/src:ro" golang:1.24-bookworm sh /src/tests/poc/collector/desktop/keyring-secret-service.sh
docker run --rm --tmpfs /root/.cache:exec,size=2g --tmpfs /go/pkg:exec,size=2g --tmpfs /w:exec,size=1g \
  -v "$PWD:/src:ro" golang:1.24-bookworm sh /src/tests/poc/collector/desktop/autostart-validate.sh
# dist/에 Core·GUI(-tags desktop,production)를 먼저 빌드
docker run --rm --tmpfs /w:exec,size=1g -v "$PWD/tests/poc/collector/desktop:/scripts:ro" -v "$PWD/dist:/bin-in:ro" \
  golang:1.24-bookworm sh /scripts/tray-e2e.sh
docker run --rm --tmpfs /w:exec,size=1g -v "$PWD/tests/poc/collector/desktop:/scripts:ro" -v "$PWD/dist:/bin-in:ro" \
  golang:1.24-bookworm sh /scripts/tray-close.sh
```
