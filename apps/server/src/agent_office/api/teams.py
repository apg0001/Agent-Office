"""팀·팀 역할·초대·프로젝트 공유(#27).

팀 권한은 팀 역할(OWNER/MANAGER/MEMBER)만으로 판단한다. 인스턴스 역할(SUPER_ADMIN 등)은 팀
권한을 주지 않는다. 남의 팀은 존재 여부도 드러내지 않도록 404로 응답한다.
"""

from __future__ import annotations

import datetime as dt
import secrets
import uuid
from typing import Annotated, Literal

from fastapi import APIRouter, HTTPException, Path, Response, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import Connection, delete, func, or_, select, update
from sqlalchemy.exc import IntegrityError

from agent_office import audit
from agent_office.api.deps import CsrfDep, CurrentUser, DbConnection, UserDep
from agent_office.auth import accounts, mailer
from agent_office.db import schema
from agent_office.security import token_hash

router = APIRouter(prefix="/api/v1", tags=["teams"])

INVITATION_LIFETIME = dt.timedelta(days=7)
MAX_TEAMS_PER_USER = 50
TeamRole = Literal["OWNER", "MANAGER", "MEMBER"]
# 팀원별 공유 범위(#197). 새 구성원의 기본은 'all'이고, 화면은 이 값을 동의 체크로 보여 주고 보낸다.
ShareMode = Literal["all", "selected", "none"]
InvitableRole = Literal["MANAGER", "MEMBER"]
TeamId = Annotated[uuid.UUID, Path()]


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class TeamBody(StrictModel):
    name: str = Field(min_length=1, max_length=80)
    description: str | None = Field(default=None, max_length=500)


class CreateTeamBody(TeamBody):
    share_mode: ShareMode = "all"


class TeamSummary(StrictModel):
    id: uuid.UUID
    name: str
    description: str | None
    role: TeamRole
    # 내가 이 팀에 보여 주는 범위(#197). 다른 구성원의 값은 내보내지 않는다.
    share_mode: ShareMode


class Member(StrictModel):
    user_id: uuid.UUID
    display_name: str
    role: TeamRole
    joined_at: dt.datetime


class TeamDetail(TeamSummary):
    members: list[Member]


class InviteBody(StrictModel):
    email: str = Field(max_length=accounts.MAX_EMAIL_LENGTH)
    role: InvitableRole = "MEMBER"


class Invitation(StrictModel):
    id: uuid.UUID
    email: str
    role: InvitableRole
    expires_at: dt.datetime


class CreatedInvitation(Invitation):
    # 메일 설정이 없어도 초대할 수 있도록 링크를 초대한 사람에게 한 번만 돌려준다.
    invite_url: str
    mailed: bool


class AcceptBody(StrictModel):
    token: str = Field(max_length=256)
    share_mode: ShareMode = "all"


class ShareModeBody(StrictModel):
    share_mode: ShareMode


class RoleBody(StrictModel):
    role: InvitableRole


class TransferBody(StrictModel):
    user_id: uuid.UUID


class ProjectBody(StrictModel):
    display_name: str = Field(min_length=1, max_length=120)


class Project(StrictModel):
    id: uuid.UUID
    display_name: str
    # 'workspace'면 작업 폴더 이름으로 자동 생성한 프로젝트(#197).
    origin: Literal["manual", "workspace"]
    shared_team_ids: list[uuid.UUID]


class SharedProject(StrictModel):
    id: uuid.UUID
    display_name: str
    owner_display_name: str


def _not_found() -> HTTPException:
    return HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")


def _forbidden() -> HTTPException:
    return HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="forbidden")


def _role(connection: Connection, team_id: uuid.UUID, user_id: uuid.UUID) -> str | None:
    members = schema.team_members
    value = connection.execute(
        select(members.c.role).where(members.c.team_id == team_id, members.c.user_id == user_id)
    ).scalar_one_or_none()
    return None if value is None else str(value)


