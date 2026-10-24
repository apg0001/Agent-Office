"""알림 공통 모델과 안전한 문구(#70).

문구는 서버 코드가 정한 종류별 템플릿으로만 만든다. 사용자 입력·원문·토큰·경로는 넣지 않고,
보내기 직전에 URL·경로·긴 토큰처럼 보이는 문자열을 한 번 더 지운다.
"""

from __future__ import annotations

import datetime as dt
import re
from dataclasses import dataclass, field
from typing import Literal

Severity = Literal["critical", "high", "warning", "info"]
SEVERITY_LABELS: dict[str, str] = {
    "critical": "긴급",
    "high": "높음",
    "warning": "주의",
    "info": "정보",
}


@dataclass(frozen=True)
class Notification:
    """kind는 `backup.failed`처럼 점으로 나눈 코드다. 같은 kind는 중복 억제 단위가 된다."""

    kind: str
    severity: Severity
    summary: str
    action: str = ""
    occurred_at: dt.datetime = field(default_factory=lambda: dt.datetime.now(dt.UTC))
    # 원인 코드(예: pg_dump_failed). 자유 문장이 아니라 코드만.
    code: str | None = None


# 종류별 템플릿. 새 알림은 여기에만 추가한다.
TEMPLATES: dict[str, tuple[Severity, str, str]] = {
    "backup.failed": (
        "critical",
        "백업이 실패했습니다.",
        "관리 화면에서 원인 코드를 확인하고 다시 실행하세요.",
    ),
    "backup.recovered": ("info", "백업이 다시 성공했습니다.", ""),
    "restore_test.failed": (
        "high",
        "백업 복원 시험이 실패했습니다.",
        "백업 파일과 키를 확인하고 관리 화면에서 복원 시험을 다시 실행하세요.",
    ),
    "restore_test.recovered": ("info", "백업 복원 시험이 다시 성공했습니다.", ""),
    "retention.failed": (
        "warning",
        "보존 기간 정리가 실패했습니다.",
        "서버 로그의 오류 종류를 확인하세요.",
    ),
    "heartbeat.failed": (
        "warning",
        "외부 Heartbeat 전송이 실패하고 있습니다.",
        "네트워크와 Heartbeat 주소를 확인하세요.",
    ),
    "heartbeat.recovered": ("info", "외부 Heartbeat 전송이 복구됐습니다.", ""),
    "channel.test": ("info", "Agent Office 알림 채널 시험입니다.", "받았다면 설정이 올바릅니다."),
}


def build(kind: str, code: str | None = None) -> Notification:
    severity, summary, action = TEMPLATES[kind]
    safe_code = code if code and re.fullmatch(r"[a-z0-9_]{1,64}", code) else None
    return Notification(
        kind=kind, severity=severity, summary=summary, action=action, code=safe_code
    )


_SCRUB = [
    re.compile(r"https?://\S+"),
    # 사용자 파일 경로(유닉스·윈도우).
    re.compile(r"(?:/home|/Users|/root|/var|/etc|/tmp)/\S*"),
    re.compile(r"[A-Za-z]:\\\S*"),
    # 토큰처럼 긴 영숫자·base64 덩어리.
    re.compile(r"[A-Za-z0-9_\-+/=]{32,}"),
    re.compile(r"\S+@\S+\.\S+"),
]


def scrub(text: str) -> str:
    for pattern in _SCRUB:
        text = pattern.sub("[제거됨]", text)
    return text[:1500]


def instance_label(name: str) -> str:
    """서비스 이름은 관리자 입력이라 짧게 자르고 제어 문자·멘션을 뺀다."""
    cleaned = re.sub(r"[\x00-\x1f\x7f@<>`*_~|]", "", name).strip()
    return (cleaned or "Agent Office")[:40]


def render(notification: Notification, instance: str) -> str:
    """모든 공급자가 같은 본문을 쓴다(일반 텍스트, 마크다운·멘션 해석 없음)."""
    lines = [
        f"[{SEVERITY_LABELS[notification.severity]}] {instance_label(instance)}",
        notification.summary,
    ]
    if notification.code:
        lines.append(f"원인 코드: {notification.code}")
    if notification.action:
        lines.append(f"조치: {notification.action}")
    lines.append(notification.occurred_at.strftime("%Y-%m-%d %H:%M UTC"))
    return scrub("\n".join(lines))
