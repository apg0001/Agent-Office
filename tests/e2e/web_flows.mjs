// 실제 브라우저(Playwright Chromium)로 주요 화면 흐름을 확인한다(#63·#62 일부).
//   node web_flows.mjs <기준 주소(http://localhost:PORT)> <bootstrap 토큰>
// 출력: 단계별 결과 JSON. 토큰·쿠키·암호는 출력하지 않는다.
import { chromium } from "playwright";

const [base, token] = process.argv.slice(2);
const browser = await chromium.launch();
const page = await (await browser.newContext({ viewport: { width: 1280, height: 900 } })).newPage();
const errors = [];
page.on("pageerror", (error) => errors.push(String(error).slice(0, 200)));
const steps = [];
const step = async (name, fn) => {
  try {
    await fn();
    steps.push({ name, ok: true });
  } catch (error) {
    steps.push({ name, ok: false, error: String(error).split("\n")[0].slice(0, 200) });
  }
};
const password = "browser e2e passphrase";

await step("설치 마법사: bootstrap으로 최초 관리자", async () => {
  await page.goto(base + "/setup");
  await page.getByLabel("bootstrap 토큰").fill(token);
  await page.getByLabel("이름").fill("E2E 관리자");
  await page.getByLabel("이메일").fill("admin@example.com");
  await page.getByLabel(/비밀번호/).fill(password);
  await page.getByRole("button", { name: "관리자 만들기" }).click();
  await page.getByRole("heading", { name: "2. 사이트" }).waitFor({ timeout: 15000 });
});
await step("설치 마법사: 사이트·가입 정책·메일 건너뛰기·완료", async () => {
  await page.getByRole("button", { name: "다음" }).click();
  await page.getByRole("heading", { name: "3. 가입 정책" }).waitFor();
  await page.getByLabel("초대한 사용자만").check();
  await page.getByRole("button", { name: "다음" }).click();
  await page.getByRole("heading", { name: /4\. 메일/ }).waitFor();
  await page.getByRole("button", { name: "건너뛰기" }).click();
  await page.getByRole("button", { name: "설치 완료" }).click();
  await page.waitForURL(base + "/", { timeout: 15000 });
});
await step("사무실: 실시간 연결·로그인 표시", async () => {
  await page.getByText("실시간 연결됨").waitFor({ timeout: 20000 });
  await page.getByText("E2E 관리자").waitFor();
  await page.locator(".office-canvas canvas").waitFor();
});
await step("관리 화면: 진단·보안 점검·사용자", async () => {
  await page.goto(base + "/admin");
  await page.getByRole("heading", { name: "상태·진단" }).waitFor({ timeout: 15000 });
  await page.getByRole("heading", { name: "보안 점검" }).waitFor();
  await page.getByText("admin@example.com").waitFor();
});
await step("팀 화면: 팀 만들기·초대 링크", async () => {
  await page.goto(base + "/teams");
  await page.getByLabel("새 팀 이름").fill("E2E 팀");
  await page.getByRole("button", { name: "팀 만들기" }).click();
  await page.getByRole("heading", { name: "E2E 팀" }).waitFor({ timeout: 15000 });
  await page.getByLabel("초대할 이메일").fill("friend@example.com");
  await page.getByRole("button", { name: "초대" }).click();
  await page.getByText("/invite#token=").waitFor({ timeout: 10000 });
});
await step("로그아웃 후 로그인", async () => {
  await page.goto(base + "/");
  await page.getByRole("button", { name: "로그아웃" }).click();
  await page.waitForURL(base + "/login");
  await page.getByLabel("이메일").fill("admin@example.com");
  await page.getByLabel("비밀번호").fill(password);
  await page.getByRole("button", { name: "로그인" }).click();
  await page.waitForURL(base + "/", { timeout: 15000 });
  await page.getByText("실시간 연결됨").waitFor({ timeout: 20000 });
});
await step("잘못된 비밀번호는 한국어 오류", async () => {
  await page.getByRole("button", { name: "로그아웃" }).click();
  await page.waitForURL(base + "/login");
  await page.getByLabel("이메일").fill("admin@example.com");
  await page.getByLabel("비밀번호").fill("wrong password!!");
  await page.getByRole("button", { name: "로그인" }).click();
  await page.getByRole("alert").getByText("이메일 또는 비밀번호가 맞지 않습니다.").waitFor({ timeout: 10000 });
});

console.log(JSON.stringify({ steps, page_errors: errors }, null, 1));
await browser.close();
process.exit(steps.every((s) => s.ok) && errors.length === 0 ? 0 : 1);
