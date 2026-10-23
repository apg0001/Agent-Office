"""인증·테스트 메일 발송(#23, #67). 설정이 없으면 보내지 않고 토큰·주소를 로그에 남기지 않는다.

설정은 관리 화면에서 저장한 값(`mail.smtp`, 암호는 암호화 저장)을 먼저 쓰고, 없으면 환경 변수를
쓴다. 메일 링크 기준 주소도 같은 순서(`site.public_url` → `AGENT_OFFICE_PUBLIC_URL`)다.
"""

from __future__ import annotations

import logging
import os
import smtplib
import ssl
from dataclasses import dataclass
from email.message import EmailMessage
from typing import Protocol

from sqlalchemy import Connection

from agent_office import instance, secret_store

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


def smtp_settings(connection: Connection) -> SmtpSettings | None:
    stored = instance.get_dict(connection, instance.SMTP_KEY)
    if not stored.get("host") or not stored.get("sender"):
        return smtp_settings_from_env()
    password = None
    if secret_store.exists(connection, instance.SMTP_PASSWORD_SECRET):
        try:
            password = secret_store.get(connection, instance.SMTP_PASSWORD_SECRET)
        except (secret_store.SecretKeyMissing, secret_store.SecretUnreadable):
            # 키가 바뀌었으면 인증 없이 시도하지 않고 발송 불가로 둔다.
            logger.warning("smtp password unreadable; mail disabled")
            return None
    return SmtpSettings(
        host=str(stored["host"]),
        port=int(stored.get("port", 587)),
        username=stored.get("username") or None,
        password=password,
        sender=str(stored["sender"]),
        starttls=bool(stored.get("starttls", True)),
    )


class SmtpError(Exception):
    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


class SmtpMailer:
    def __init__(self, settings: SmtpSettings | None, base_url: str | None) -> None:
        self.settings = settings
        self.base_url = base_url

    @property
    def configured(self) -> bool:
        return self.settings is not None and self.base_url is not None

    def deliver(self, mail: OutgoingMail) -> None:
        """보내고 실패하면 원인 코드만 담은 SmtpError. 관리자 테스트 발송이 쓴다."""
        if self.settings is None:
            raise SmtpError("not_configured")
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
        except smtplib.SMTPAuthenticationError as error:
            raise SmtpError("auth_failed") from error
        except smtplib.SMTPNotSupportedError as error:
            raise SmtpError("starttls_unsupported") from error
        except ssl.SSLError as error:
            raise SmtpError("tls_failed") from error
        except smtplib.SMTPRecipientsRefused as error:
            raise SmtpError("recipient_refused") from error
        except smtplib.SMTPException as error:
            raise SmtpError("smtp_error") from error
        except TimeoutError as error:
            raise SmtpError("timeout") from error
        except OSError as error:
            raise SmtpError("connect_failed") from error

    def send(self, mail: OutgoingMail) -> bool:
        try:
            self.deliver(mail)
        except SmtpError as error:
            # 주소·본문(토큰)은 남기지 않는다.
            logger.warning("mail delivery failed: %s", error.code)
            return False
        return True


def public_url(connection: Connection) -> str | None:
    stored = instance.get_dict(connection, instance.SITE_KEY).get("public_url")
    value = str(stored or os.environ.get(PUBLIC_URL_ENV, "")).rstrip("/")
    return value or None


_override: Mailer | None = None


def get_mailer(connection: Connection) -> Mailer:
    if _override is not None:
        return _override
    return SmtpMailer(smtp_settings(connection), public_url(connection))


def set_mailer(mailer: Mailer | None) -> None:
    """테스트용 발송기 교체. None이면 설정 기반 발송기로 돌아간다."""
    global _override
    _override = mailer
