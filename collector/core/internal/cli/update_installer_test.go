package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/runner"
	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

// installerServer는 이 OS용 설치 파일을 담은 서명 manifest(0.2.0, beta)를 내놓는다.
func installerServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	files := map[string][]byte{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	add := func(kind, pkg, name string) update.Installer {
		body := []byte("installer " + name)
		files["/"+name] = body
		sum := sha256.Sum256(body)
		arch := runtime.GOARCH
		if kind == update.KindPkg {
			arch = update.ArchUniversal
		}
		return update.Installer{OS: runtime.GOOS, Arch: arch, Kind: kind, Package: pkg, URL: srv.URL + "/" + name,
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
	}
	var list []update.Installer
	switch runtime.GOOS {
	case "windows":
		list = append(list, add(update.KindNSIS, "", "agent-office-collector-setup-0.2.0-windows-amd64.exe"))
	case "darwin":
		list = append(list, add(update.KindPkg, "", "agent-office-collector-0.2.0-macos-universal.pkg"))
	default:
		list = append(list, add(update.KindDeb, update.PackageCore, "agent-office-collector_0.2.0_x.deb"),
			add(update.KindDeb, update.PackageDesktop, "agent-office-collector-desktop_0.2.0_x.deb"))
	}
	m, _ := json.Marshal(update.Manifest{Schema: 1, Channel: "beta", Version: "0.2.0", Installers: list})
	files["/manifest.json"] = m
	files["/manifest.json.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m)))
	prev := update.TrustedKeys
	update.TrustedKeys = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { update.TrustedKeys = prev })
	return srv, srv.URL + "/manifest.json"
}

// installedLayout은 설치 프로그램 배치(Core 옆에 트레이 도우미)를 만든다.
func installedLayout(t *testing.T) (core, tray string) {
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	core = filepath.Join(dir, "agent-office-collector"+ext)
	tray = filepath.Join(dir, "agent-office-collector-tray"+ext)
	os.WriteFile(core, []byte("core"), 0o755)
	os.WriteFile(tray, []byte("tray"), 0o755)
	return core, tray
}

type recorder struct {
	mu     sync.Mutex
	spawn  [][]string
	long   [][]string
	cmds   [][]string
	exit   int
	tail   string
	coreV  string
	failOn string
}

func (r *recorder) install(te *testEnv) {
	te.env.Spawn = func(name string, args ...string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.spawn = append(r.spawn, append([]string{name}, args...))
		return nil
	}
	te.env.RunLong = func(_ time.Duration, args ...string) (int, []byte, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.long = append(r.long, args)
		return r.exit, []byte(r.tail), nil
	}
	te.env.Command = func(args ...string) ([]byte, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cmds = append(r.cmds, args)
		if len(args) == 2 && args[1] == "version" {
			return []byte("agent-office-collector " + r.coreV + " (x)\n"), nil
		}
		if len(args) > 1 && args[0] == "schtasks" && args[1] == "/Query" {
			return nil, os.ErrNotExist // 같은 이름 작업 없음
		}
		if r.failOn != "" && args[0] == r.failOn {
			return nil, os.ErrPermission
		}
		return nil, nil
	}
}

func decode[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &v); err != nil {
		t.Fatalf("json %q: %v", s, err)
	}
	return v
}