def _require_role(
    connection: Connection, team_id: uuid.UUID, user: CurrentUser, *allowed: str
) -> str:
    role = _role(connection, team_id, user.id)
    if role is None:
        raise _not_found()
    if allowed and role not in allowed:
        raise _forbidden()
    return role


def _share_mode(connection: Connection, team_id: uuid.UUID, user_id: uuid.UUID) -> str:
    members = schema.team_members
    return str(
        connection.execute(
            select(members.c.share_mode).where(
                members.c.team_id == team_id, members.c.user_id == user_id
            )
        ).scalar_one()
    )


def _detail(
    connection: Connection, team_id: uuid.UUID, role: str, user_id: uuid.UUID
) -> TeamDetail:
    teams, members, users = schema.teams, schema.team_members, schema.users
    team = connection.execute(select(teams).where(teams.c.id == team_id)).one()
    rows = connection.execute(
        select(members.c.user_id, users.c.display_name, members.c.role, members.c.joined_at)
        .select_from(members.join(users, users.c.id == members.c.user_id))
        .where(members.c.team_id == team_id)
        .order_by(members.c.joined_at, members.c.user_id)
    ).all()
    return TeamDetail(
        id=team.id,
        name=team.name,
        description=team.description,
        role=role,  # type: ignore[arg-type]
        share_mode=_share_mode(connection, team_id, user_id),  # type: ignore[arg-type]
        members=[
            Member(
                user_id=r.user_id, display_name=r.display_name, role=r.role, joined_at=r.joined_at
            )
            for r in rows
        ],
    )


# --- 팀 ------------------------------------------------------------------------------


@router.post("/teams", status_code=status.HTTP_201_CREATED, response_model=TeamDetail)
def create_team(
    body: CreateTeamBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> TeamDetail:
    members = schema.team_members
    count = connection.execute(
        select(func.count()).select_from(members).where(members.c.user_id == user.id)
    ).scalar_one()
    if count >= MAX_TEAMS_PER_USER:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="too_many_teams")
    team_id = connection.execute(
        schema.teams.insert()
        .values(name=body.name.strip(), description=(body.description or "").strip() or None)
        .returning(schema.teams.c.id)
    ).scalar_one()
    connection.execute(
        members.insert().values(
            team_id=team_id, user_id=user.id, role="OWNER", share_mode=body.share_mode
        )
    )
    audit.record(
        connection,
        "team.created",
        "success",
        actor_user_id=user.id,
        target_type="team",
        target_id=team_id,
        details={"reason": f"share_{body.share_mode}"},
    )
    return _detail(connection, team_id, "OWNER", user.id)


@router.get("/teams", response_model=list[TeamSummary])
def list_teams(user: UserDep, connection: DbConnection) -> list[TeamSummary]:
    teams, members = schema.teams, schema.team_members
    rows = connection.execute(
        select(teams.c.id, teams.c.name, teams.c.description, members.c.role, members.c.share_mode)
        .select_from(teams.join(members, members.c.team_id == teams.c.id))
        .where(members.c.user_id == user.id)
        .order_by(teams.c.name, teams.c.id)
    ).all()
    return [
        TeamSummary(
            id=r.id,
            name=r.name,
            description=r.description,
            role=r.role,
            share_mode=r.share_mode,
        )
        for r in rows
    ]


@router.get("/teams/{team_id}", response_model=TeamDetail)
def get_team(team_id: TeamId, user: UserDep, connection: DbConnection) -> TeamDetail:
    return _detail(connection, team_id, _require_role(connection, team_id, user), user.id)


