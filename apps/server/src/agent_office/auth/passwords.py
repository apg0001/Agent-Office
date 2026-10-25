"""비밀번호 해시(#23). Argon2id, OWASP 최소 권장값(19MiB·2회·병렬 1)으로 Pi에서도 가볍다."""

from __future__ import annotations

from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerificationError, VerifyMismatchError

MIN_PASSWORD_LENGTH = 10
MAX_PASSWORD_LENGTH = 128

_hasher = PasswordHasher(time_cost=2, memory_cost=19_456, parallelism=1)
# 없는 계정으로 로그인할 때도 같은 시간이 걸리도록 대신 검증할 해시.
_DUMMY_HASH = _hasher.hash("agent-office-dummy-password")


def password_problem(password: str) -> str | None:
    """정책 위반 코드. 길이만 본다(NIST SP 800-63B: 조합 규칙 대신 길이)."""
    if len(password) < MIN_PASSWORD_LENGTH:
        return "password_too_short"
    if len(password) > MAX_PASSWORD_LENGTH:
        return "password_too_long"
    return None


def hash_password(password: str) -> str:
    return _hasher.hash(password)


def verify_password(encoded: str | None, password: str) -> bool:
    """일치 여부. 해시가 없거나(OAuth 전용) 깨졌어도 같은 비용을 치르고 False."""
    try:
        return _hasher.verify(encoded or _DUMMY_HASH, password) and encoded is not None
    except (VerifyMismatchError, VerificationError, InvalidHashError):
        return False


def needs_rehash(encoded: str) -> bool:
    try:
        return _hasher.check_needs_rehash(encoded)
    except InvalidHashError:
        return True