func TestUpdateInstallerFlow(t *testing.T) {
	prevSettle := helperSettle
	helperSettle = 0
	t.Cleanup(func() { helperSettle = prevSettle })
	srv, url := installerServer(t)
	te := newTestEnv(t, "inst")
	te.env.Version = "0.1.0-beta.1"
	te.env.HTTP = srv.Client()
	core, tray := installedLayout(t)
	te.env.Executable = func() (string, error) { return core, nil }
	rec := &recorder{coreV: "0.2.0"}
	rec.install(te)
	args := []string{"--installer", "--channel", "beta", "--manifest-url", url, "--json"}

	if code := te.run("", append([]string{"update", "check"}, args...)...); code != ExitOK {
		t.Fatalf("check %d %s %s", code, te.stdout, te.stderr)
	}
	rep := decode[installerReport](t, te.stdout.String())
	if rep.Status != "available" || rep.Version != "0.2.0" || rep.Current != "0.1.0-beta.1" {
		t.Fatalf("%+v", rep)
	}
	if strings.Contains(te.stdout.String(), srv.URL) {
		t.Fatal("URL leaked")
	}
	// 승인(--yes) 없이는 설치를 시작하지 않는다.
	te.run("", append([]string{"update", "install"}, args...)...)
	if r := decode[installerReport](t, te.stdout.String()); r.Code != "confirmation_required" || len(rec.spawn) != 0 {
		t.Fatalf("%+v %v", r, rec.spawn)
	}
	result := filepath.Join(t.TempDir(), "update-result.json")
	if code := te.run("", append([]string{"update", "install", "--yes", "--relaunch", tray, "--result", result}, args...)...); code != ExitOK {
		t.Fatalf("install %d %s", code, te.stdout)
	}
	if r := decode[installerReport](t, te.stdout.String()); r.Status != "installer_started" {
		t.Fatalf("%+v", r)
	}
	if len(rec.spawn) != 1 || rec.spawn[0][1] != "update" || rec.spawn[0][2] != "run-plan" {
		t.Fatalf("spawn %q", rec.spawn)
	}
	helper, planPath := rec.spawn[0][0], rec.spawn[0][3]
	if !strings.HasPrefix(filepath.Base(helper), HelperName) {
		t.Fatalf("helper name %s", helper)
	}
	if b, _ := os.ReadFile(helper); string(b) != "core" {
		t.Fatal("helper is not a copy of Core")
	}
	var plan update.Plan
	if err := update.ReadJSON(planPath, &plan); err != nil || plan.Validate() != nil || plan.Core != core || plan.Relaunch != tray {
		t.Fatalf("plan %+v %v", plan, err)
	}

	// 도우미: 설치 프로그램 실행 → 버전 확인 → 결과 기록 → 트레이 재실행.
	rec.spawn = nil
	if code := te.run("", "update", "run-plan", planPath); code != ExitOK {
		t.Fatalf("run-plan %d", code)
	}
	if len(rec.long) != 1 {
		t.Fatalf("installer runs %q", rec.long)
	}
	var out update.Outcome
	if err := update.ReadJSON(result, &out); err != nil || out.Code != "installed" || out.To != "0.2.0" || out.From != "0.1.0-beta.1" {
		t.Fatalf("outcome %+v %v", out, err)
	}
	if len(rec.spawn) != 1 || rec.spawn[0][0] != tray {
		t.Fatalf("tray relaunch %q", rec.spawn)
	}
	if _, err := os.Stat(plan.Files[0].Path); !os.IsNotExist(err) {
		t.Fatal("installer file kept after success")
	}

	// 실패: 설치 프로그램이 실패해도 결과를 남기고 트레이를 다시 띄운다(이전 버전 유지).
	te.run("", append([]string{"update", "install", "--yes", "--relaunch", tray, "--result", result}, args...)...)
	planPath = rec.spawn[len(rec.spawn)-1][3]
	rec.spawn, rec.exit = nil, 1
	if code := te.run("", "update", "run-plan", planPath); code != ExitError {
		t.Fatalf("run-plan failure %d", code)
	}
	update.ReadJSON(result, &out)
	if out.Code != "installer_failed" || len(rec.spawn) != 1 {
		t.Fatalf("failure outcome %+v %q", out, rec.spawn)
	}
	// 버전이 오르지 않았으면 성공으로 보지 않는다.
	rec.exit, rec.coreV, rec.spawn = 0, "0.1.0-beta.1", nil
	te.run("", append([]string{"update", "install", "--yes", "--relaunch", tray, "--result", result}, args...)...)
	planPath = rec.spawn[len(rec.spawn)-1][3]
	te.run("", "update", "run-plan", planPath)
	update.ReadJSON(result, &out)
	if out.Code != "version_mismatch" {
		t.Fatalf("version check %+v", out)
	}
	// 계획을 쓴 뒤 설치 파일이 바뀌면 실행하지 않는다.
	rec.long = nil
	os.WriteFile(plan.Files[0].Path, []byte("swapped"), 0o600)
	te.run("", "update", "run-plan", planPath)
	update.ReadJSON(result, &out)
	if out.Code != "checksum_mismatch" || len(rec.long) != 0 {
		t.Fatalf("swapped file %+v %q", out, rec.long)
	}
}

