"""PostgreSQL 스키마의 단일 소스(SQLAlchemy Core 메타데이터).

원본 프롬프트·코드·파일 본문·도구 입출력·전체 명령·로컬 경로를 담는 컬럼을 두지 않는다.
이벤트는 이벤트 v1 허용 필드만 저장한다. 마이그레이션은 이 메타데이터와 일치해야 한다
(`tests/test_db_schema.py`).
"""

from __future__ import annotations

from typing import Any

from sqlalchemy import (
    BigInteger,
    Boolean,
    CheckConstraint,
    Column,
    DateTime,
    ForeignKey,
    ForeignKeyConstraint,
    Index,
    Integer,
    LargeBinary,
    MetaData,
    PrimaryKeyConstraint,
    String,
    Table,
    UniqueConstraint,
    func,
    text,
)
from sqlalchemy.dialects.postgresql import ARRAY, JSONB, UUID

NAMING_CONVENTION = {
    "ix": "ix_%(table_name)s_%(column_0_N_name)s",
    "uq": "uq_%(table_name)s_%(column_0_N_name)s",
    "ck": "ck_%(table_name)s_%(constraint_name)s",
    "fk": "fk_%(table_name)s_%(column_0_N_name)s_%(referred_table_name)s",
    "pk": "pk_%(table_name)s",
}

metadata = MetaData(naming_convention=NAMING_CONVENTION)

INSTANCE_ROLES = ("SUPER_ADMIN", "ADMIN", "USER")
USER_STATUSES = ("pending", "active", "disabled")
TEAM_ROLES = ("OWNER", "MANAGER", "MEMBER")
INVITABLE_TEAM_ROLES = ("MANAGER", "MEMBER")
OAUTH_PROVIDERS = ("google", "github")
DEVICE_PLATFORMS = ("windows", "macos", "linux", "unknown")
PROVIDERS = ("codex", "claude", "unknown")
EVENT_SOURCES = ("hook", "jsonl", "app_server", "inferred")
EVENT_TYPES = (
    "session.discovered",
    "session.started",
    "session.ended",
    "turn.started",
    "turn.completed",
    "turn.interrupted",
    "tool.started",
    "tool.completed",
    "tool.failed",
    "approval.requested",
    "approval.resolved",
    "agent.started",
    "agent.completed",
    "plan.updated",
    "unknown",
)
ACTIVITIES = (
    "reading",
    "browsing",
    "coding",
    "testing",
    "planning",
    "delegating",
    "reporting",
    "thinking",
    "waiting_user",
    "idle",
    "unknown",
)
CONFIDENCES = ("observed", "inferred")
SESSION_LIFECYCLES = ("DISCOVERED", "OPEN", "ENDED", "UNKNOWN")
TURN_STATES = ("WORKING", "THINKING", "WAITING_USER", "IDLE", "UNKNOWN")
AGENT_ROLES = ("lead", "subagent")
PLAN_STEP_STATUSES = ("pending", "in_progress", "completed", "unknown")
LAYOUT_SCOPES = ("user", "team")
NOTIFICATION_PROVIDERS = ("discord", "telegram", "teams_workflows", "email_smtp")
SEVERITIES = ("critical", "high", "warning", "info")
AUDIT_OUTCOMES = ("success", "denied", "failure")


def _in(column: str, values: tuple[str, ...]) -> str:
    quoted = ", ".join(f"'{value}'" for value in values)
    return f"{column} IN ({quoted})"


def _uuid_pk() -> Column[Any]:
    return Column(
        "id", UUID(as_uuid=True), primary_key=True, server_default=text("gen_random_uuid()")
    )


def _created_at() -> Column[Any]:
    return Column("created_at", DateTime(timezone=True), nullable=False, server_default=func.now())


def _fk(name: str, target: str, *, ondelete: str, nullable: bool = False) -> Column[Any]:
    return Column(
        name, UUID(as_uuid=True), ForeignKey(target, ondelete=ondelete), nullable=nullable
    )


