import { OfficeView, ScopePicker, useTeams } from "./OfficeView";

/** 나란히 보기에 한 번에 띄우는 팀 수. 화면마다 Phaser와 실시간 연결이 하나씩 생긴다. */
export const MAX_SIDE_TEAMS = 3;

/** 개인 사무실과 팀 사무실들을 나란히(#53). 각 화면은 따로 연결하고 같은 세션이 여러 화면에 보일 수 있다. */
export function SideBySide() {
  const teams = useTeams();
  const shown = teams.slice(0, MAX_SIDE_TEAMS);
  return (
    <section className="side-by-side" aria-label="나란히 보기">
      <ScopePicker scope={{ kind: "side" }} />
      {teams.length > MAX_SIDE_TEAMS && (
        <p className="hint">팀이 많아 처음 {MAX_SIDE_TEAMS}개만 보입니다. 나머지는 팀 사무실로 따로 여세요.</p>
      )}
      <div className="side-grid">
        <OfficeView scope={{ kind: "personal" }} compact title="내 사무실" />
        {shown.map((team) => (
          <OfficeView key={team.id} scope={{ kind: "team", teamId: team.id }} compact title={`팀: ${team.name}`} />
        ))}
      </div>
    </section>
  );
}
