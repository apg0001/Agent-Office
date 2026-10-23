# Collector 규칙 참조 구현

관련 이슈: #20

Go Collector(#30 tail, #32 정규화, #33 큐)가 따를 규칙을 Python으로 단순하게 구현한 참조 코드입니다. 운영 코드가 아니며 서버나 Collector가 import하지 않습니다.

- `tail_reference.py`: JSONL append tail과 체크포인트(partial write, BOM, CRLF, 깨진 줄, 과대 줄, truncate, 재작성, 로테이션, 파일 없음, 권한 거부)
- `dedup_reference.py`: Claude Code Hook·transcript 후보 추출, 결정적 중복 제거 키와 UUIDv5 `event_id`

규칙과 실측 근거는 [보고서](../../../docs/poc/collector-dedup-tail.md)에 있습니다. fixture는 `tests/fixtures/collector/claude/dual-source/`이고 테스트는 `tests/test_collector_reference.py`입니다.

```bash
python -m pytest tests/test_collector_reference.py
```
