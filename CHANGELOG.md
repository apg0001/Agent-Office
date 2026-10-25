# 변경 기록

사용자에게 영향을 주는 변경만 기록하고, 실제 릴리스가 아닌 작업을 릴리스로 표현하지 않습니다. 아직 Stable 릴리스는 없습니다.

## Unreleased

## 0.1.0-beta.1 (2026-10-25)

첫 Beta. 자세한 내용과 검증 범위는 `docs/releases/0.1.0-beta.1.md`를 봅니다.

- 서버: 이벤트 수집·상태 엔진·실시간(개인·팀·통합), 보존·계정 삭제
- 계정: 이메일·OAuth 로그인, 가입 정책·관리자 승인·남용 방지·감사 로그
- 설치·운영: bootstrap·설치 마법사, Caddy 단일 진입점, 백업·복원 시험, 알림, 승인형 배포·롤백, 보안 점검
- 팀 사무실과 공유, 아이소메트릭 사무실 화면·상세 패널·계획 진행도
- 헤드리스 Collector(Linux 검증, macOS·Windows는 GitHub Actions 실제 러너 검증, 사람이 쓰는 데스크톱 조작은 미검증)
- Collector 장치 토큰 OS 보안 저장소 보관·기존 파일 토큰 이전·`logout`(Linux Secret Service 컨테이너·macOS Keychain·Windows Credential Manager 러너 검증), GUI Linux 트레이·GUI 자동 시작(#41, #43)
