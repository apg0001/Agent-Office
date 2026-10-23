# ADR-0002: Collector 수집 소스 우선순위와 app-server 제한

- 상태: 제안됨(EP-02 PoC 근거, 소유자 검토 대기)
- 날짜: 2026-10-23
- 관련 이슈: #17, #19, #20, #21

## 맥락

마스터 사양은 Hook과 JSONL tail을 결합하고, 접근 가능한 실행 방식에서만 app-server를 보조로 쓰라고 합니다. EP-02 실측에서 다음을 확인했습니다.

- Claude Code 2.1.308(Linux arm64, `-p`): Hook과 transcript가 `tool_use_id`·`prompt_id`를 공유하고, transcript는 append-only였습니다(#19, #20).
- Codex CLI 0.162.0-alpha.17.2(Windows): Hook 5종은 관측됐지만 `PostToolUse`는 관측되지 않았고, transcript 내부 형식은 안정 계약이 아닙니다(#17).
- Codex app-server 0.163.0: 같은 app-server 프로세스의 thread만 보이고, 다른 프로세스의 세션에는 붙을 수 없습니다. 제어 소켓에 연결하면 `command/exec`, `fs/writeFile` 같은 실행·쓰기 권한도 갖습니다(#21).

## 결정

1. 1차 소스는 Hook입니다. 실제 이벤트 시점과 식별자를 가장 먼저, 가장 적은 해석으로 제공합니다.
2. 2차 소스는 transcript JSONL입니다. Hook 누락(예: `Stop` 미관측)을 보완하는 용도로만 읽고, 두 소스가 공유하는 식별자로 만든 결정적 키와 UUIDv5 `event_id`로 중복을 제거합니다(#20 규칙).
3. Codex transcript는 버전별로 검증된 구조만 읽고, 검증되지 않은 버전은 `unknown`으로 둡니다.
4. Codex app-server는 기본으로 쓰지 않습니다. 쓰려면 별도 Issue로 실제 대화형 CLI·Desktop의 공유 daemon 사용을 실측하고, 읽기 전용 메서드 허용 목록, 사용자 명시 동의, 승인 요청에 응답하지 않는 규칙을 갖춘 뒤 이 ADR을 개정합니다.

## 결과

- Collector는 Codex·Claude 모두 Hook 설치가 필요합니다. Hook 설치·제거 안전성(#31)이 핵심 위험이 됩니다.
- Codex에서 `tool.completed`는 Hook만으로 만들 수 없는 버전이 있어, 해당 버전은 완료를 `unknown`으로 둡니다(#17).
- app-server를 쓰지 않으므로 Collector는 Codex 실행 권한 채널에 접근하지 않습니다. 대신 app-server만 제공하는 세부 알림(계획 갱신, 승인 해결 등)은 초기 버전에서 관측하지 않습니다.
