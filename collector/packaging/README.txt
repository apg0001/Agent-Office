Agent Office Collector Core

1. 웹의 /devices에서 PC 등록 코드를 받습니다.
2. agent-office-collector register --server https://<서버> --code <코드> --name <PC 이름>
3. agent-office-collector hooks install --tool claude   (미리보기 확인 후 --yes)
4. agent-office-collector service install --activate    (로그인 시 자동 시작)

정리: agent-office-collector-uninstall (Windows: uninstall.ps1). --purge는 설정·토큰·큐까지 지웁니다.
Claude·Codex의 원본 기록은 지우지 않습니다. 자세한 내용: docs/installation/collector-cli.md
macOS·Windows 실행은 아직 실장비로 검증하지 않았습니다.