# --- 사용자·인증 -----------------------------------------------------------

users = Table(
    "users",
    metadata,
    _uuid_pk(),
    Column("email", String(320), nullable=False),
    Column("display_name", String(80), nullable=False),
    # OAuth 전용 계정은 비밀번호 해시가 없다. 값은 Argon2id 인코딩 문자열이다.
    Column("password_hash", String(255), nullable=True),
    Column("instance_role", String(16), nullable=False, server_default="USER"),
    Column("status", String(16), nullable=False, server_default="pending"),
    Column("email_verified_at", DateTime(timezone=True), nullable=True),
    _created_at(),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    CheckConstraint(_in("instance_role", INSTANCE_ROLES), name="instance_role"),
    CheckConstraint(_in("status", USER_STATUSES), name="status"),
    CheckConstraint("email = lower(email) AND position('@' in email) > 1", name="email_normalized"),
)
Index("uq_users_email", users.c.email, unique=True)

auth_identities = Table(
    "auth_identities",
    metadata,
    _uuid_pk(),
    _fk("user_id", "users.id", ondelete="CASCADE"),
    Column("provider", String(16), nullable=False),
    # 제공자가 발급한 불변 subject. 이메일·프로필 원문은 저장하지 않는다.
    Column("provider_subject", String(255), nullable=False),
    _created_at(),
    CheckConstraint(_in("provider", OAUTH_PROVIDERS), name="provider"),
    UniqueConstraint("provider", "provider_subject"),
    UniqueConstraint("user_id", "provider"),
)

