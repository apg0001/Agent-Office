package app

// 설치 파일 기반 승인형 업데이트와 설치 뒤 후처리(#180, ADR-0006). 서명·해시 검증과 설치 프로그램 실행은
// Core(`update … --installer`, `update post-install`, `legacy`)가 하고, 이 파일은 그 JSON 결과를 화면·트레이용으로
// 정리한다. Core 출력에서 정해진 enum·코드·버전만 받아들인다.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
)

// ResultFile은 업데이트 도우미가 결과를 남기는 파일 이름이다(GUI 설정 폴더).
const ResultFile = "update-result.json"

var (
	codePattern    = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]{1,40})?$`)
)

func safeEnum(v string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return "unknown"
}

func safeCodeOrEmpty(v string) string {
	if v == "" || codePattern.MatchString(v) {
		return v
	}
	return "unexpected"
}

func safeVersion(v string) string {
	if versionPattern.MatchString(v) {
		return v
	}
	return ""
}

// UpdatePrefsView는 업데이트 설정 화면 데이터다.
type UpdatePrefsView struct {
	OK      bool   `json:"ok"`
	Mode    string `json:"mode"`
	Channel string `json:"channel"`
	// Effective는 실제로 확인할 채널(stable·beta)이다.
	Effective string `json:"effective"`
}

func (a *App) loadPrefs() (prefs.Prefs, string, error) {
	path, err := a.prefsPath()
	if err != nil {
		return prefs.Prefs{SchemaVersion: prefs.SchemaVersion}, "", err
	}
	p, _ := prefs.Load(path)
	return p, path, nil
}

// updateChannel은 설정한 채널, 없으면 Core 버전이 사전 배포(`-beta.N`)면 beta, 아니면 stable이다.
func (a *App) updateChannel() string {
	if p, _, err := a.loadPrefs(); err == nil && (p.UpdateChannel == "stable" || p.UpdateChannel == "beta") {
		return p.UpdateChannel
	}
	if v := a.Status(); v.OK && v.Report != nil && strings.Contains(v.Report.CollectorVersion, "-") {
		return "beta"
	}
	return "stable"
}

// UpdatePrefs는 업데이트 모드·채널 설정이다.
func (a *App) UpdatePrefs() UpdatePrefsView {
	p, _, err := a.loadPrefs()
	return UpdatePrefsView{OK: err == nil, Mode: p.Mode(), Channel: p.UpdateChannel, Effective: a.updateChannel()}
}

// SaveUpdatePrefs는 업데이트 모드(notify·download·auto·manual)와 채널(""·stable·beta)을 저장한다.
func (a *App) SaveUpdatePrefs(mode, channel string) Result {
	if !prefs.ValidMode(mode) || !prefs.ValidChannel(channel) {
		return fail("invalid_update_prefs")
	}
	p, path, err := a.loadPrefs()
	if err != nil {
		return fail("prefs_unavailable")
	}
	p.UpdateMode, p.UpdateChannel = mode, channel
	if err := prefs.Save(path, p); err != nil {
		return fail("prefs_write_failed")
	}
	return okResult("update_prefs_saved", "")
}

// InstallerView는 Core `update check|download|install --installer --json` 결과다.
type InstallerView struct {
	OK bool `json:"ok"`
	// Status는 latest / available / downloaded / installer_started / error / unknown이다.
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	Version string `json:"version,omitempty"`
	Current string `json:"current,omitempty"`
	Channel string `json:"channel,omitempty"`
	Message string `json:"message"`
}

func firstJSONLine(s string) []byte {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "{") {
			return []byte(t)
		}
	}
	return nil
}

// installerTimeout은 설치 파일 받기(최대 200MB×2) 상한이다. Core 내부 상한(15분)보다 조금 길다.
const installerTimeout = 16 * time.Minute

func (a *App) installer(sub string, extra ...string) InstallerView {
	r, err := a.runner()
	if err != nil {
		c := errCode(err)
		return InstallerView{Status: "error", Code: c, Message: messageFor(c)}
	}
	r.Timeout = updateTimeout
	if sub != "check" {
		r.Timeout = installerTimeout
	}
	args := append([]string{"update", sub, "--installer", "--channel", a.updateChannel(), "--json"}, extra...)
	res, err := r.Run(a.context(), "", args...)
	if err != nil {
		c := errCode(err)
		return InstallerView{Status: "error", Code: c, Message: messageFor(c)}
	}
	var raw InstallerView
	if json.Unmarshal(firstJSONLine(res.Stdout), &raw) != nil {
		return InstallerView{Status: "error", Code: "core_output_invalid", Message: messageFor("core_output_invalid")}
	}
	v := InstallerView{
		Status:  safeEnum(raw.Status, "latest", "available", "downloaded", "installer_started", "error"),
		Code:    safeCodeOrEmpty(raw.Code),
		Version: safeVersion(raw.Version), Current: safeVersion(raw.Current),
		Channel: safeEnum(raw.Channel, "stable", "beta"),
	}
	v.OK = v.Status != "error" && v.Status != "unknown"
	key := "update_" + v.Status
	if !v.OK {
		key = "update_" + v.Code
		if _, ok := messages[key]; !ok {
			key = "update_check_failed"
		}
	}
	v.Message = messageFor(key)
	return v
}

// InstallerCheck는 새 버전의 플랫폼 설치 파일이 있는지 확인한다(서명된 manifest).
func (a *App) InstallerCheck() InstallerView { return a.installer("check") }

// InstallerDownload는 설치 파일을 받아 해시를 확인해 둔다(자동 다운로드 모드).
func (a *App) InstallerDownload() InstallerView { return a.installer("download") }

// UpdateResultPath는 업데이트 도우미가 결과를 쓰는 파일이다.
func (a *App) UpdateResultPath() (string, error) {
	p, err := a.prefsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), ResultFile), nil
}

// trayExecutable은 설치 뒤 다시 띄울 트레이 도우미 위치다(설치 위치 고정).
func (a *App) trayExecutable() (string, error) {
	exe, err := a.guiExecutable()
	if err != nil {
		return "", err
	}
	if t, ok := corecli.FindSibling(exe, a.opts.GOOS, corecli.TrayName); ok {
		return t, nil
	}
	return "", errors.New("tray_missing")
}

// InstallerInstall은 사용자가 승인한 업데이트를 시작한다. Core가 받기·검증 뒤 업데이트 도우미를 띄우고 곧바로
// 끝난다(installer_started). 부른 쪽(트레이)은 설치 프로그램이 파일을 바꿀 수 있게 스스로 끝나야 한다.
// 도우미는 설치가 끝나면(성공·실패 모두) 트레이를 다시 띄운다.
func (a *App) InstallerInstall(confirmed bool) InstallerView {
	if !confirmed {
		return InstallerView{Status: "error", Code: "confirmation_required", Message: messageFor("confirmation_required")}
	}
	tray, err := a.trayExecutable()
	if err != nil {
		return InstallerView{Status: "error", Code: "tray_missing", Message: messageFor("update_tray_missing")}
	}
	res, err := a.UpdateResultPath()
	if err != nil {
		return InstallerView{Status: "error", Code: "prefs_unavailable", Message: messageFor("prefs_unavailable")}
	}
	return a.installer("install", "--yes", "--relaunch", tray, "--result", res)
}

// Outcome은 업데이트 도우미 결과다.
type Outcome struct {
	From string `json:"from"`
	To   string `json:"to"`
	Code string `json:"code"`
}

// TakeUpdateOutcome은 도우미가 남긴 결과를 읽고 지운다(없으면 nil).
func (a *App) TakeUpdateOutcome() *Outcome {
	path, err := a.UpdateResultPath()
	if err != nil {
		return nil
	}
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 64<<10 {
		return nil
	}
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return nil
	}
	var o Outcome
	if json.Unmarshal(data, &o) != nil {
		return nil
	}
	o.From, o.To, o.Code = safeVersion(o.From), safeVersion(o.To), safeCodeOrEmpty(o.Code)
	return &o
}

// PostInstallView는 Core `update post-install --json` 결과다.
type PostInstallView struct {
	OK           bool         `json:"ok"`
	Status       string       `json:"status"`
	Version      string       `json:"version,omitempty"`
	Hooks        []HookAction `json:"hooks,omitempty"`
	Service      string       `json:"service,omitempty"`
	Health       string       `json:"health,omitempty"`
	CodexRetrust bool         `json:"codex_retrust"`
	LegacyFiles  int          `json:"legacy_files"`
	Code         string       `json:"code,omitempty"`
}

// HookAction은 도구별 Hook 후처리 결과다.
type HookAction struct {
	Tool   string `json:"tool"`
	Action string `json:"action"`
}

// postInstallTimeout은 서비스 재시작·연결 확인(30초) 포함 상한이다.
const postInstallTimeout = 3 * time.Minute

// PostInstall은 설치·업데이트 뒤 후처리다. 이전에 설치돼 있던 소유 Hook·서비스 정의만 다시 적용한다.
func (a *App) PostInstall() PostInstallView {
	r, err := a.runner()
	if err != nil {
		return PostInstallView{Status: "error", Code: errCode(err)}
	}
	r.Timeout = postInstallTimeout
	res, err := r.Run(a.context(), "", "update", "post-install", "--json")
	if err != nil {
		return PostInstallView{Status: "error", Code: errCode(err)}
	}
	var raw PostInstallView
	if json.Unmarshal(firstJSONLine(res.Stdout), &raw) != nil {
		return PostInstallView{Status: "error", Code: "core_output_invalid"}
	}
	v := PostInstallView{
		Status:       safeEnum(raw.Status, "ok", "attention"),
		Version:      safeVersion(raw.Version),
		Service:      safeEnum(raw.Service, "absent", "foreign", "reinstalled", "failed", "unsupported"),
		Health:       safeEnum(raw.Health, "healthy", "unhealthy", "skipped"),
		CodexRetrust: raw.CodexRetrust, LegacyFiles: raw.LegacyFiles,
	}
	for _, h := range raw.Hooks {
		v.Hooks = append(v.Hooks, HookAction{Tool: safeEnum(h.Tool, "claude", "codex"),
			Action: safeEnum(h.Action, "absent", "unchanged", "reapplied", "failed", "unreadable")})
	}
	v.OK = v.Status == "ok" || v.Status == "attention"
	return v
}

// LastVersion·SetLastVersion은 트레이가 마지막으로 후처리를 마친 Core 버전이다.
func (a *App) LastVersion() string {
	p, _, _ := a.loadPrefs()
	return p.LastVersion
}

// SetLastVersion은 후처리를 마친 버전을 기록한다.
func (a *App) SetLastVersion(v string) {
	p, path, err := a.loadPrefs()
	if err != nil || safeVersion(v) == "" {
		return
	}
	p.LastVersion = v
	_ = prefs.Save(path, p)
}

// LegacyFile은 이전 명령행 설치 파일 하나다(이름·폴더 기호 표기·종류만).
type LegacyFile struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Location string `json:"location"`
	State    string `json:"state,omitempty"`
}

// LegacyView는 Core `legacy status|remove --json` 결과다.
type LegacyView struct {
	OK       bool         `json:"ok"`
	Code     string       `json:"code,omitempty"`
	Eligible bool         `json:"eligible"`
	Files    []LegacyFile `json:"files"`
	Removed  int          `json:"removed"`
	Failed   int          `json:"failed"`
}

var legacyName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func (a *App) legacy(args ...string) LegacyView {
	r, err := a.runner()
	if err != nil {
		return LegacyView{Code: errCode(err)}
	}
	res, err := r.Run(a.context(), "", append([]string{"legacy"}, args...)...)
	if err != nil {
		return LegacyView{Code: errCode(err)}
	}
	var raw LegacyView
	if json.Unmarshal(firstJSONLine(res.Stdout), &raw) != nil {
		return LegacyView{Code: "core_output_invalid"}
	}
	v := LegacyView{OK: true, Eligible: raw.Eligible, Removed: raw.Removed, Failed: raw.Failed, Files: []LegacyFile{}}
	for _, f := range raw.Files {
		if !legacyName.MatchString(f.Name) {
			continue
		}
		v.Files = append(v.Files, LegacyFile{
			Kind: safeEnum(f.Kind, "binary", "backup", "uninstall", "readme"), Name: f.Name,
			Location: redact(f.Location), State: safeCodeOrEmpty(f.State),
		})
	}
	return v
}

// LegacyStatus는 설치 프로그램으로 옮긴 뒤 남은 이전 명령행 설치 파일이다.
func (a *App) LegacyStatus() LegacyView { return a.legacy("status", "--json") }

// LegacyRemove는 승인 시 알아본 이전 설치 파일만 지운다.
func (a *App) LegacyRemove(confirmed bool) LegacyView {
	if !confirmed {
		return LegacyView{Code: "confirmation_required"}
	}
	return a.legacy("remove", "--yes", "--json")
}
