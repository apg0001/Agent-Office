// Agent Office Collector GUI 프런트엔드(#40, #41). 빌드 도구·외부 의존성 없이 Wails 바인딩만 쓴다.
// 화면에는 Go 백엔드가 고른 코드·개수·한국어 문구만 표시한다. 장치 토큰은 백엔드도 다루지 않는다.
"use strict";

const $ = (id) => document.getElementById(id);

function backend() {
  const app = window.go && window.go.app && window.go.app.App;
  if (!app) {
    throw new Error("wails_binding_unavailable");
  }
  return app;
}

async function call(method, ...args) {
  try {
    return await backend()[method](...args);
  } catch (e) {
    return { ok: false, code: "unexpected", message: "앱 내부 호출에 실패했습니다." };
  }
}

function show(id, result) {
  const el = $(id);
  el.hidden = false;
  el.textContent = result.message || "";
  el.className = "notice " + (result.ok ? "info" : "error");
}

function showOutput(id, text) {
  const el = $(id);
  el.hidden = !text;
  el.textContent = text || "";
}

// --- 페이지 전환 ---------------------------------------------------------------
const loaders = {};
document.querySelectorAll("nav button").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll("nav button").forEach((b) => b.classList.toggle("active", b === btn));
    document.querySelectorAll(".page").forEach((p) => {
      p.hidden = p.id !== "page-" + btn.dataset.page;
    });
    const load = loaders[btn.dataset.page];
    if (load) {
      void load();
    }
  });
});

// --- 계정·서버 --------------------------------------------------------------------
async function loadSettings() {
  const s = await call("Settings");
  if (!s || s.code === "unexpected") {
    return;
  }
  $("server-url").value = s.server_url || "";
  if (!$("reg-name").value) {
    $("reg-name").value = s.device_name || "";
  }
  const missing = $("core-missing");
  missing.hidden = s.core_found;
  missing.textContent = s.core_found
    ? ""
    : "Collector Core(agent-office-collector)를 찾지 못했습니다. GUI와 같은 폴더에 두거나 AGENT_OFFICE_COLLECTOR_BIN을 지정하세요.";
}

$("server-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  show("server-result", await call("SaveServer", $("server-url").value));
});

// --- 기기 등록 --------------------------------------------------------------------
$("open-browser").addEventListener("click", async () => {
  show("register-result", await call("OpenRegistrationPage"));
});

$("register-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const codeInput = $("reg-code");
  const result = await call("Register", codeInput.value, $("reg-name").value, $("reg-force").checked);
  // 일회용 코드는 성공·실패와 관계없이 입력란에서 지운다.
  codeInput.value = "";
  show("register-result", result);
  showOutput("register-output", result.ok ? "" : result.output);
  if (result.ok) {
    $("reg-force").checked = false;
  }
});

// --- 도구 탐지·Hook ---------------------------------------------------------------
const SUPPORT = { verified: "검증됨", partial: "부분 검증", unverified: "미검증" };
const HOOK = {
  installed: "설치됨",
  partial: "일부 누락",
  modified: "변경됨",
  not_installed: "미설치",
  unreadable: "설정 파일을 읽을 수 없음",
  unknown: "알 수 없음",
};

async function loadTools() {
  const view = await call("Tools");
  const body = $("tools-body");
  body.textContent = "";
  if (!view.ok) {
    show("tools-result", { ok: false, message: "도구 탐지 실패(" + (view.code || "unexpected") + ")" });
    return;
  }
  $("tools-result").hidden = true;
  for (const t of view.tools) {
    const tr = document.createElement("tr");
    const name = t.tool === "claude" ? "Claude Code" : "Codex";
    const version = t.detected ? t.version : "감지 안 됨(미설치 또는 PATH에 없음)";
    let support = SUPPORT[t.support] || t.support;
    if (t.verified_mode) {
      support += " (실측 모드 " + t.verified_mode + " 한정)";
    }
    let hook;
    if (t.hook_install === "print_only") {
      hook = "자동 설치 미지원(직접 등록)";
    } else {
      hook = (HOOK[t.hook_state] || t.hook_state) + " " + t.events_current + "/" + t.events_total;
      if (t.foreign > 0) {
        hook += ", 다른 Hook " + t.foreign + "개 보존";
      }
      if (t.error) {
        hook += " (" + t.error + ")";
      }
    }
    for (const text of [name, version, support, hook]) {
      const td = document.createElement("td");
      td.textContent = text;
      tr.appendChild(td);
    }
    body.appendChild(tr);
  }
}
loaders.tools = loadTools;
$("tools-refresh").addEventListener("click", () => void loadTools());

$("hook-preview").addEventListener("click", async () => {
  const r = await call("HookPreview");
  show("hook-result", r);
  showOutput("hook-output", r.output);
  $("hook-install").disabled = !r.ok;
});

$("hook-install").addEventListener("click", async () => {
  const yes = window.confirm("미리보기의 변경을 Claude Code 사용자 설정에 적용합니다. 원본은 백업됩니다. 계속할까요?");
  if (!yes) {
    return;
  }
  const r = await call("HookInstall", true);
  show("hook-result", r);
  showOutput("hook-output", r.output);
  $("hook-install").disabled = true;
  void loadTools();
});