login_sessions = Table(
    "login_sessions",
    metadata,
    _uuid_pk(),
    _fk("user_id", "users.id", ondelete="CASCADE"),
    # 토큰 원문은 저장하지 않고 SHA-256 해시만 저장한다.
    Column("token_hash", LargeBinary(32), nullable=False),
    _created_at(),
    Column("last_seen_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    Column("expires_at", DateTime(timezone=True), nullable=False),
    Column("revoked_at", DateTime(timezone=True), nullable=True),
    UniqueConstraint("token_hash"),
    CheckConstraint("expires_at > created_at", name="expiry_after_creation"),
)
Index(
    "ix_login_sessions_user_id_active",
    login_sessions.c.user_id,
    postgresql_where=login_sessions.c.revoked_at.is_(None),
)
Index("ix_login_sessions_expires_at", login_sessions.c.expires_at)


def _one_time_token_table(name: str) -> Table:
    table = Table(
        name,
        metadata,
        _uuid_pk(),
        _fk("user_id", "users.id", ondelete="CASCADE"),
        Column("token_hash", LargeBinary(32), nullable=False),
        _created_at(),
        Column("expires_at", DateTime(timezone=True), nullable=False),
        # 단일 사용: 사용 시각이 기록되면 다시 쓸 수 없다.
        Column("consumed_at", DateTime(timezone=True), nullable=True),
        UniqueConstraint("token_hash"),
        CheckConstraint("expires_at > created_at", name="expiry_after_creation"),
        CheckConstraint(
            "consumed_at IS NULL OR consumed_at >= created_at", name="consumed_after_creation"
        ),
    )
    Index(f"ix_{name}_user_id", table.c.user_id)
    Index(f"ix_{name}_expires_at", table.c.expires_at)
    return table


email_verifications = _one_time_token_table("email_verifications")
password_resets = _one_time_token_table("password_resets")

# --- 팀 --------------------------------------------------------------------

teams = Table(
    "teams",
    metadata,
    _uuid_pk(),
    Column("name", String(80), nullable=False),
    Column("description", String(500), nullable=True),
    _created_at(),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    CheckConstraint("length(btrim(name)) > 0", name="name_not_blank"),
)

team_members = Table(
    "team_members",
    metadata,
    _fk("team_id", "teams.id", ondelete="CASCADE"),
    _fk("user_id", "users.id", ondelete="CASCADE"),
    Column("role", String(16), nullable=False),
    Column("joined_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    PrimaryKeyConstraint("team_id", "user_id"),
    CheckConstraint(_in("role", TEAM_ROLES), name="role"),
)
# 팀당 OWNER는 최대 1명(인덱스)이고, 최소 1명은 지연 제약 트리거가 보장한다.
Index(
    "uq_team_members_single_owner",
    team_members.c.team_id,
    unique=True,
    postgresql_where=team_members.c.role == "OWNER",
)
Index("ix_team_members_user_id", team_members.c.user_id)

team_invitations = Table(
    "team_invitations",
    metadata,
    _uuid_pk(),
    _fk("team_id", "teams.id", ondelete="CASCADE"),
    Column("email", String(320), nullable=False),
    Column("role", String(16), nullable=False, server_default="MEMBER"),
    Column("token_hash", LargeBinary(32), nullable=False),
    _fk("invited_by_user_id", "users.id", ondelete="SET NULL", nullable=True),
    _created_at(),
    Column("expires_at", DateTime(timezone=True), nullable=False),
    Column("accepted_at", DateTime(timezone=True), nullable=True),
    Column("revoked_at", DateTime(timezone=True), nullable=True),
    UniqueConstraint("token_hash"),
    CheckConstraint(_in("role", INVITABLE_TEAM_ROLES), name="role"),
    CheckConstraint("email = lower(email)", name="email_normalized"),
    CheckConstraint("accepted_at IS NULL OR revoked_at IS NULL", name="accepted_or_revoked"),
)
Index(
    "uq_team_invitations_open_email",
    team_invitations.c.team_id,
    team_invitations.c.email,
    unique=True,
    postgresql_where=text("accepted_at IS NULL AND revoked_at IS NULL"),
)

# --- 장치 ------------------------------------------------------------------

devices = Table(
    "devices",
    metadata,
    _uuid_pk(),
    _fk("owner_user_id", "users.id", ondelete="CASCADE"),
    Column("name", String(80), nullable=False),
    Column("platform", String(16), nullable=False, server_default="unknown"),
    Column("collector_version", String(64), nullable=True),
    _created_at(),
    Column("last_seen_at", DateTime(timezone=True), nullable=True),
    Column("revoked_at", DateTime(timezone=True), nullable=True),
    CheckConstraint(_in("platform", DEVICE_PLATFORMS), name="platform"),
    CheckConstraint("length(btrim(name)) > 0", name="name_not_blank"),
)
Index(
    "uq_devices_owner_active_name",
    devices.c.owner_user_id,
    func.lower(devices.c.name),
    unique=True,
    postgresql_where=devices.c.revoked_at.is_(None),
)

device_registration_codes = Table(
    "device_registration_codes",
    metadata,
    _uuid_pk(),
    _fk("user_id", "users.id", ondelete="CASCADE"),
    # 사용자가 웹에서 받아 Collector에 입력하는 단기 일회용 코드. 해시만 저장한다.
    Column("code_hash", LargeBinary(32), nullable=False),
    _created_at(),
    Column("expires_at", DateTime(timezone=True), nullable=False),
    Column("consumed_at", DateTime(timezone=True), nullable=True),
    _fk("device_id", "devices.id", ondelete="SET NULL", nullable=True),
    UniqueConstraint("code_hash"),
    CheckConstraint("expires_at > created_at", name="expiry_after_creation"),
    CheckConstraint(
        "consumed_at IS NULL OR consumed_at >= created_at", name="consumed_after_creation"
    ),
)
Index("ix_device_registration_codes_user_id", device_registration_codes.c.user_id)
Index("ix_device_registration_codes_expires_at", device_registration_codes.c.expires_at)

device_credentials = Table(
    "device_credentials",
    metadata,
    _uuid_pk(),
    _fk("device_id", "devices.id", ondelete="CASCADE"),
    Column("token_hash", LargeBinary(32), nullable=False),
    _fk("rotated_from_id", "device_credentials.id", ondelete="SET NULL", nullable=True),
    _created_at(),
    Column("expires_at", DateTime(timezone=True), nullable=True),
    Column("revoked_at", DateTime(timezone=True), nullable=True),
    UniqueConstraint("token_hash"),
)
Index(
    "ix_device_credentials_device_id_active",
    device_credentials.c.device_id,
    postgresql_where=device_credentials.c.revoked_at.is_(None),
)

device_heartbeats = Table(
    "device_heartbeats",
    metadata,
    Column("id", BigInteger, primary_key=True, autoincrement=True),
    _fk("device_id", "devices.id", ondelete="CASCADE"),
    Column("received_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    Column("collector_version", String(64), nullable=True),
    Column("queue_depth", Integer, nullable=True),
    CheckConstraint("queue_depth IS NULL OR queue_depth >= 0", name="queue_depth_non_negative"),
)
Index(
    "ix_device_heartbeats_device_id_received_at",
    device_heartbeats.c.device_id,
    device_heartbeats.c.received_at,
)
Index("ix_device_heartbeats_received_at", device_heartbeats.c.received_at)

# --- 프로젝트 ----------------------------------------------------------------

projects = Table(
    "projects",
    metadata,
    _uuid_pk(),
    _fk("owner_user_id", "users.id", ondelete="CASCADE"),
    Column("display_name", String(120), nullable=False),
    _created_at(),
    CheckConstraint("length(btrim(display_name)) > 0", name="display_name_not_blank"),
)
Index("ix_projects_owner_user_id", projects.c.owner_user_id)

project_sources = Table(
    "project_sources",
    metadata,
    _uuid_pk(),
    _fk("project_id", "projects.id", ondelete="CASCADE"),
    _fk("device_id", "devices.id", ondelete="CASCADE"),
    # Collector가 만든 장치별 HMAC 지문. 경로·원격 저장소 URL 원문은 저장하지 않는다.
    Column("source_fingerprint", LargeBinary(32), nullable=False),
    _created_at(),
    UniqueConstraint("device_id", "source_fingerprint"),
)
Index("ix_project_sources_project_id", project_sources.c.project_id)

project_shares = Table(
    "project_shares",
    metadata,
    _fk("project_id", "projects.id", ondelete="CASCADE"),
    _fk("team_id", "teams.id", ondelete="CASCADE"),
    _fk("shared_by_user_id", "users.id", ondelete="SET NULL", nullable=True),
    _created_at(),
    PrimaryKeyConstraint("project_id", "team_id"),
)
Index("ix_project_shares_team_id", project_shares.c.team_id)

# --- 세션·에이전트·이벤트 -------------------------------------------------------

agent_sessions = Table(
    "agent_sessions",
    metadata,
    _uuid_pk(),
    _fk("device_id", "devices.id", ondelete="CASCADE"),
    _fk("project_id", "projects.id", ondelete="SET NULL", nullable=True),
    Column("provider", String(16), nullable=False),
    # 이벤트 v1의 session_id. 장치 범위에서만 고유하므로 서버 PK와 분리한다.
    Column("collector_session_id", UUID(as_uuid=True), nullable=False),
    Column("lifecycle", String(16), nullable=False, server_default="DISCOVERED"),
    Column("started_at", DateTime(timezone=True), nullable=True),
    Column("ended_at", DateTime(timezone=True), nullable=True),
    Column("last_event_at", DateTime(timezone=True), nullable=True),
    _created_at(),
    UniqueConstraint("device_id", "provider", "collector_session_id"),
    # 이벤트가 세션과 같은 장치에서 왔는지 복합 FK로 강제하기 위한 키.
    UniqueConstraint("id", "device_id"),
    CheckConstraint(_in("provider", PROVIDERS), name="provider"),
    CheckConstraint(_in("lifecycle", SESSION_LIFECYCLES), name="lifecycle"),
    CheckConstraint("ended_at IS NULL OR lifecycle = 'ENDED'", name="ended_at_requires_ended"),
)
Index("ix_agent_sessions_project_id", agent_sessions.c.project_id)
Index(
    "ix_agent_sessions_ended_at",
    agent_sessions.c.ended_at,
    postgresql_where=agent_sessions.c.ended_at.is_not(None),
)

agents = Table(
    "agents",
    metadata,
    _uuid_pk(),
    Column("session_id", UUID(as_uuid=True), nullable=False),
    Column("parent_agent_id", UUID(as_uuid=True), nullable=True),
    # 이벤트 v1의 agent_id(장치 범위).
    Column("collector_agent_id", UUID(as_uuid=True), nullable=False),
    Column("role", String(16), nullable=False),
    Column("started_at", DateTime(timezone=True), nullable=True),
    Column("ended_at", DateTime(timezone=True), nullable=True),
    _created_at(),
    ForeignKeyConstraint(["session_id"], ["agent_sessions.id"], ondelete="CASCADE"),
    # 부모는 반드시 같은 세션의 에이전트다.
    ForeignKeyConstraint(
        ["session_id", "parent_agent_id"],
        ["agents.session_id", "agents.id"],
        ondelete="CASCADE",
        name="fk_agents_parent_same_session",
    ),
    UniqueConstraint("session_id", "id"),
    UniqueConstraint("session_id", "collector_agent_id"),
    CheckConstraint(_in("role", AGENT_ROLES), name="role"),
    CheckConstraint("parent_agent_id IS NULL OR parent_agent_id <> id", name="not_own_parent"),
    CheckConstraint("(role = 'lead') = (parent_agent_id IS NULL)", name="lead_has_no_parent"),
)
Index("ix_agents_parent_agent_id", agents.c.parent_agent_id)

activity_events = Table(
    "activity_events",
    metadata,
    # 멱등 키. 같은 event_id 재전송은 저장되지 않는다.
    Column("event_id", UUID(as_uuid=True), primary_key=True),
    _fk("device_id", "devices.id", ondelete="CASCADE"),
    Column("session_id", UUID(as_uuid=True), nullable=False),
    Column("agent_id", UUID(as_uuid=True), nullable=True),
    Column("schema_version", String(8), nullable=False),
    Column("provider", String(16), nullable=False),
    Column("source", String(16), nullable=False),
    Column("event_type", String(32), nullable=False),
    Column("activity", String(16), nullable=False),
    Column("confidence", String(16), nullable=False),
    Column("occurred_at", DateTime(timezone=True), nullable=False),
    Column("sequence", BigInteger, nullable=False),
    Column("received_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    # 허용 필드 정규 직렬화의 SHA-256. 같은 event_id의 내용 충돌을 감지한다.
    Column("payload_digest", LargeBinary(32), nullable=False),
    CheckConstraint(_in("provider", PROVIDERS), name="provider"),
    CheckConstraint(_in("source", EVENT_SOURCES), name="source"),
    CheckConstraint(_in("event_type", EVENT_TYPES), name="event_type"),
    CheckConstraint(_in("activity", ACTIVITIES), name="activity"),
    CheckConstraint(_in("confidence", CONFIDENCES), name="confidence"),
    CheckConstraint("sequence >= 0", name="sequence_non_negative"),
    CheckConstraint(
        "source <> 'inferred' OR confidence = 'inferred'", name="inferred_source_is_inferred"
    ),
    # 세션은 이벤트를 보낸 장치의 것이어야 하고, 에이전트는 같은 세션의 것이어야 한다.
    ForeignKeyConstraint(
        ["session_id", "device_id"],
        ["agent_sessions.id", "agent_sessions.device_id"],
        ondelete="CASCADE",
        name="fk_activity_events_session_same_device",
    ),
    ForeignKeyConstraint(
        ["session_id", "agent_id"],
        ["agents.session_id", "agents.id"],
        ondelete="CASCADE",
        name="fk_activity_events_agent_same_session",
    ),
)
Index(
    "ix_activity_events_session_id_occurred_at",
    activity_events.c.session_id,
    activity_events.c.occurred_at,
)
Index(
    "ix_activity_events_device_id_sequence", activity_events.c.device_id, activity_events.c.sequence
)
# 30일 보존 삭제는 서버가 정한 수신 시각을 기준으로 한다(클라이언트 시각은 위조 가능).
Index("ix_activity_events_received_at", activity_events.c.received_at)

agent_current_states = Table(
    "agent_current_states",
    metadata,
    Column(
        "agent_id",
        UUID(as_uuid=True),
        ForeignKey("agents.id", ondelete="CASCADE"),
        primary_key=True,
    ),
    Column("turn_state", String(16), nullable=False, server_default="UNKNOWN"),
    Column("activity", String(16), nullable=False, server_default="unknown"),
    # 늦게 도착한 과거 이벤트가 현재 상태를 되돌리지 않도록 비교하는 기준.
    Column("state_version", BigInteger, nullable=False, server_default="0"),
    Column("last_event_occurred_at", DateTime(timezone=True), nullable=True),
    Column("last_event_sequence", BigInteger, nullable=True),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    CheckConstraint(_in("turn_state", TURN_STATES), name="turn_state"),
    CheckConstraint(_in("activity", ACTIVITIES), name="activity"),
    CheckConstraint("state_version >= 0", name="state_version_non_negative"),
)

plan_steps = Table(
    "plan_steps",
    metadata,
    _uuid_pk(),
    _fk("session_id", "agent_sessions.id", ondelete="CASCADE"),
    Column("agent_id", UUID(as_uuid=True), nullable=True),
    # 계획 원문은 저장하지 않는다. 순서와 상태만 남긴다.
    Column("step_index", Integer, nullable=False),
    Column("status", String(16), nullable=False),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    UniqueConstraint("session_id", "step_index"),
    ForeignKeyConstraint(
        ["session_id", "agent_id"],
        ["agents.session_id", "agents.id"],
        ondelete="CASCADE",
        name="fk_plan_steps_agent_same_session",
    ),
    CheckConstraint("step_index >= 0", name="step_index_non_negative"),
    CheckConstraint(_in("status", PLAN_STEP_STATUSES), name="status"),
)

event_cursors = Table(
    "event_cursors",
    metadata,
    _fk("device_id", "devices.id", ondelete="CASCADE"),
    Column("stream_key", String(64), nullable=False),
    Column("last_sequence", BigInteger, nullable=False),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    PrimaryKeyConstraint("device_id", "stream_key"),
    CheckConstraint("last_sequence >= 0", name="last_sequence_non_negative"),
)

# --- 사무실 ------------------------------------------------------------------

office_layouts = Table(
    "office_layouts",
    metadata,
    _uuid_pk(),
    Column("scope", String(8), nullable=False),
    _fk("owner_user_id", "users.id", ondelete="CASCADE", nullable=True),
    _fk("team_id", "teams.id", ondelete="CASCADE", nullable=True),
    Column("version", Integer, nullable=False, server_default="1"),
    _created_at(),
    CheckConstraint(_in("scope", LAYOUT_SCOPES), name="scope"),
    CheckConstraint(
        "(scope = 'user' AND owner_user_id IS NOT NULL AND team_id IS NULL)"
        " OR (scope = 'team' AND team_id IS NOT NULL AND owner_user_id IS NULL)",
        name="scope_owner",
    ),
    UniqueConstraint("owner_user_id"),
    UniqueConstraint("team_id"),
)

office_slots = Table(
    "office_slots",
    metadata,
    _uuid_pk(),
    _fk("layout_id", "office_layouts.id", ondelete="CASCADE"),
    Column("slot_index", Integer, nullable=False),
    Column("grid_x", Integer, nullable=False),
    Column("grid_y", Integer, nullable=False),
    _fk("session_id", "agent_sessions.id", ondelete="SET NULL", nullable=True),
    Column("assigned_at", DateTime(timezone=True), nullable=True),
    UniqueConstraint("layout_id", "slot_index"),
    UniqueConstraint("layout_id", "grid_x", "grid_y"),
    CheckConstraint("slot_index >= 0", name="slot_index_non_negative"),
    CheckConstraint("(session_id IS NULL) = (assigned_at IS NULL)", name="assignment_pair"),
)
# 한 레이아웃에서 세션은 책상 하나만 차지한다.
Index(
    "uq_office_slots_layout_session",
    office_slots.c.layout_id,
    office_slots.c.session_id,
    unique=True,
    postgresql_where=office_slots.c.session_id.is_not(None),
)

# --- 운영 ------------------------------------------------------------------

instance_settings = Table(
    "instance_settings",
    metadata,
    Column("key", String(64), primary_key=True),
    # 비밀 값은 저장하지 않는다(비밀은 notification_channels.secret_ciphertext 또는 인프라 secret).
    Column("value", JSONB, nullable=False),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    _fk("updated_by_user_id", "users.id", ondelete="SET NULL", nullable=True),
    CheckConstraint("key ~ '^[a-z][a-z0-9_.]{0,63}$'", name="key_format"),
)

notification_channels = Table(
    "notification_channels",
    metadata,
    _uuid_pk(),
    Column("provider", String(32), nullable=False),
    Column("display_name", String(80), nullable=False),
    Column("enabled", Boolean, nullable=False, server_default="false"),
    # 비밀이 아닌 설정만. Webhook URL·토큰·SMTP 암호는 암호화해 secret_ciphertext에 둔다.
    Column("config", JSONB, nullable=False, server_default=text("'{}'::jsonb")),
    Column("secret_ciphertext", LargeBinary, nullable=True),
    Column(
        "subscribed_severities",
        ARRAY(String(16)),
        nullable=False,
        server_default=text("ARRAY['critical','high']::varchar(16)[]"),
    ),
    _created_at(),
    Column("updated_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    CheckConstraint(_in("provider", NOTIFICATION_PROVIDERS), name="provider"),
    CheckConstraint(
        "subscribed_severities <@ ARRAY["
        + ",".join(f"'{s}'" for s in SEVERITIES)
        + "]::varchar(16)[]",
        name="severities",
    ),
)

audit_logs = Table(
    "audit_logs",
    metadata,
    Column("id", BigInteger, primary_key=True, autoincrement=True),
    Column("occurred_at", DateTime(timezone=True), nullable=False, server_default=func.now()),
    # 계정·장치가 삭제돼도 기록은 남기고 행위자 참조만 끊는다.
    _fk("actor_user_id", "users.id", ondelete="SET NULL", nullable=True),
    _fk("actor_device_id", "devices.id", ondelete="SET NULL", nullable=True),
    Column("action", String(64), nullable=False),
    Column("target_type", String(32), nullable=True),
    Column("target_id", UUID(as_uuid=True), nullable=True),
    Column("outcome", String(16), nullable=False),
    # 허용 목록 키만 담는 요약. 원문·비밀·전체 경로를 넣지 않는다.
    Column("details", JSONB, nullable=False, server_default=text("'{}'::jsonb")),
    CheckConstraint("action ~ '^[a-z][a-z0-9_.]{0,63}$'", name="action_format"),
    CheckConstraint(_in("outcome", AUDIT_OUTCOMES), name="outcome"),
)
Index("ix_audit_logs_occurred_at", audit_logs.c.occurred_at)
Index("ix_audit_logs_actor_user_id", audit_logs.c.actor_user_id)
