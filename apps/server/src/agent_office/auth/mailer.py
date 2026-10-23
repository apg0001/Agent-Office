"""인증 메일 발송(#23). SMTP 설정이 없으면 보내지 않고, 토큰·주소를 로그에 남기지 않는다.

메일 본문에는 일회용 링크만 담는다. 설정은 환경 변수로 받으며 관리 화면 설정은 #68이다.
"""

from __future__ import annotations

import logging
import os
import smtplib
import ssl
from dataclasses import dataclass
from email.message import EmailMessage
from typing import Protocol

logger = logging.getLogger(__name__)

PUBLIC_URL_ENV = "AGENT_OFFICE_PUBLIC_URL"
SMTP_ENV_PREFIX = "AGENT_OFFICE_SMTP_"


@dataclass(frozen=True)
class OutgoingMail:
    to: str
    subject: str
    body: str


class Mailer(Protocol):
    @property
    def configured(self) -> bool: ...

    def send(self, mail: OutgoingMail) -> bool: ...


@dataclass(frozen=True)
class SmtpSettings:
    host: str
    port: int
    username: str | None
    password: str | None
    sender: str
    starttls: bool


def smtp_settings_from_env() -> SmtpSettings | None:
    host = os.environ.get(SMTP_ENV_PREFIX + "HOST")
    sender = os.environ.get(SMTP_ENV_PREFIX + "FROM")
    if not host or not sender:
        return None
    return SmtpSettings(
        host=host,
        port=int(os.environ.get(SMTP_ENV_PREFIX + "PORT", "587")),
        username=os.environ.get(SMTP_ENV_PREFIX + "USERNAME") or None,
        password=os.environ.get(SMTP_ENV_PREFIX + "PASSWORD") or None,
        sender=sender,
        starttls=os.environ.get(SMTP_ENV_PREFIX + "STARTTLS", "true").lower() != "false",
    )


class SmtpMailer:
    def __init__(self, settings: SmtpSettings | None) -> None:
        self.settings = settings

    @property
    def configured(self) -> bool:
        return self.settings is not None and public_url() is not None

    def send(self, mail: OutgoingMail) -> bool:
        if self.settings is None:
            return False
        message = EmailMessage()
        message["From"] = self.settings.sender
        message["To"] = mail.to
        message["Subject"] = mail.subject
        message.set_content(mail.body)
        try:
            with smtplib.SMTP(self.settings.host, self.settings.port, timeout=10) as client:
                if self.settings.starttls:
                    client.starttls(context=ssl.create_default_context())
                if self.settings.username and self.settings.password:
                    client.login(self.settings.username, self.settings.password)
                client.send_message(message)
        except (OSError, smtplib.SMTPException) as error:
            # 주소·본문(토큰)은 남기지 않는다.
            logger.warning("mail delivery failed: %s", type(error).__name__)
            return False
        return True


def public_url() -> str | None:
    value = os.environ.get(PUBLIC_URL_ENV, "").rstrip("/")
    return value or None


_mailer: Mailer | None = None


def get_mailer() -> Mailer:
    global _mailer
    if _mailer is None:
        _mailer = SmtpMailer(smtp_settings_from_env())
    return _mailer


def set_mailer(mailer: Mailer | None) -> None:
    """테스트·관리 설정 변경용."""
    global _mailer
    _mailer = mailer
