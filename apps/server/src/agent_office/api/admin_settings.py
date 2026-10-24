"""관리자 인스턴스 설정: 사이트 주소, 가입 정책, SMTP, OAuth(#67).

비밀(SMTP 암호, OAuth client secret)은 쓰기만 하고 응답에는 설정 여부만 돌려준다(평문 재표시 금지).
"""

from __future__ import annotations

import datetime as dt
import os
from typing import Annotated, Any, Literal
from urllib.parse import urlsplit

from fastapi import APIRouter, Depends, HTTPException, Path, Response, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import Connection, select

from agent_office import audit, backup, instance, monitoring, retention, secret_store
from agent_office.api.deps import CsrfDep, CurrentUser, DbConnection, UserDep
from agent_office.auth import accounts, mailer
from agent_office.db import schema

router = APIRouter(prefix="/api/v1/admin/settings", tags=["admin"])

ADMIN_ROLES = ("SUPER_ADMIN", "ADMIN")


def require_admin(user: UserDep) -> CurrentUser:
    if user.instance_role not in ADMIN_ROLES:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="forbidden")
    return user


AdminDep = Annotated[CurrentUser, Depends(require_admin)]
Provider = Annotated[Literal["google", "github"], Path()]


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class SiteSettings(StrictModel):
    service_name: str = Field(min_length=1, max_length=80)
    public_url: str | None = Field(default=None, max_length=300)


class SignupPolicyBody(StrictModel):
    policy: accounts.SignupPolicy


class SmtpSettingsBody(StrictModel):
    host: str = Field(min_length=1, max_length=253)
    port: int = Field(ge=1, le=65535)
    username: str | None = Field(default=None, max_length=254)
    sender: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)
    starttls: bool = True
    # None: 기존 암호 유지, "": 암호 삭제, 그 밖: 새 암호.
    password: str | None = Field(default=None, max_length=512)


class SmtpView(StrictModel):
    host: str
    port: int
    username: str | None
    sender: str
    starttls: bool
    password_set: bool


class OAuthBody(StrictModel):
    enabled: bool
    client_id: str = Field(max_length=255)
    # None: 기존 값 유지, "": 삭제.
    client_secret: str | None = Field(default=None, max_length=512)


class OAuthView(StrictModel):
    enabled: bool
    client_id: str
    client_secret_set: bool


class SettingsView(StrictModel):
    site: SiteSettings
    signup_policy: accounts.SignupPolicy
    smtp: SmtpView | None
    smtp_source: Literal["settings", "environment", "none"]
    oauth: dict[str, OAuthView]
    secret_storage_available: bool
    blocked_email_domains: list[str]


class BlockedDomainsBody(StrictModel):
    domains: list[str] = Field(max_length=accounts.MAX_BLOCKED_DOMAINS)


class SmtpTestResult(StrictModel):
    ok: bool
    error: str | None = None


DEFAULT_SERVICE_NAME = "Agent Office"


def _site(connection: Connection) -> SiteSettings:
    stored = instance.get_dict(connection, instance.SITE_KEY)
    return SiteSettings(
        service_name=str(stored.get("service_name") or DEFAULT_SERVICE_NAME),
        public_url=stored.get("public_url"),
    )


def _view(connection: Connection) -> SettingsView:
    smtp = instance.get_dict(connection, instance.SMTP_KEY)
    smtp_view = (
        SmtpView(
            host=str(smtp["host"]),
            port=int(smtp.get("port", 587)),
            username=smtp.get("username"),
            sender=str(smtp["sender"]),
            starttls=bool(smtp.get("starttls", True)),
            password_set=secret_store.exists(connection, instance.SMTP_PASSWORD_SECRET),
        )
        if smtp.get("host")
        else None
    )
    source: Literal["settings", "environment", "none"] = (
        "settings" if smtp_view else ("environment" if mailer.smtp_settings_from_env() else "none")
    )
    oauth = {}
    for provider in instance.OAUTH_PROVIDERS:
        stored = instance.get_dict(connection, instance.oauth_key(provider))
        oauth[provider] = OAuthView(
            enabled=bool(stored.get("enabled", False)),
            client_id=str(stored.get("client_id", "")),
            client_secret_set=secret_store.exists(connection, instance.oauth_secret(provider)),
        )
    return SettingsView(
        site=_site(connection),
        signup_policy=accounts.signup_policy(connection),
        smtp=smtp_view,
        smtp_source=source,
        oauth=oauth,
        secret_storage_available=secret_store.available(),
        blocked_email_domains=accounts.blocked_domains(connection),
    )


