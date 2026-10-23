import { OfficeView } from "./office/OfficeView";

const principles = [
  ["관측 우선", "실제 이벤트와 추정·연출을 분리합니다."],
  ["로컬 원문", "프롬프트와 코드, 로그 원문은 개발 PC에만 둡니다."],
  ["비침습", "Collector 장애가 에이전트 작업을 막지 않습니다."],
];

export function App() {
  return (
    <main className="shell">
      <section className="hero">
        <p className="eyebrow">SELF-HOSTED · MULTI-USER · PRIVACY-FIRST</p>
        <h1>Agent Office</h1>
        <p className="lede">
          코딩 에이전트의 안전한 활동 메타데이터를 하나의 아이소메트릭 사무실로
          연결합니다.
        </p>
        <span className="status">기반 구축 중 · 실제 수집 비활성</span>
      </section>
      <OfficeView />
      <section className="principles" aria-label="프로젝트 원칙">
        {principles.map(([title, description]) => (
          <article key={title}>
            <h2>{title}</h2>
            <p>{description}</p>
          </article>
        ))}
      </section>
    </main>
  );
}