func TestUpdateInstallerRefusesCLIInstall(t *testing.T) {
	srv, url := installerServer(t)
	te := newTestEnv(t, "cli")
	te.env.HTTP = srv.Client()
	exe := filepath.Join(t.TempDir(), "agent-office-collector")
	os.WriteFile(exe, []byte("x"), 0o755)
	te.env.Executable = func() (string, error) { return exe, nil }
	(&recorder{}).install(te)
	te.run("", "update", "check", "--installer", "--channel", "beta", "--manifest-url", url, "--json")
	if r := decode[installerReport](t, te.stdout.String()); r.Code != "not_installer_install" {
		t.Fatalf("%+v", r)
	}
}

func TestInstallerCancelled(t *testing.T) {
	if !installerCancelled("darwin", 1, []byte("execution error: User canceled. (-128)")) || installerCancelled("darwin", 1, []byte("other")) {
		t.Fatal("darwin")
	}
	if !installerCancelled("linux", 126, nil) || installerCancelled("linux", 1, nil) || installerCancelled("windows", 2, nil) {
		t.Fatal("linux/windows")
	}
}

// 명령행 설치(~/.local/bin 등) → 설치 프로그램 배치로 옮긴 뒤의 후처리: 소유 Hook·서비스 정의가 새 경로를
// 가리키고, 다른 Hook은 그대로다.
func TestPostInstallMigratesOwnedEntries(t *testing.T) {
	te := newTestEnv(t, "migrate")
	rec := &recorder{}
	rec.install(te)
	old := filepath.Join(te.home, ".local", "bin", "agent-office-collector")
	if runtime.GOOS == "windows" {
		old = filepath.Join(te.home, "AgentOffice", "bin", "agent-office-collector.exe")
	}
	te.env.Executable = func() (string, error) { return old, nil }
	settings := filepath.Join(te.home, "claude", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o700)
	os.WriteFile(settings, []byte(`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"echo user"}]}]}}`), 0o600)
	if code := te.run("", "hooks", "install", "--tool", "claude", "--yes"); code != ExitOK {
		t.Fatalf("old hooks %s", te.stderr)
	}
	if code := te.run("", "service", "install"); code != ExitOK {
		t.Fatalf("old service %s", te.stderr)
	}
	core, _ := installedLayout(t)
	te.env.Executable = func() (string, error) { return core, nil }
	te.env.Version = "0.2.0"
	// 새 Core가 상태 파일을 썼다고 둔다(연결 확인).
	snap, _ := json.Marshal(runner.Snapshot{SchemaVersion: 1, CollectorVersion: "0.2.0", State: "running"})
	os.MkdirAll(te.dirs.State, 0o700)
	os.WriteFile(te.dirs.StatusFile(), snap, 0o600)

	if code := te.run("", "update", "post-install", "--json"); code != ExitOK {
		t.Fatalf("post-install %d %s %s", code, te.stdout, te.stderr)
	}
	rep := decode[postInstallReport](t, te.stdout.String())
	if rep.Service != "reinstalled" || rep.Health != "healthy" || rep.CodexRetrust {
		t.Fatalf("%+v", rep)
	}
	if !slices.Contains(rep.Hooks, hookAction{Tool: "claude", Action: "reapplied"}) || !slices.Contains(rep.Hooks, hookAction{Tool: "codex", Action: "absent"}) {
		t.Fatalf("hooks %+v", rep.Hooks)
	}
	data, _ := os.ReadFile(settings)
	// Hook 명령 안의 경로 표기(Windows는 슬래시, hooks.Command).
	esc := func(p string) string {
		if runtime.GOOS == "windows" {
			return strings.ReplaceAll(p, `\`, "/")
		}
		b, _ := json.Marshal(p)
		return strings.Trim(string(b), `"`)
	}
	if strings.Contains(string(data), esc(old)) || !strings.Contains(string(data), esc(core)) || !strings.Contains(string(data), "echo user") {
		t.Fatalf("settings not migrated: %s", data)
	}
	if strings.Contains(te.stdout.String(), te.home) {
		t.Fatal("path leaked in report")
	}
	// 두 번째 실행은 바꿀 것이 없다(멱등).
	te.run("", "update", "post-install", "--json")
	if rep := decode[postInstallReport](t, te.stdout.String()); !slices.Contains(rep.Hooks, hookAction{Tool: "claude", Action: "unchanged"}) {
		t.Fatalf("second %+v", rep.Hooks)
	}
	if runtime.GOOS == "linux" {
		if !slices.ContainsFunc(rec.cmds, func(c []string) bool {
			return slices.Equal(c, []string{"systemctl", "--user", "restart", "agent-office-collector.service"})
		}) {
			t.Fatalf("no restart: %q", rec.cmds)
		}
	}
}

func TestLegacyStatusAndRemove(t *testing.T) {
	te := newTestEnv(t, "legacy")
	te.env.GOOS = "linux"
	(&recorder{}).install(te)
	prev := coreBinaryCheck
	coreBinaryCheck = func(p string) bool { b, _ := os.ReadFile(p); return string(b) == "ELF core" }
	t.Cleanup(func() { coreBinaryCheck = prev })
	bin := filepath.Join(te.home, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	write := func(name, body string) { os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755) }
	write("agent-office-collector", "ELF core")
	write("agent-office-collector.bak", "ELF core")
	write("agent-office-collector-uninstall", "#!/bin/sh\n# Agent Office ... agent-office-collector\n")
	write("README.txt", "Agent Office Collector Core\nagent-office-collector register\n")
	write("other-tool", "ELF core")
	readme2 := filepath.Join(te.home, ".local", "README.txt")
	os.WriteFile(readme2, []byte("unrelated"), 0o644)

	// 명령행 설치에서 실행하면 정리 대상이 아니다(eligible=false, 지우지 않음).
	te.env.Executable = func() (string, error) { return filepath.Join(bin, "agent-office-collector"), nil }
	te.run("", "legacy", "status", "--json")
	if r := decode[legacyReport](t, te.stdout.String()); len(r.Files) != 0 {
		t.Fatalf("self dir listed %+v", r)
	}
	other := filepath.Join(t.TempDir(), "agent-office-collector")
	te.env.Executable = func() (string, error) { return other, nil }
	if code := te.run("", "legacy", "remove", "--yes"); code != ExitError {
		t.Fatal("removed while not installer layout")
	}

	core, _ := installedLayout(t)
	te.env.Executable = func() (string, error) { return core, nil }
	te.run("", "legacy", "status", "--json")
	r := decode[legacyReport](t, te.stdout.String())
	if !r.Eligible || len(r.Files) != 4 {
		t.Fatalf("status %+v", r)
	}
	if strings.Contains(te.stdout.String(), te.home) {
		t.Fatal("home path leaked")
	}
	// Hook이 아직 이전 실행 파일을 가리키면 그 파일은 지우지 않는다.
	settings := filepath.Join(te.home, "claude", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o700)
	os.WriteFile(settings, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"'`+filepath.Join(bin, "agent-office-collector")+`' hook claude"}]}]}}`), 0o600)
	if code := te.run("", "legacy", "remove", "--yes", "--json"); code != ExitError {
		t.Fatal("removed referenced binary")
	}
	r = decode[legacyReport](t, te.stdout.String())
	if r.Removed != 3 || r.Failed != 1 {
		t.Fatalf("referenced %+v", r)
	}
	os.Remove(settings)
	if code := te.run("", "legacy", "remove", "--yes", "--json"); code != ExitOK {
		t.Fatalf("remove %s", te.stdout)
	}
	for _, keep := range []string{filepath.Join(bin, "other-tool"), readme2} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("removed unrelated %s", keep)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "agent-office-collector")); !os.IsNotExist(err) {
		t.Fatal("legacy binary kept")
	}
}