def _bad(code: str) -> HTTPException:
    return HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=code)


def _store_secret(connection: Connection, name: str, value: str | None, user: CurrentUser) -> None:
    if value is None:
        return
    if value == "":
        secret_store.delete(connection, name)
        return
    if not secret_store.available():
        # 키 없이 평문으로 저장하지 않는다.
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="secret_key_missing"
        )
    secret_store.put(connection, name, value, user.id)


def normalize_public_url(value: str) -> str | None:
    """`https://office.example.com` 형태만. 경로·쿼리·조각·자격 증명은 받지 않는다."""
    parts = urlsplit(value.strip())
    if parts.scheme not in ("http", "https") or not parts.hostname:
        return None
    if parts.username or parts.password or parts.query or parts.fragment:
        return None
    if parts.path not in ("", "/"):
        return None
    return f"{parts.scheme}://{parts.netloc.lower()}"


@router.get("", response_model=SettingsView)
def read_settings(_admin: AdminDep, connection: DbConnection) -> SettingsView:
    return _view(connection)


@router.put("/site", response_model=SettingsView)
def update_site(
    body: SiteSettings, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> SettingsView:
    url = None
    if body.public_url:
        url = normalize_public_url(body.public_url)
        if url is None:
            raise _bad("invalid_public_url")
    instance.put_value(
        connection,
        instance.SITE_KEY,
        {"service_name": body.service_name.strip(), "public_url": url},
        admin.id,
    )
    audit.record(connection, "settings.site", "success", actor_user_id=admin.id)
    return _view(connection)


@router.put("/signup-policy", response_model=SettingsView)
def update_signup_policy(
    body: SignupPolicyBody, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> SettingsView:
    instance.put_value(connection, accounts.SIGNUP_POLICY_KEY, body.policy, admin.id)
    audit.record(
        connection,
        "settings.signup_policy",
        "success",
        actor_user_id=admin.id,
        details={"reason": body.policy},
    )
    return _view(connection)


@router.put("/blocked-domains", response_model=SettingsView)
def update_blocked_domains(
    body: BlockedDomainsBody, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> SettingsView:
    """가입을 막을 이메일 도메인(일회용 메일 등, #28)."""
    domains = []
    for raw in body.domains:
        domain = accounts.normalize_domain(raw)
        if domain is None:
            raise _bad("invalid_domain")
        domains.append(domain)
    instance.put_value(connection, accounts.BLOCKED_DOMAINS_KEY, sorted(set(domains)), admin.id)
    audit.record(connection, "settings.blocked_domains", "success", actor_user_id=admin.id)
    return _view(connection)


@router.put("/smtp", response_model=SettingsView)
def update_smtp(
    body: SmtpSettingsBody, admin: AdminDep, _csrf: CsrfDep, connection: DbConnection
) -> SettingsView:
    sender = accounts.normalize_email(body.sender)
    if sender is None:
        raise _bad("invalid_sender")
    host = body.host.strip().lower()
    if any(ch.isspace() or ch in "/@" for ch in host):
        raise _bad("invalid_host")
    _store_secret(connection, instance.SMTP_PASSWORD_SECRET, body.password, admin)
    instance.put_value(
        connection,
        instance.SMTP_KEY,
        {
            "host": host,
            "port": body.port,
            "username": (body.username or "").strip() or None,
            "sender": sender,
            "starttls": body.starttls,
        },
        admin.id,
    )
    audit.record(connection, "settings.smtp", "success", actor_user_id=admin.id)
    return _view(connection)


@router.delete("/smtp", status_code=status.HTTP_204_NO_CONTENT)
def delete_smtp(admin: AdminDep, _csrf: CsrfDep, connection: DbConnection) -> Response:
    instance.delete_value(connection, instance.SMTP_KEY)
    secret_store.delete(connection, instance.SMTP_PASSWORD_SECRET)
    audit.record(connection, "settings.smtp_deleted", "success", actor_user_id=admin.id)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.post("/smtp/test", response_model=SmtpTestResult)
def test_smtp(admin: AdminDep, _csrf: CsrfDep, connection: DbConnection) -> SmtpTestResult:
    """관리자 본인 주소로 시험 메일을 보낸다. 실패는 원인 코드만 돌려준다."""
    to = connection.execute(
        select(schema.users.c.email).where(schema.users.c.id == admin.id)
    ).scalar_one()
    sender = mailer.get_mailer(connection)
    mail = mailer.OutgoingMail(
        to=to,
        subject=f"{_site(connection).service_name} 메일 설정 시험",
        body="Agent Office 메일 설정 시험 메일입니다. 받았다면 설정이 올바릅니다.",
    )
    if isinstance(sender, mailer.SmtpMailer):
        try:
            sender.deliver(mail)
        except mailer.SmtpError as error:
            result = SmtpTestResult(ok=False, error=error.code)
        else:
            result = SmtpTestResult(ok=True)
    else:
        result = SmtpTestResult(ok=sender.send(mail))
    audit.record(
        connection,
        "settings.smtp_test",
        "success" if result.ok else "failure",
        actor_user_id=admin.id,
        details={"reason": result.error} if result.error else None,
    )
    return result


@router.put("/oauth/{provider}", response_model=SettingsView)
def update_oauth(
    provider: Provider,
    body: OAuthBody,
    admin: AdminDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> SettingsView:
    client_id = body.client_id.strip()
    secret_name = instance.oauth_secret(provider)
    _store_secret(connection, secret_name, body.client_secret, admin)
    if body.enabled and (not client_id or not secret_store.exists(connection, secret_name)):
        raise _bad("oauth_incomplete")
    instance.put_value(
        connection,
        instance.oauth_key(provider),
        {"enabled": body.enabled, "client_id": client_id},
        admin.id,
    )
    audit.record(
        connection,
        "settings.oauth",
        "success",
        actor_user_id=admin.id,
        details={"reason": provider},
    )
    return _view(connection)


metrics_router = APIRouter(prefix="/api/v1/admin", tags=["admin"])


@metrics_router.get("/metrics")
def read_metrics(_admin: AdminDep, connection: DbConnection) -> dict[str, Any]:
    """운영 지표(#59). 숫자·코드만 담고 사용자·장치 식별자와 주소는 없다."""
    return {
        "host": monitoring.host_metrics(),
        "database": monitoring.database_metrics(connection),
        "collection": monitoring.collection_metrics(connection),
        "readiness": monitoring.readiness(connection),
        "external_heartbeat": {
            "configured": monitoring.heartbeat_url() is not None,
            "last_at": monitoring.last_heartbeat["at"],
            "last_ok": monitoring.last_heartbeat["ok"],
        },
        "retention": {"last_at": retention.last_run["at"], "last_ok": retention.last_run["ok"]},
        "backup": {"configured": backup.configured(), **backup.read_status(connection)},
    }


class BackupRunResult(StrictModel):
    ok: bool
    error: str | None = None
    name: str | None = None
    size_bytes: int | None = None


def _run_backup_action(action: str) -> BackupRunResult:
    from pathlib import Path as FsPath

    try:
        if action == "backup":
            info = backup.create_backup()
            return BackupRunResult(ok=True, name=info.name, size_bytes=info.size_bytes)
        directory = FsPath(os.environ.get(backup.BACKUP_DIR_ENV, backup.DEFAULT_BACKUP_DIR))
        latest = backup.backups(directory)
        if not latest:
            return BackupRunResult(ok=False, error="no_backup")
        backup.restore_test(latest[-1])
        return BackupRunResult(ok=True, name=latest[-1].name)
    except backup.BackupError as error:
        return BackupRunResult(ok=False, error=error.code)


@metrics_router.post("/backup/{action}", response_model=BackupRunResult)
def run_backup(
    action: Annotated[Literal["run", "restore-test"], Path()],
    admin: AdminDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> BackupRunResult:
    """관리 화면의 "지금 백업"·"복원 시험"(#68). 결과는 코드만 돌려주고 상태에 기록한다."""
    if not backup.configured():
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="backup_key_missing"
        )
    now = dt.datetime.now(dt.UTC).isoformat()
    result = _run_backup_action("backup" if action == "run" else "restore")
    fields: dict[str, Any] = (
        {"last_backup_at": now, "last_backup_ok": result.ok, "last_error": result.error}
        if action == "run"
        else {
            "last_restore_test_at": now,
            "last_restore_test_ok": result.ok,
            "last_error": result.error,
        }
    )
    if result.ok and action == "run":
        fields["last_backup_size_bytes"] = result.size_bytes
    instance.put_value(
        connection, backup.STATUS_KEY, {**backup.read_status(connection), **fields}, admin.id
    )
    audit.record(
        connection,
        "backup.run" if action == "run" else "backup.restore_test",
        "success" if result.ok else "failure",
        actor_user_id=admin.id,
        details={"reason": result.error} if result.error else None,
    )
    return result
