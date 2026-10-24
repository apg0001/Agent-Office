"""알림 공급자(#71 Discord, #72 Telegram, #73 Teams Workflows, #74 Email SMTP).

비밀(Webhook URL·봇 토큰)은 호출자가 복호화해 넘긴다. 공급자별로 허용 호스트를 정해 내부망
주소로 보내는 SSRF를 막고, 실패는 코드만 돌려준다(응답 본문·URL은 남기지 않음).
"""

from __future__ import annotations

import json
import re
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Protocol
from urllib.parse import urlsplit

from agent_office.auth import accounts, mailer

TIMEOUT_SECONDS = 10
MAX_EMAIL_RECIPIENTS = 10


class DeliveryError(Exception):
    def __init__(self, code: str, retryable: bool) -> None:
        super().__init__(code)
        self.code = code
        self.retryable = retryable


class ConfigError(Exception):
    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


class Provider(Protocol):
    @property
    def name(self) -> str: ...

    def validate(self, config: dict[str, Any], secret: str | None) -> dict[str, Any]: ...

    def send(
        self, text: str, config: dict[str, Any], secret: str | None, connection: Any
    ) -> None: ...


def _post_json(url: str, body: dict[str, Any]) -> None:
    request = urllib.request.Request(
        url,
        data=json.dumps(body).encode(),
        method="POST",
        headers={"Content-Type": "application/json", "User-Agent": "agent-office"},
    )
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:  # noqa: S310
            response.read(1024)
    except urllib.error.HTTPError as error:
        retryable = error.code == 429 or error.code >= 500
        raise DeliveryError(f"http_{error.code}", retryable) from None
    except (urllib.error.URLError, TimeoutError, OSError):
        raise DeliveryError("connect_failed", True) from None


def _https_url(value: str | None, hosts: tuple[str, ...], path_prefix: str = "") -> str:
    if not value:
        raise ConfigError("secret_required")
    parts = urlsplit(value.strip())
    host = (parts.hostname or "").lower()
    allowed = any(host == h or host.endswith("." + h) for h in hosts)
    if parts.scheme != "https" or not allowed or not parts.path.startswith(path_prefix):
        raise ConfigError("webhook_url_not_allowed")
    if parts.username or parts.password:
        raise ConfigError("webhook_url_not_allowed")
    return value.strip()


@dataclass(frozen=True)
class Discord:
    name: str = "discord"

    def validate(self, config: dict[str, Any], secret: str | None) -> dict[str, Any]:
        _https_url(secret, ("discord.com", "discordapp.com"), "/api/webhooks/")
        return {}

    def send(self, text: str, config: dict[str, Any], secret: str | None, connection: Any) -> None:
        url = _https_url(secret, ("discord.com", "discordapp.com"), "/api/webhooks/")
        # 멘션(@everyone 등)을 해석하지 않는다.
        _post_json(url, {"content": text, "allowed_mentions": {"parse": []}})


@dataclass(frozen=True)
class Telegram:
    name: str = "telegram"

    def validate(self, config: dict[str, Any], secret: str | None) -> dict[str, Any]:
        if not secret or not re.fullmatch(r"\d{5,20}:[A-Za-z0-9_-]{30,64}", secret.strip()):
            raise ConfigError("invalid_bot_token")
        chat = str(config.get("chat_id", "")).strip()
        if not re.fullmatch(r"-?\d{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31}", chat):
            raise ConfigError("invalid_chat_id")
        return {"chat_id": chat}

    def send(self, text: str, config: dict[str, Any], secret: str | None, connection: Any) -> None:
        token = (secret or "").strip()
        _post_json(
            f"https://api.telegram.org/bot{token}/sendMessage",
            {"chat_id": config["chat_id"], "text": text, "disable_web_page_preview": True},
        )


@dataclass(frozen=True)
class TeamsWorkflows:
    """Microsoft Teams Workflows(Power Automate) "Webhook 요청 수신" 트리거 주소."""

    name: str = "teams_workflows"
    hosts: tuple[str, ...] = ("logic.azure.com", "api.powerplatform.com")

    def validate(self, config: dict[str, Any], secret: str | None) -> dict[str, Any]:
        _https_url(secret, self.hosts)
        return {}

    def send(self, text: str, config: dict[str, Any], secret: str | None, connection: Any) -> None:
        url = _https_url(secret, self.hosts)
        card = {
            "type": "AdaptiveCard",
            "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
            "version": "1.4",
            "body": [
                {"type": "TextBlock", "text": line, "wrap": True} for line in text.split("\n")
            ],
        }
        _post_json(
            url,
            {
                "type": "message",
                "attachments": [
                    {"contentType": "application/vnd.microsoft.card.adaptive", "content": card}
                ],
            },
        )


@dataclass(frozen=True)
class EmailSmtp:
    """관리 화면 SMTP 설정(#67)으로 보낸다. 수신자는 최대 10명."""

    name: str = "email_smtp"

    def validate(self, config: dict[str, Any], secret: str | None) -> dict[str, Any]:
        raw = config.get("recipients", [])
        if not isinstance(raw, list) or not 1 <= len(raw) <= MAX_EMAIL_RECIPIENTS:
            raise ConfigError("invalid_recipients")
        recipients = []
        for value in raw:
            email = accounts.normalize_email(str(value))
            if email is None:
                raise ConfigError("invalid_recipients")
            recipients.append(email)
        return {"recipients": sorted(set(recipients))}

    def send(self, text: str, config: dict[str, Any], secret: str | None, connection: Any) -> None:
        sender = mailer.get_mailer(connection)
        if not sender.configured:
            raise DeliveryError("mail_not_configured", False)
        subject = text.split("\n", 1)[0][:120]
        for recipient in config.get("recipients", []):
            if not sender.send(mailer.OutgoingMail(to=recipient, subject=subject, body=text)):
                raise DeliveryError("smtp_failed", True)


PROVIDERS: dict[str, Provider] = {
    p.name: p for p in (Discord(), Telegram(), TeamsWorkflows(), EmailSmtp())
}
