"""인스턴스 비밀 암호화 저장(#67). AES-256-GCM, 키는 인프라 비밀 `AGENT_OFFICE_SECRET_KEY`.

키는 DB와 따로 두므로 DB 백업만 유출돼도 비밀을 읽을 수 없다. 키가 없으면 비밀을 저장하지 않는다.
암호문 형식: `v1` + 12바이트 nonce + GCM 암호문. 비밀 이름을 AAD로 묶어 다른 칸으로 옮길 수 없다.
"""

from __future__ import annotations

import base64
import binascii
import os
import uuid

from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from sqlalchemy import Connection, func, select
from sqlalchemy.dialects.postgresql import insert

from agent_office.db import schema

SECRET_KEY_ENV = "AGENT_OFFICE_SECRET_KEY"
_VERSION = b"v1"


class SecretKeyMissing(Exception):
    """키가 설정되지 않았거나 형식이 틀렸다."""


class SecretUnreadable(Exception):
    """키가 바뀌었거나 암호문이 손상됐다."""


def _key() -> bytes:
    raw = os.environ.get(SECRET_KEY_ENV, "")
    try:
        key = base64.urlsafe_b64decode(raw + "=" * (-len(raw) % 4)) if raw else b""
    except (binascii.Error, ValueError) as error:
        raise SecretKeyMissing from error
    if len(key) != 32:
        raise SecretKeyMissing
    return key


def available() -> bool:
    try:
        _key()
    except SecretKeyMissing:
        return False
    return True


def encrypt(name: str, plaintext: str) -> bytes:
    nonce = os.urandom(12)
    return _VERSION + nonce + AESGCM(_key()).encrypt(nonce, plaintext.encode(), name.encode())


def decrypt(name: str, blob: bytes) -> str:
    if not blob.startswith(_VERSION) or len(blob) < len(_VERSION) + 12 + 16:
        raise SecretUnreadable
    nonce, body = blob[2:14], blob[14:]
    try:
        return AESGCM(_key()).decrypt(nonce, body, name.encode()).decode()
    except InvalidTag as error:
        raise SecretUnreadable from error


def put(connection: Connection, name: str, plaintext: str, user_id: uuid.UUID | None) -> None:
    table = schema.instance_secrets
    statement = insert(table).values(
        key=name, ciphertext=encrypt(name, plaintext), updated_by_user_id=user_id
    )
    connection.execute(
        statement.on_conflict_do_update(
            index_elements=[table.c.key],
            set_={
                "ciphertext": statement.excluded.ciphertext,
                "updated_by_user_id": statement.excluded.updated_by_user_id,
                "updated_at": func.now(),
            },
        )
    )


def delete(connection: Connection, name: str) -> None:
    table = schema.instance_secrets
    connection.execute(table.delete().where(table.c.key == name))


def exists(connection: Connection, name: str) -> bool:
    table = schema.instance_secrets
    return connection.execute(select(table.c.key).where(table.c.key == name)).first() is not None


def get(connection: Connection, name: str) -> str | None:
    table = schema.instance_secrets
    blob = connection.execute(
        select(table.c.ciphertext).where(table.c.key == name)
    ).scalar_one_or_none()
    return None if blob is None else decrypt(name, bytes(blob))
