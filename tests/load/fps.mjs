// 실시간 사무실 화면의 브라우저 FPS를 잰다(#63). Playwright Chromium(헤드리스).
//   node fps.mjs <기준 주소(http://localhost:PORT)> <ao_session 값> <측정 초>
// 출력: {fps_mean, fps_p5, characters, renderer} JSON. 쿠키 값은 출력하지 않는다.
import { chromium } from "playwright";

const [base, cookie, seconds = "15"] = process.argv.slice(2);
const browser = await chromium.launch({ args: ["--use-gl=swiftshader", "--ignore-gpu-blocklist"] });
const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const url = new URL(base);
// localhost는 브라우저가 안전한 출처로 보므로 Secure 쿠키가 평문 HTTP에서도 전달된다.
await context.addCookies([
  { name: "ao_session", value: cookie, domain: url.hostname, path: "/", secure: true, httpOnly: true, sameSite: "Lax" },
]);
const page = await context.newPage();
const errors = [];
page.on("pageerror", (error) => errors.push(String(error).slice(0, 200)));
await page.goto(base + "/", { waitUntil: "networkidle" });
await page.waitForSelector(".office-canvas canvas", { timeout: 30000 });
// 실시간 연결과 캐릭터 출근을 기다린다.
await page.waitForFunction(() => document.querySelector(".live-badge")?.textContent?.includes("실시간 연결됨"), null, { timeout: 30000 });
await page.waitForTimeout(5000);
const result = await page.evaluate(async (ms) => {
  const frames = [];
  let last = performance.now();
  const end = last + ms;
  await new Promise((resolve) => {
    const tick = (now) => {
      frames.push(now - last);
      last = now;
      if (now < end) requestAnimationFrame(tick);
      else resolve();
    };
    requestAnimationFrame(tick);
  });
  const fps = frames.slice(1).map((d) => 1000 / d).sort((a, b) => a - b);
  const mean = (frames.length - 1) / (ms / 1000);
  const canvas = document.querySelector(".office-canvas canvas");
  const gl = canvas?.getContext("webgl2") || canvas?.getContext("webgl");
  return {
    fps_mean: Math.round(mean * 10) / 10,
    fps_p5: Math.round(fps[Math.floor(fps.length * 0.05)] * 10) / 10,
    renderer: gl ? "webgl" : "canvas",
  };
}, Number(seconds) * 1000);
const characters = await page.evaluate(() => {
  const game = window.__aoGame;
  return game ? game.scene.scenes[0]?.children?.list?.length ?? null : null;
});
console.log(JSON.stringify({ ...result, characters_hint: characters, page_errors: errors }));
await browser.close();
