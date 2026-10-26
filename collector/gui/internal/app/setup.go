package app

import "github.com/apg0001/Agent-Office/collector/gui/internal/prefs"

// 첫 실행 마법사(#180): 서버 주소 → 브라우저 로그인·PC 등록 → Claude·Codex 탐지 → 한 화면 동의(Hook·Core 자동
// 시작·트레이 자동 시작) → 연결 확인. 각 단계는 기존 동작(SaveServer, Register, Tools, HookPreview·HookInstall,
// ServiceInstall, GUIAutostartInstall, Status)을 그대로 쓰고, 동의 화면만 여러 동작을 한 번의 승인으로 묶는다.

// SetupView는 마법사가 어디서 시작할지 정하는 상태다.
type SetupView struct {
	// State는 prefs 마법사 상태(""·done·dismissed)다.
	State      string `json:"state"`
	ServerSet  bool   `json:"server_set"`
	Registered bool   `json:"registered"`
	// Show는 창을 열 때 마법사를 먼저 보여 줄지다(아직 끝내거나 미루지 않았음).
	Show bool `json:"show"`
}

// SetupStatus는 마법사 상태다.
func (a *App) SetupStatus() SetupView {
	p, _, _ := a.loadPrefs()
	v := SetupView{State: p.Setup, ServerSet: p.ServerURL != ""}
	if st := a.Status(); st.OK && st.Report != nil {
		v.Registered = st.Report.Registration == "registered"
	}
	v.Show = p.Setup == ""
	return v
}

// SetupFinish는 마법사를 끝냈거나(done) 나중에 하기로(dismissed) 했음을 기록한다.
func (a *App) SetupFinish(dismissed bool) Result {
	p, path, err := a.loadPrefs()
	if err != nil {
		return fail("prefs_unavailable")
	}
	p.Setup = "done"
	if dismissed {
		p.Setup = "dismissed"
	}
	if err := prefs.Save(path, p); err != nil {
		return fail("prefs_write_failed")
	}
	return okResult("setup_saved", "")
}

// SetupPreview는 동의 화면에 보여 줄 도구별 Hook 설치 미리보기다(파일을 바꾸지 않음). 미리보기를 만든 도구만
// SetupApply에서 설치할 수 있다.
func (a *App) SetupPreview(tools []string) []ToolResult {
	var out []ToolResult
	for _, t := range tools {
		out = append(out, ToolResult{Tool: t, Result: a.HookPreview(t)})
	}
	return out
}

// ToolResult는 도구 하나의 결과다.
type ToolResult struct {
	Tool   string `json:"tool"`
	Result Result `json:"result"`
}

// SetupChoices는 동의 화면에서 고른 항목이다(기본: 탐지된 도구 Hook, Core 자동 시작, 트레이 자동 시작 모두 켬).
type SetupChoices struct {
	Tools         []string `json:"tools"`
	Service       bool     `json:"service"`
	TrayAutostart bool     `json:"tray_autostart"`
}

// SetupApplyView는 동의 뒤 실행 결과다.
type SetupApplyView struct {
	OK    bool         `json:"ok"`
	Hooks []ToolResult `json:"hooks"`
	// Service·Tray는 고르지 않았으면 nil이다.
	Service *Result `json:"service,omitempty"`
	Tray    *Result `json:"tray,omitempty"`
	// CodexTrust는 Codex Hook을 설치해 Codex에서 "Hooks need review"를 신뢰해야 하는지다.
	CodexTrust bool `json:"codex_trust"`
}

// SetupApply는 한 번의 승인으로 고른 항목을 설치한다. 각 Hook은 이번 실행에서 미리보기를 본 도구만 설치한다.
func (a *App) SetupApply(c SetupChoices, confirmed bool) SetupApplyView {
	v := SetupApplyView{OK: true}
	if !confirmed {
		r := fail("confirmation_required")
		return SetupApplyView{Service: &r}
	}
	for _, t := range c.Tools {
		r := a.HookInstall(t, true)
		v.Hooks = append(v.Hooks, ToolResult{Tool: t, Result: r})
		v.OK = v.OK && r.OK
		if t == "codex" && r.OK {
			v.CodexTrust = true
		}
	}
	if c.Service {
		r := a.ServiceInstall(true, true)
		v.Service = &r
		v.OK = v.OK && r.OK
	}
	if c.TrayAutostart {
		r := a.GUIAutostartInstall(true)
		v.Tray = &r
		v.OK = v.OK && r.OK
	}
	return v
}
