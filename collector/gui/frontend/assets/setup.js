// 처음 설정 마법사·업데이트 화면(#180). app.js 다음에 읽는다(call·show·openPage·loaders 등을 같이 쓴다).
// 화면에는 Go 백엔드가 고른 코드·버전·한국어 문구만 표시한다.
"use strict";

let wizStep = 1;
function wizardGo(step) {
  wizStep = step;
  document.querySelectorAll(".wizard-step").forEach((el) => {
    el.hidden = Number(el.dataset.step) !== step;
  });
  document.querySelectorAll("#wizard-progress li").forEach((li) => {
    const n = Number(li.dataset.step);
    li.classList.toggle("current", n === step);
    li.classList.toggle("done", n < step);
  });
  if (step === 2) {
    void wizardStep2();
  } else if (step === 3) {
    void wizardStep3();
  } else if (step === 4) {
    void wizardCheck();
  }
}

async function wizardStart() {
  const s = await call("Settings");
  if (s && s.server_url) {
    $("wiz-server-url").value = s.server_url;
  }
  if (s && s.device_name && !$("wiz-reg-name").value) {
    $("wiz-reg-name").value = s.device_name;
  }
  const st = await call("SetupStatus");
  if (st && st.registered) {
    // 명령행 설치에서 옮겨 온 PC: 등록·장치 토큰·큐를 그대로 쓰므로 연동 승인부터.
    wizardGo(st.server_set ? 3 : 1);
  } else {
    wizardGo(st && st.server_set ? 2 : 1);
  }
}
loaders.setup = wizardStart;

$("wiz-server-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const r = await call("SaveServer", $("wiz-server-url").value);
  if (r.ok) {
    $("server-url").value = $("wiz-server-url").value;
    wizardGo(2);
  } else {
    await tell(r.message);
  }
});

async function wizardStep2() {
  const st = await call("SetupStatus");
  const reg = Boolean(st && st.registered);
  $("wiz-registered").hidden = !reg;
  $("wiz-register-box").hidden = reg;
}

$("wiz-open-browser").addEventListener("click", async () => {
  show("wiz-register-result", await call("OpenRegistrationPage"));
});

$("wiz-register-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const code = $("wiz-reg-code");
  const r = await call("Register", code.value, $("wiz-reg-name").value, false);
  // 일회용 코드는 성공·실패와 관계없이 입력란에서 지운다.
  code.value = "";
  show("wiz-register-result", r);
  if (r.ok) {
    wizardGo(3);
  }
});
$("wiz-to-3").addEventListener("click", () => wizardGo(3));

async function wizardStep3() {
  const box = $("wiz-tools");
  box.textContent = "도구를 찾는 중…";
  const view = await call("Tools");
  box.textContent = "";
  if (!view.ok) {
    box.textContent = "도구 탐지 실패(" + (view.code || "unexpected") + ")";
    return;
  }
  const tools = view.tools.filter((t) => t.hook_install === "auto");
  // 미리보기는 파일을 바꾸지 않는다. 백엔드는 미리보기를 만든 도구만 설치를 허용한다.
  const previews = (await call("SetupPreview", tools.map((t) => t.tool))) || [];
  for (const t of tools) {
    const name = TOOL_NAMES[t.tool] || t.tool;
    const div = document.createElement("div");
    div.className = "wizard-tool";
    const label = document.createElement("label");
    label.className = "check";
    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.dataset.tool = t.tool;
    cb.checked = t.detected;
    label.append(cb, " " + name + " Hook 설치 " + (t.detected ? "(" + t.version + " 감지됨)" : "(감지 안 됨)"));
    div.appendChild(label);
    const p = previews.find((x) => x.tool === t.tool);
    const details = document.createElement("details");
    const summary = document.createElement("summary");
    summary.textContent = "변경 미리보기";
    const pre = document.createElement("pre");
    pre.textContent = p ? p.result.output || p.result.message : "미리보기 없음";
    if (!p || !p.result.ok) {
      cb.checked = false;
      cb.disabled = true;
    }
    details.append(summary, pre);
    div.appendChild(details);
    if (t.tool === "codex") {
      const hint = document.createElement("span");
      hint.className = "hint";
      hint.textContent = '설치 뒤 Codex를 열어 "Hooks need review"에서 신뢰해야 동작합니다.';
      div.appendChild(hint);
    }
    box.appendChild(div);
  }
}