@router.patch("/teams/{team_id}", response_model=TeamDetail)
def update_team(
    team_id: TeamId, body: TeamBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> TeamDetail:
    role = _require_role(connection, team_id, user, "OWNER", "MANAGER")
    connection.execute(
        update(schema.teams)
        .where(schema.teams.c.id == team_id)
        .values(
            name=body.name.strip(),
            description=(body.description or "").strip() or None,
            updated_at=func.now(),
        )
    )
    return _detail(connection, team_id, role, user.id)


@router.delete("/teams/{team_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_team(
    team_id: TeamId, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> Response:
    _require_role(connection, team_id, user, "OWNER")
    connection.execute(delete(schema.teams).where(schema.teams.c.id == team_id))
    audit.record(
        connection,
        "team.deleted",
        "success",
        actor_user_id=user.id,
        target_type="team",
        target_id=team_id,
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


# --- 구성원 ----------------------------------------------------------------------------


@router.patch("/teams/{team_id}/members/{member_id}", response_model=TeamDetail)
def change_role(
    team_id: TeamId,
    member_id: uuid.UUID,
    body: RoleBody,
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> TeamDetail:
    _require_role(connection, team_id, user, "OWNER")
    target = _role(connection, team_id, member_id)
    if target is None:
        raise _not_found()
    if target == "OWNER":
        # OWNER 변경은 소유권 이전으로만 한다.
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="use_transfer")
    members = schema.team_members
    connection.execute(
        update(members)
        .where(members.c.team_id == team_id, members.c.user_id == member_id)
        .values(role=body.role)
    )
    audit.record(
        connection,
        "team.role_changed",
        "success",
        actor_user_id=user.id,
        target_type="user",
        target_id=member_id,
        details={"reason": body.role},
    )
    return _detail(connection, team_id, "OWNER", user.id)


@router.delete("/teams/{team_id}/members/{member_id}", status_code=status.HTTP_204_NO_CONTENT)
def remove_member(
    team_id: TeamId,
    member_id: uuid.UUID,
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Response:
    """탈퇴(자기 자신) 또는 내보내기. OWNER는 이전 전에는 탈퇴할 수 없다."""
    role = _require_role(connection, team_id, user)
    target = _role(connection, team_id, member_id)
    if target is None:
        raise _not_found()
    if member_id == user.id:
        if role == "OWNER":
            raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="owner_must_transfer")
    elif not (role == "OWNER" or (role == "MANAGER" and target == "MEMBER")):
        raise _forbidden()
    members = schema.team_members
    connection.execute(
        delete(members).where(members.c.team_id == team_id, members.c.user_id == member_id)
    )
    # 팀을 떠난 사람의 공유는 함께 거둔다(팀 사무실에서 즉시 사라짐, #55).
    projects, shares = schema.projects, schema.project_shares
    connection.execute(
        delete(shares).where(
            shares.c.team_id == team_id,
            shares.c.project_id.in_(
                select(projects.c.id).where(projects.c.owner_user_id == member_id)
            ),
        )
    )
    audit.record(
        connection,
        "team.left" if member_id == user.id else "team.member_removed",
        "success",
        actor_user_id=user.id,
        target_type="user",
        target_id=member_id,
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.put("/teams/{team_id}/share-mode", response_model=TeamDetail)
def change_share_mode(
    team_id: TeamId, body: ShareModeBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> TeamDetail:
    """내가 이 팀에 보여 줄 범위를 바꾼다(#197). 본인 것만 바꿀 수 있고, 팀 사무실 스냅샷·실시간·
    상세는 매 요청·매 주기 이 값으로 다시 거르므로 바로 반영된다. 'selected'로 돌아오면 전에
    체크해 둔 프로젝트 공유가 그대로 쓰인다."""
    role = _require_role(connection, team_id, user)
    members = schema.team_members
    previous = _share_mode(connection, team_id, user.id)
    if previous != body.share_mode:
        connection.execute(
            update(members)
            .where(members.c.team_id == team_id, members.c.user_id == user.id)
            .values(share_mode=body.share_mode)
        )
        audit.record(
            connection,
            "team.share_mode_changed",
            "success",
            actor_user_id=user.id,
            target_type="team",
            target_id=team_id,
            details={"reason": f"{previous}_to_{body.share_mode}"},
        )
    return _detail(connection, team_id, role, user.id)


@router.post("/teams/{team_id}/transfer", response_model=TeamDetail)
def transfer_ownership(
    team_id: TeamId, body: TransferBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> TeamDetail:
    _require_role(connection, team_id, user, "OWNER")
    if body.user_id == user.id or _role(connection, team_id, body.user_id) is None:
        raise _not_found()
    members = schema.team_members
    # 단일 OWNER 인덱스 때문에 먼저 강등하고 승격한다. 최소 1명 검사는 커밋 시점이다.
    connection.execute(
        update(members)
        .where(members.c.team_id == team_id, members.c.user_id == user.id)
        .values(role="MANAGER")
    )
    connection.execute(
        update(members)
        .where(members.c.team_id == team_id, members.c.user_id == body.user_id)
        .values(role="OWNER")
    )
    audit.record(
        connection,
        "team.ownership_transferred",
        "success",
        actor_user_id=user.id,
        target_type="user",
        target_id=body.user_id,
    )
    return _detail(connection, team_id, "MANAGER", user.id)


# --- 초대 ------------------------------------------------------------------------------


@router.post(
    "/teams/{team_id}/invitations",
    status_code=status.HTTP_201_CREATED,
    response_model=CreatedInvitation,
)
def invite(
    team_id: TeamId, body: InviteBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> CreatedInvitation:
    role = _require_role(connection, team_id, user, "OWNER", "MANAGER")
    if role == "MANAGER" and body.role != "MEMBER":
        raise _forbidden()
    email = accounts.normalize_email(body.email)
    if email is None:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="invalid_email")
    users, members, invitations = schema.users, schema.team_members, schema.team_invitations
    already = connection.execute(
        select(members.c.user_id)
        .select_from(members.join(users, users.c.id == members.c.user_id))
        .where(members.c.team_id == team_id, users.c.email == email)
    ).first()
    if already:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="already_member")
    # 같은 주소의 열린 초대는 새 초대로 바꾼다.
    connection.execute(
        update(invitations)
        .where(
            invitations.c.team_id == team_id,
            invitations.c.email == email,
            invitations.c.accepted_at.is_(None),
            invitations.c.revoked_at.is_(None),
        )
        .values(revoked_at=func.now())
    )
    token = secrets.token_urlsafe(32)
    row = connection.execute(
        invitations.insert()
        .values(
            team_id=team_id,
            email=email,
            role=body.role,
            token_hash=token_hash(token),
            invited_by_user_id=user.id,
            expires_at=func.now() + INVITATION_LIFETIME,
        )
        .returning(invitations.c.id, invitations.c.expires_at)
    ).one()
    base = mailer.public_url(connection) or ""
    url = f"{base}/invite#token={token}"
    sender = mailer.get_mailer(connection)
    team_name = connection.execute(
        select(schema.teams.c.name).where(schema.teams.c.id == team_id)
    ).scalar_one()
    mailed = sender.configured and sender.send(
        mailer.OutgoingMail(
            to=email,
            subject=f"Agent Office 팀 초대: {team_name}",
            body=(
                f"'{team_name}' 팀에 초대되었습니다. 7일 안에 아래 링크를 열어 수락하세요.\n{url}\n"
                "본인이 요청하지 않았다면 이 메일을 무시하세요."
            ),
        )
    )
    audit.record(
        connection,
        "team.invited",
        "success",
        actor_user_id=user.id,
        target_type="team",
        target_id=team_id,
    )
    return CreatedInvitation(
        id=row.id,
        email=email,
        role=body.role,
        expires_at=row.expires_at,
        invite_url=url,
        mailed=mailed,
    )


@router.get("/teams/{team_id}/invitations", response_model=list[Invitation])
def list_invitations(team_id: TeamId, user: UserDep, connection: DbConnection) -> list[Invitation]:
    _require_role(connection, team_id, user, "OWNER", "MANAGER")
    invitations = schema.team_invitations
    rows = connection.execute(
        select(invitations.c.id, invitations.c.email, invitations.c.role, invitations.c.expires_at)
        .where(
            invitations.c.team_id == team_id,
            invitations.c.accepted_at.is_(None),
            invitations.c.revoked_at.is_(None),
            invitations.c.expires_at > func.now(),
        )
        .order_by(invitations.c.created_at)
    ).all()
    return [Invitation(id=r.id, email=r.email, role=r.role, expires_at=r.expires_at) for r in rows]


@router.delete(
    "/teams/{team_id}/invitations/{invitation_id}", status_code=status.HTTP_204_NO_CONTENT
)
def revoke_invitation(
    team_id: TeamId,
    invitation_id: uuid.UUID,
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Response:
    _require_role(connection, team_id, user, "OWNER", "MANAGER")
    invitations = schema.team_invitations
    result = connection.execute(
        update(invitations)
        .where(
            invitations.c.id == invitation_id,
            invitations.c.team_id == team_id,
            invitations.c.accepted_at.is_(None),
            invitations.c.revoked_at.is_(None),
        )
        .values(revoked_at=func.now())
    )
    if result.rowcount == 0:
        raise _not_found()
    return Response(status_code=status.HTTP_204_NO_CONTENT)


def open_invitation(connection: Connection, token: str, email: str) -> uuid.UUID | None:
    """만료 전 열린 초대 중 이메일이 맞는 것. 가입(#23 초대 전용 정책)도 쓴다."""
    invitations = schema.team_invitations
    if not token or len(token) > 256:
        return None
    value = connection.execute(
        select(invitations.c.id).where(
            invitations.c.token_hash == token_hash(token),
            invitations.c.email == email,
            invitations.c.accepted_at.is_(None),
            invitations.c.revoked_at.is_(None),
            invitations.c.expires_at > func.now(),
        )
    ).scalar_one_or_none()
    return None if value is None else uuid.UUID(str(value))


@router.post("/invitations/accept", response_model=TeamDetail)
def accept_invitation(
    body: AcceptBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> TeamDetail:
    """초대받은 이메일로 로그인한 사용자만 수락할 수 있다(링크를 넘겨받은 다른 계정 차단)."""
    email = connection.execute(
        select(schema.users.c.email).where(schema.users.c.id == user.id)
    ).scalar_one()
    invitation_id = open_invitation(connection, body.token, str(email))
    if invitation_id is None:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="invalid_token")
    invitations = schema.team_invitations
    row = connection.execute(
        update(invitations)
        .where(invitations.c.id == invitation_id, invitations.c.accepted_at.is_(None))
        .values(accepted_at=func.now())
        .returning(invitations.c.team_id, invitations.c.role)
    ).one_or_none()
    if row is None:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="invalid_token")
    try:
        with connection.begin_nested():
            connection.execute(
                schema.team_members.insert().values(
                    team_id=row.team_id,
                    user_id=user.id,
                    role=row.role,
                    share_mode=body.share_mode,
                )
            )
    except IntegrityError:
        pass  # 이미 구성원이면 초대만 소비한다.
    audit.record(
        connection,
        "team.joined",
        "success",
        actor_user_id=user.id,
        target_type="team",
        target_id=row.team_id,
        details={"reason": f"share_{body.share_mode}"},
    )
    role = _role(connection, row.team_id, user.id)
    return _detail(connection, row.team_id, role or row.role, user.id)


# --- 프로젝트 공유 -----------------------------------------------------------------------


def _projects(connection: Connection, user_id: uuid.UUID) -> list[Project]:
    projects, shares = schema.projects, schema.project_shares
    rows = connection.execute(
        select(projects.c.id, projects.c.display_name, projects.c.origin)
        .where(projects.c.owner_user_id == user_id)
        .order_by(projects.c.display_name, projects.c.id)
    ).all()
    shared: dict[uuid.UUID, list[uuid.UUID]] = {}
    if rows:
        for share in connection.execute(
            select(shares.c.project_id, shares.c.team_id).where(
                shares.c.project_id.in_([r.id for r in rows])
            )
        ):
            shared.setdefault(share.project_id, []).append(share.team_id)
    return [
        Project(
            id=r.id,
            display_name=r.display_name,
            origin=r.origin,
            shared_team_ids=sorted(shared.get(r.id, [])),
        )
        for r in rows
    ]


@router.post("/projects", status_code=status.HTTP_201_CREATED, response_model=Project)
def create_project(
    body: ProjectBody, user: UserDep, _csrf: CsrfDep, connection: DbConnection
) -> Project:
    project_id = connection.execute(
        schema.projects.insert()
        .values(owner_user_id=user.id, display_name=body.display_name.strip())
        .returning(schema.projects.c.id)
    ).scalar_one()
    return Project(
        id=project_id, display_name=body.display_name.strip(), origin="manual", shared_team_ids=[]
    )


@router.get("/projects", response_model=list[Project])
def list_projects(user: UserDep, connection: DbConnection) -> list[Project]:
    return _projects(connection, user.id)


def _own_project(connection: Connection, project_id: uuid.UUID, user: CurrentUser) -> None:
    projects = schema.projects
    owned = connection.execute(
        select(projects.c.id).where(
            projects.c.id == project_id, projects.c.owner_user_id == user.id
        )
    ).first()
    if owned is None:
        raise _not_found()


@router.put("/projects/{project_id}/shares/{team_id}", response_model=Project)
def share_project(
    project_id: uuid.UUID,
    team_id: TeamId,
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Project:
    """프로젝트 소유자 본인만, 자신이 속한 팀에만 공유한다(소유자 동의 = 이 요청)."""
    _own_project(connection, project_id, user)
    _require_role(connection, team_id, user)
    statement = schema.project_shares.insert().values(
        project_id=project_id, team_id=team_id, shared_by_user_id=user.id
    )
    try:
        with connection.begin_nested():
            connection.execute(statement)
    except IntegrityError:
        pass  # 이미 공유됨
    audit.record(
        connection,
        "project.shared",
        "success",
        actor_user_id=user.id,
        target_type="team",
        target_id=team_id,
    )
    return next(p for p in _projects(connection, user.id) if p.id == project_id)


@router.delete("/projects/{project_id}/shares/{team_id}", response_model=Project)
def unshare_project(
    project_id: uuid.UUID,
    team_id: TeamId,
    user: UserDep,
    _csrf: CsrfDep,
    connection: DbConnection,
) -> Project:
    _own_project(connection, project_id, user)
    shares = schema.project_shares
    connection.execute(
        delete(shares).where(shares.c.project_id == project_id, shares.c.team_id == team_id)
    )
    audit.record(
        connection,
        "project.unshared",
        "success",
        actor_user_id=user.id,
        target_type="team",
        target_id=team_id,
    )
    return next(p for p in _projects(connection, user.id) if p.id == project_id)


@router.get("/teams/{team_id}/projects", response_model=list[SharedProject])
def team_projects(team_id: TeamId, user: UserDep, connection: DbConnection) -> list[SharedProject]:
    """이 팀에 보이는 프로젝트(#197). 공유 범위가 'all'인 구성원은 모든 프로젝트, 'selected'는
    이 팀에 공유한 프로젝트만, 'none'은 없음. 공유를 거두거나 범위를 바꾸면 바로 사라진다."""
    _require_role(connection, team_id, user)
    projects, shares, users = schema.projects, schema.project_shares, schema.users
    members = schema.team_members
    shared_here = (
        select(shares.c.project_id)
        .where(shares.c.team_id == team_id, shares.c.project_id == projects.c.id)
        .exists()
    )
    rows = connection.execute(
        select(projects.c.id, projects.c.display_name, users.c.display_name.label("owner"))
        .select_from(
            projects.join(users, users.c.id == projects.c.owner_user_id).join(
                members,
                (members.c.team_id == team_id) & (members.c.user_id == projects.c.owner_user_id),
            )
        )
        .where(
            or_(
                members.c.share_mode == "all",
                (members.c.share_mode == "selected") & shared_here,
            )
        )
        .order_by(projects.c.display_name, projects.c.id)
    ).all()
    return [
        SharedProject(id=r.id, display_name=r.display_name, owner_display_name=r.owner)
        for r in rows
    ]
