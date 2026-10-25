Agent Office Collector Core

1. 웹의 /devices에서 PC 등록 코드를 받습니다.
2. agent-office-collector register --server https://<서버> --code <코드> --name <PC 이름>
3. agent-office-collector hooks install --tool claude   (미리보기 확인 후 --yes)
4. agent-office-collector service install --activate    (로그인 시 자동 시작)

정리: agent-office-collector-uninstall (Windows: uninstall.ps1). --purge는 설정·토큰·큐까지 지웁니다.
Claude·Codex의 원본 기록은 지우지 않습니다. 자세한 내용: docs/installation/collector-cli.md
GUI·트레이(설치 프로그램): Windows agent-office-collector-setup-<버전>-windows-amd64.exe, macOS agent-office-collector-<버전>-macos-universal.pkg,
Linux agent-office-collector-desktop deb·rpm. 서명하지 않은 설치 프로그램이라 처음 열 때 OS 경고가 나옵니다(docs/installation/collector-gui.md).
macOS·Windows는 GitHub Actions 러너에서만 검증했고 사람이 쓰는 실장비 확인은 남아 있습니다.
