import { ZONE_COLORS, ZONE_GUIDE, ZONE_LABELS } from "./grid";

const hex = (color: number) => `#${color.toString(16).padStart(6, "0")}`;

const MARKS: [string, string][] = [
  ["왕관", "세션의 대장 에이전트"],
  ["작은 캐릭터", "하위 에이전트(대장 옆에 붙어 다님)"],
  ["옷 색", "PC마다 다름. 이름표에 PC 이름"],
  ["머리 위 !", "사용자 승인을 기다리는 중"],
  ["흐린 캐릭터 + 배지", "PC 연결이 끊겼거나 마지막 활동 이후 소식이 없음"],
];

/** 사무실 그래픽 아래 범례(#160). 구역 색은 지도와 같은 값을 쓴다. */
export function ZoneLegend() {
  return (
    <section className="zone-legend" aria-label="사무실 안내">
      <h2>각 구역의 뜻</h2>
      <ul className="zone-list">
        {ZONE_GUIDE.map(({ zone, meaning }) => (
          <li key={zone}>
            <span className="swatch" style={{ background: hex(ZONE_COLORS[zone]) }} aria-hidden="true" />
            <strong>{ZONE_LABELS[zone]}</strong>
            <span>{meaning}</span>
          </li>
        ))}
      </ul>
      <h2>캐릭터 표시</h2>
      <ul className="mark-list">
        {MARKS.map(([mark, meaning]) => (
          <li key={mark}>
            <strong>{mark}</strong> <span>{meaning}</span>
          </li>
        ))}
      </ul>
      <p className="legend-note">
        캐릭터를 누르면 지금 상태와 최근 활동을 볼 수 있습니다. 확실하지 않은 상태는 상세 화면에 "(추정)"으로
        표시합니다.
      </p>
    </section>
  );
}
