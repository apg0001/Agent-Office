"""토큰·등록 코드 생성과 해시. 원문은 응답에서 한 번만 내보내고 저장하지 않는다."""

from __future__ import annotations

import hashlib
import hmac
import secrets

DEVICE_TOKEN_PREFIX = "aod_"
# 혼동하기 쉬운 문자(0/O, 1/I/L, U)를 뺀 Crockford base32 계열 알파벳.
REGISTRATION_CODE_ALPHABET = "ABCDEFGHJKMNPQRSTVWXYZ23456789"
REGISTRATION_CODE_LENGTH = 8


def token_hash(value: str) -> bytes:
    return hashlib.sha256(value.encode()).digest()


def new_device_token() -> str:
    return DEVICE_TOKEN_PREFIX + secrets.token_urlsafe(32)


def new_registration_code() -> str:
    """`XXXX-XXXX` 형식. 약 39비트 엔트로피이며 10분 만료·일회용으로만 쓴다."""
    raw = "".join(
        secrets.choice(REGISTRATION_CODE_ALPHABET) for _ in range(REGISTRATION_CODE_LENGTH)
    )
    return f"{raw[:4]}-{raw[4:]}"


def normalize_registration_code(value: str) -> str | None:
    """사용자 입력의 공백·하이픈·대소문자를 정리한다. 형식이 틀리면 None."""
    cleaned = "".join(ch for ch in value.upper() if ch not in "- ")
    if len(cleaned) != REGISTRATION_CODE_LENGTH or any(
        ch not in REGISTRATION_CODE_ALPHABET for ch in cleaned
    ):
        return None
    return f"{cleaned[:4]}-{cleaned[4:]}"


def constant_time_equal(left: bytes, right: bytes) -> bool:
    return hmac.compare_digest(left, right)