$("wiz-apply").addEventListener("click", async () => {
  const tools = Array.from(document.querySelectorAll("#wiz-tools input[type=checkbox]"))
    .filter((cb) => cb.checked && !cb.disabled)
    .map((cb) => cb.dataset.tool);
  const choices = { tools, service: $("wiz-service").checked, tray_autostart: $("wiz-tray").checked };
  const parts = tools.map((t) => (TOOL_NAMES[t] || t) + " Hook");
  if (choices.service) parts.push("Core 자동 시작");
  if (choices.tray_autostart) parts.push("트레이 자동 시작");
  if (!parts.length) {
    wizardGo(4);
    return;
  }
  if (!await ask(parts.join(", ") + "을(를) 설치합니다. 원본 설정은 백업합니다. 계속할까요?")) {
    return;
  }
  const v = await call("SetupApply", choices, true);
  const lines = [];
  for (const h of v.hooks || []) {
    lines.push((TOOL_NAMES[h.tool] || h.tool) + ": " + h.result.message);
  }
  if (v.service) lines.push("Core 자동 시작: " + v.service.message);
  if (v.tray) lines.push("트레이 자동 시작: " + v.tray.message);
  show("wiz-apply-result", {
    ok: v.ok,
    message: v.ok ? "승인한 항목을 설치했습니다." : "일부 항목을 설치하지 못했습니다. 아래 내용을 확인하세요.",
  });
  showOutput("wiz-apply-output", lines.join("\n"));
  $("wiz-codex-trust").hidden = !v.codex_trust;
  if (v.ok) {
    setTimeout(() => wizardGo(4), v.codex_trust ? 4000 : 800);
  }
});

async function wizardCheck() {
  const dl = $("wiz-check");
  dl.textContent = "";
  const view = await call("Status");
  if (!view.ok) {
    addRow(dl, "Core", "응답 없음(" + (view.code || "unexpected") + ")");
    return;
  }
  const r = view.report;
  addRow(dl, "등록", REGISTRATION[r.registration] || r.registration);
  addRow(dl, "Core 프로세스", r.process_running ? "실행 중" : "실행 중 아님");
  addRow(dl, "자동 시작", SERVICE[r.service] || r.service);
  const diag = await call("Diagnose", false);
  const server = diag.ok && diag.report ? diag.report.server : "unknown";
  addRow(dl, "서버 도달", server === "reachable" ? "됨" : server || "알 수 없음");
  if (r.run) {
    addRow(dl, "마지막 heartbeat", time(r.run.last_heartbeat_ok_at));
  }
  const ok = r.registration === "registered" && r.process_running && server === "reachable";
  show("wiz-check-result", {
    ok,
    message: ok
      ? "연결됐습니다. 창을 닫아도 수집은 계속됩니다."
      : "아직 연결되지 않았습니다. 몇 초 뒤 '다시 확인'을 누르세요(Core가 시작 중일 수 있음).",
  });
}
$("wiz-recheck").addEventListener("click", () => void wizardCheck());
$("wiz-done").addEventListener("click", async () => {
  await call("SetupFinish", false);
  openPage("status");
});
$("wiz-later").addEventListener("click", async () => {
  await call("SetupFinish", true);
  openPage("account");
});

// --- 업데이트 ----------------------------------------------------------------------
async function loadUpdate() {
  const v = await call("UpdatePrefs");
  document.querySelectorAll("input[name=update-mode]").forEach((r) => {
    r.checked = r.value === (v.mode || "notify");
  });
  $("update-channel").value = v.channel || "";
  const l = await call("LegacyStatus");
  const list = $("legacy-list");
  list.textContent = "";
  const files = (l && l.files) || [];
  for (const f of files) {
    const li = document.createElement("li");
    li.textContent = f.location + "/" + f.name + (f.state ? " — " + f.state : "");
    list.appendChild(li);
  }
  if (!files.length) {
    const li = document.createElement("li");
    li.textContent = "없음";
    list.appendChild(li);
  }
  $("legacy-remove").disabled = !(l && l.eligible && files.length);
}
loaders.update = loadUpdate;

$("update-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const checked = document.querySelector("input[name=update-mode]:checked");
  show("update-prefs-result", await call("SaveUpdatePrefs", checked ? checked.value : "notify", $("update-channel").value));
});

$("update-check").addEventListener("click", async () => {
  const v = await call("InstallerCheck");
  const msg = v.status === "available" ? "새 버전 v" + v.version + "이 있습니다. " + v.message : v.message;
  show("update-check-result", { ok: v.ok, message: msg });
});

$("legacy-remove").addEventListener("click", async () => {
  if (!await ask("목록의 이전 명령행 설치 파일만 지웁니다. 등록·장치 토큰·큐는 그대로입니다. 계속할까요?")) {
    return;
  }
  const r = await call("LegacyRemove", true);
  let message = "정리하지 못했습니다(" + (r.code || "unexpected") + ").";
  if (r.ok) {
    message = r.removed + "개를 지웠습니다" + (r.failed ? ", " + r.failed + "개는 지우지 못했습니다(사용 중이거나 아직 참조됨)." : ".");
  }
  show("legacy-result", { ok: r.ok && !r.failed, message });
  void loadUpdate();
});

// 처음 설정을 아직 끝내거나 미루지 않았으면 마법사부터 연다(설치 직후 첫 실행).
void (async () => {
  const st = await call("SetupStatus");
  if (st && st.show) {
    openPage("setup");
  }
})();
