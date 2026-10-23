# GitHub 작업·보호 규칙

관련 이슈: #14

## 브랜치 흐름

1. 한국어 Issue를 만들고 실제 번호를 확인합니다.
2. `develop` 최신 상태에서 Issue 번호가 포함된 작업 브랜치를 만듭니다.
3. 구현·검증·문서·자체 검토 후 `develop` 대상 PR을 만듭니다.
4. 리뷰 대화를 해결하고 보호 규칙을 통과한 뒤 병합합니다.
5. `main`은 Beta·보안·백업·호환성 검증과 관리자 릴리스 승인이 있을 때만 PR로 변경합니다.

## 보호 설정 기준선

`main`과 `develop`에 다음 규칙을 적용합니다.

적용·검증일: 2026-10-23 (KST)

- 관리자 포함 PR 경유 필수
- force-push와 브랜치 삭제 금지
- 모든 리뷰 대화 해결 필수
- 필수 승인 수 0: 현재 단일 관리자 저장소에서 자기 PR 승인 불가로 인한 교착 방지
- 필수 상태 검사 미설정: #16에서 CI job 이름이 고정된 뒤 추가
- 서명 커밋과 선형 이력은 현재 미강제: 배포·기여 환경 검증 후 별도 이슈에서 결정

GitHub Branch Protection API로 두 브랜치의 설정을 다시 조회해 관리자 적용, 대화 해결, 강제 push·삭제 차단, 승인 수와 상태 검사 설정을 검증했습니다.

필수 승인 수가 0이어도 PR 템플릿의 검증 근거와 자체 리뷰 기록은 필수입니다. 협업자가 추가되면 필수 승인 수를 1 이상으로 올리고 CODEOWNERS 도입을 검토합니다.

## 감사 명령

```powershell
gh api repos/apg0001/Agent-Office/branches/main/protection
gh api repos/apg0001/Agent-Office/branches/develop/protection
gh pr list --repo apg0001/Agent-Office --state open
```

보호 규칙 변경도 별도 Issue와 감사 기록을 남깁니다. 긴급 상황을 이유로 직접 push하거나 규칙을 임시 해제하지 않습니다.