$("hook-uninstall").addEventListener("click", async () => {
  if (!window.confirm("Agent Office가 추가한 Hook만 제거합니다. 계속할까요?")) {
    return;
  }
  const r = await call("HookUninstall", true);
  show("hook-result", r);
  showOutput("hook-output", r.output);
  void loadTools();
});

$("codex-print").addEventListener("click", async () => {
  const r = await call("CodexHookPrint");
  show("hook-result", r);
  showOutput("hook-output", r.output);
});

// --- 자동 시작 ---------------------------------------------------------------------
$("service-preview").addEventListener("click", async () => {
  const r = await call("ServicePreview");
  show("service-result", r);
  showOutput("service-output", r.output);
});

$("service-install").addEventListener("click", async () => {
  const activate = $("service-activate").checked;
  if (!window.confirm(activate ? "자동 시작 정의를 설치하고 바로 활성화합니다. 계속할까요?" : "자동 시작 정의 파일을 설치합니다. 계속할까요?")) {
    return;
  }
  const r = await call("ServiceInstall", activate, true);
  show("service-result", r);
  showOutput("service-output", r.output);
});

// --- 수집 상태 ---------------------------------------------------------------------
const REGISTRATION = { registered: "등록됨", not_registered: "미등록", config_invalid: "설정 파일 손상(다시 등록 필요)", unknown: "알 수 없음" };
const RUN_STATE = {
  starting: "시작 중",
  running: "수집 중",
  paused: "일시중지",
  device_revoked: "장치 폐기됨(다시 등록 필요)",
  not_registered: "등록 대기",
  config_invalid: "설정 손상",
  stopped: "정지됨",
  unknown: "알 수 없음",
};
const SERVICE = { installed: "설치됨", absent: "없음", foreign: "다른 파일이 있음(덮어쓰지 않음)", unsupported: "이 OS 미지원", unknown: "알 수 없음" };

function time(v) {
  return v ? new Date(v).toLocaleString() : "없음";
}

function addRow(dl, label, value) {
  const dt = document.createElement("dt");
  dt.textContent = label;
  const dd = document.createElement("dd");
  dd.textContent = value;
  dl.append(dt, dd);
}

async function loadStatus() {
  const view = await call("Status");
  const dl = $("status-list");
  const errs = $("status-errors");
  const counters = $("status-counters");
  dl.textContent = "";
  errs.textContent = "";
  counters.textContent = "";
  if (!view.ok) {
    addRow(dl, "상태", "읽을 수 없음(" + (view.code || "unexpected") + ")");
    return;
  }
  const r = view.report;
  addRow(dl, "Collector", r.collector_version + " (" + r.os + "/" + r.arch + ")");
  addRow(dl, "등록", REGISTRATION[r.registration] || r.registration);
  if (r.server_scheme) {
    addRow(dl, "서버", r.server_scheme + (r.server_loopback ? ", loopback" : "") + " (주소는 표시하지 않음)");
  }
  addRow(dl, "수집 설정", r.paused ? "일시중지" : "수집 중");
  addRow(dl, "Core 프로세스", r.process_running ? "실행 중" : "실행 중 아님(자동 시작을 설치·활성화하세요)");
  addRow(dl, "자동 시작", SERVICE[r.service] || r.service);
  if (r.run) {
    addRow(dl, "마지막 실행 상태", (RUN_STATE[r.run.state] || r.run.state) + " · 갱신 " + time(r.run.updated_at));
    addRow(dl, "감시", r.run.watch_root + ", 파일 " + r.run.watched_files + "개");
    addRow(dl, "마지막 전송 성공", time(r.run.last_send_ok_at));
    addRow(dl, "마지막 heartbeat", time(r.run.last_heartbeat_ok_at));
    for (const e of r.run.errors || []) {
      const li = document.createElement("li");
      li.textContent = e.code + ": " + e.count + "회, 마지막 " + time(e.last_at);
      errs.appendChild(li);
    }
    for (const [k, v] of Object.entries(r.run.counters || {}).sort()) {
      const li = document.createElement("li");
      li.textContent = k + " = " + v;
      counters.appendChild(li);
    }
  }
  if (r.queue) {
    addRow(dl, "로컬 큐", r.queue.state + ", 대기 " + r.queue.depth + "건 · drop-oldest " + r.queue.dropped_oldest + " · dead-letter " + r.queue.dead_letter);
  }
  if (!errs.childElementCount) {
    const li = document.createElement("li");
    li.textContent = "없음";
    errs.appendChild(li);
  }
}
loaders.status = loadStatus;
$("status-refresh").addEventListener("click", () => void loadStatus());
$("pause").addEventListener("click", async () => {
  show("pause-result", await call("Pause"));
  void loadStatus();
});
$("resume").addEventListener("click", async () => {
  show("pause-result", await call("Resume"));
  void loadStatus();
});
setInterval(() => {
  if (!$("page-status").hidden) {
    void loadStatus();
  }
}, 5000);

// --- 진단 -------------------------------------------------------------------------
$("diagnose-run").addEventListener("click", async () => {
  const view = await call("Diagnose", $("diagnose-offline").checked);
  showOutput("diagnose-output", view.ok ? JSON.stringify(view.report, null, 2) : "진단 실패(" + (view.code || "unexpected") + ")");
});

loaders.account = loadSettings;
loaders.device = loadSettings;
void loadSettings();
