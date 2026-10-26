package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func inst(s *server, osName, arch, kind, pkg, name string, data []byte) Installer {
	sum := sha256.Sum256(data)
	s.files["/"+name] = data
	return Installer{OS: osName, Arch: arch, Kind: kind, Package: pkg, URL: s.URL + "/" + name,
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

func TestSelectInstallers(t *testing.T) {
	s := newServer(t)
	win := inst(s, "windows", "amd64", KindNSIS, "", "setup-0.2.0-windows-amd64.exe", []byte("exe"))
	mac := inst(s, "darwin", ArchUniversal, KindPkg, "", "agent-office-collector-0.2.0-macos-universal.pkg", []byte("pkg"))
	core := inst(s, "linux", "arm64", KindDeb, PackageCore, "agent-office-collector_0.2.0_arm64.deb", []byte("c"))
	desk := inst(s, "linux", "arm64", KindDeb, PackageDesktop, "agent-office-collector-desktop_0.2.0_arm64.deb", []byte("d"))
	m := Manifest{Installers: []Installer{desk, win, mac, core}}

	if got, err := SelectInstallers(m, "windows", "amd64", KindNSIS); err != nil || len(got) != 1 || got[0] != win {
		t.Fatalf("windows %v %v", got, err)
	}
	for _, arch := range []string{"arm64", "amd64"} {
		if got, err := SelectInstallers(m, "darwin", arch, KindPkg); err != nil || got[0] != mac {
			t.Fatalf("darwin %s %v %v", arch, got, err)
		}
	}
	// Linux는 Core → 데스크톱 순서로 둘 다 있어야 한다.
	if got, err := SelectInstallers(m, "linux", "arm64", KindDeb); err != nil || !slices.Equal(got, []Installer{core, desk}) {
		t.Fatalf("linux %v %v", got, err)
	}
	if _, err := SelectInstallers(Manifest{Installers: []Installer{core}}, "linux", "arm64", KindDeb); Code(err) != "no_installer_for_platform" {
		t.Fatalf("desktop missing: %v", err)
	}
	if _, err := SelectInstallers(m, "linux", "amd64", KindDeb); Code(err) != "no_installer_for_platform" {
		t.Fatalf("other arch: %v", err)
	}
	if _, err := SelectInstallers(m, "linux", "arm64", KindNSIS); Code(err) != "unsupported_installer" {
		t.Fatalf("kind/os mismatch: %v", err)
	}
	dup := Manifest{Installers: []Installer{win, win}}
	if _, err := SelectInstallers(dup, "windows", "amd64", KindNSIS); Code(err) != "bad_manifest" {
		t.Fatalf("duplicate: %v", err)
	}
	for _, mutate := range []func(*Installer){
		func(i *Installer) { i.SHA256 = "abc" },
		func(i *Installer) { i.Size = 0 },
		func(i *Installer) { i.Size = MaxArtifactBytes + 1 },
		func(i *Installer) { i.URL = s.URL + "/bad%20name.exe" },
		func(i *Installer) { i.URL = s.URL + "/" },
	} {
		bad := win
		mutate(&bad)
		if _, err := SelectInstallers(Manifest{Installers: []Installer{bad}}, "windows", "amd64", KindNSIS); Code(err) != "bad_manifest" {
			t.Fatalf("bad installer %+v accepted: %v", bad, err)
		}
	}
}

func TestCheckInstallerBackwardCompatible(t *testing.T) {
	r, s := newRelease(t), newServer(t)
	ctx := context.Background()
	a := artifactFor(s, tarGz(t, map[string]string{"agent-office-collector": "x"}))
	// 이전 형식(Core 압축본만): 설치 파일 업데이트는 없다고 알리고, Core 자체 업데이트는 그대로 동작한다.
	s.publish(t, r, Manifest{Schema: 1, Channel: "beta", Version: "0.2.0", PublishedAt: time.Now(), Artifacts: []Artifact{a}})
	if _, err := CheckInstaller(ctx, options(s, r), KindDeb); Code(err) != "no_installer_for_platform" {
		t.Fatalf("old manifest: %v", err)
	}
	core := inst(s, "linux", "arm64", KindDeb, PackageCore, "agent-office-collector_0.2.0_arm64.deb", []byte("core"))
	desk := inst(s, "linux", "arm64", KindDeb, PackageDesktop, "agent-office-collector-desktop_0.2.0_arm64.deb", []byte("desk"))
	s.publish(t, r, Manifest{Schema: 1, Channel: "beta", Version: "0.2.0", PublishedAt: time.Now(),
		Artifacts: []Artifact{a}, Installers: []Installer{core, desk}})
	if avail, err := Check(ctx, options(s, r)); err != nil || avail == nil {
		t.Fatalf("archive check with installers present: %v", err)
	}
	up, err := CheckInstaller(ctx, options(s, r), KindDeb)
	if err != nil || up == nil || up.Version != "0.2.0" || len(up.Installers) != 2 {
		t.Fatalf("installer check %+v %v", up, err)
	}
	o := options(s, r)
	o.Current = "0.2.0"
	if up, err := CheckInstaller(ctx, o, KindDeb); err != nil || up != nil {
		t.Fatalf("same version %+v %v", up, err)
	}
	o = options(s, r)
	o.AllowLoopbackHTTP = false
	if _, err := CheckInstaller(ctx, o, KindDeb); Code(err) != "insecure_url" {
		t.Fatalf("http refused: %v", err)
	}
	// 서명은 installers까지 덮는다: 서명 뒤 목록을 바꾸면 거부한다.
	s.files["/manifest.json"] = []byte(strings.Replace(string(s.files["/manifest.json"]), "desktop", "deskt0p", 1))
	if _, err := CheckInstaller(ctx, options(s, r), KindDeb); Code(err) != "bad_signature" {
		t.Fatalf("tampered: %v", err)
	}

	dir := t.TempDir()
	files, err := DownloadInstallers(ctx, options(s, r), up.Installers, dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("download %v %v", files, err)
	}
	for i, f := range files {
		if err := VerifyFile(f, up.Installers[i].SHA256, up.Installers[i].Size); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Stat(f); fi.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
			t.Fatalf("perm %v", fi.Mode())
		}
	}
	// 받아 둔 파일은 다시 받지 않는다(서버에서 지워도 성공).
	delete(s.files, "/agent-office-collector_0.2.0_arm64.deb")
	if _, err := DownloadInstallers(ctx, options(s, r), up.Installers, dir); err != nil {
		t.Fatalf("cached: %v", err)
	}
	os.WriteFile(files[0], []byte("tampered"), 0o600)
	if err := VerifyFile(files[0], up.Installers[0].SHA256, up.Installers[0].Size); Code(err) != "checksum_mismatch" {
		t.Fatalf("verify tampered: %v", err)
	}
	s.files["/agent-office-collector-desktop_0.2.0_arm64.deb"] = []byte("evil")
	os.Remove(files[1])
	if _, err := DownloadInstallers(ctx, options(s, r), up.Installers[1:], dir); Code(err) != "checksum_mismatch" {
		t.Fatalf("server tampered: %v", err)
	}
}

func TestInstallCommand(t *testing.T) {
	abs := "/tmp/x.pkg"
	if os.PathSeparator == '\\' {
		abs = `C:\x\setup.exe`
	}
	cases := []struct {
		goos, kind, elevate string
		want                []string
	}{
		{"windows", KindNSIS, "", []string{abs, "/S"}},
		{"darwin", KindPkg, ElevateSudoN, []string{"sudo", "-n", "/usr/sbin/installer", "-pkg", abs, "-target", "/"}},
		{"linux", KindDeb, ElevateSudoN, []string{"sudo", "-n", "DEBIAN_FRONTEND=noninteractive", "/usr/bin/apt-get", "install", "-y", "-q", "--no-install-recommends", abs, abs}},
		{"linux", KindRPM, "", []string{"/usr/bin/pkexec", "env", "DEBIAN_FRONTEND=noninteractive", "/usr/bin/dnf", "install", "-y", abs, abs}},
	}
	for _, c := range cases {
		files := []string{abs}
		if c.goos == "linux" {
			files = []string{abs, abs}
		}
		got, err := InstallCommand(c.goos, c.kind, c.elevate, files)
		if err != nil || !slices.Equal(got, c.want) {
			t.Fatalf("%s/%s: %q %v", c.goos, c.kind, got, err)
		}
	}
	// macOS 기본은 관리자 승인 창이고 경로는 셸 문자열이 아니라 argv로 넘긴다.
	got, err := InstallCommand("darwin", KindPkg, "", []string{"/tmp/a'b.pkg"})
	if err != nil || got[0] != "/usr/bin/osascript" || got[len(got)-1] != "/tmp/a'b.pkg" || !strings.Contains(got[4], "with administrator privileges") || strings.Contains(got[4], "a'b") {
		t.Fatalf("osascript %q %v", got, err)
	}
	if got, err := InstallCommand("linux", KindDeb, "", []string{"/a.deb", "/b.deb"}); err != nil || got[0] != "/usr/bin/pkexec" {
		t.Fatalf("pkexec %q %v", got, err)
	}
	for _, bad := range [][]string{{"relative.deb"}, {"/a\n.deb"}, {}} {
		if _, err := InstallCommand("linux", KindDeb, "", bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := InstallCommand("linux", KindDeb, "doas", []string{"/a.deb"}); Code(err) != "bad_elevate" {
		t.Fatalf("elevate: %v", err)
	}
	if _, err := InstallCommand("darwin", KindNSIS, "", []string{"/a.exe"}); Code(err) != "unsupported_installer" {
		t.Fatalf("kind: %v", err)
	}
}

func TestDetectKind(t *testing.T) {
	none := func(...string) bool { return false }
	deb := func(a ...string) bool { return a[0] == "dpkg-query" }
	rpm := func(a ...string) bool { return a[0] == "rpm" }
	for _, c := range []struct {
		goos string
		has  func(...string) bool
		want string
	}{{"windows", none, KindNSIS}, {"darwin", none, KindPkg}, {"linux", deb, KindDeb}, {"linux", rpm, KindRPM}, {"linux", none, ""}, {"plan9", none, ""}} {
		if got := DetectKind(c.goos, c.has); got != c.want {
			t.Fatalf("%s: %q", c.goos, got)
		}
	}
}

func TestPlanValidateAndJSON(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.deb")
	good := Plan{Schema: PlanSchema, From: "0.1.0", To: "0.2.0", GOOS: "linux", Kind: KindDeb, Core: f,
		Files: []PlanFile{{Path: f, SHA256: strings.Repeat("a", 64), Size: 1}}, Relaunch: f}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Schema = 2 },
		func(p *Plan) { p.To = "latest" },
		func(p *Plan) { p.Files = nil },
		func(p *Plan) { p.Files[0].Path = "a.deb" },
		func(p *Plan) { p.Kind = KindPkg },
		func(p *Plan) { p.Relaunch = "tray" },
		func(p *Plan) { p.Core = "" },
	} {
		p := good
		p.Files = slices.Clone(good.Files)
		mutate(&p)
		if p.Validate() == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	path := filepath.Join(dir, "sub", "plan.json")
	if err := WriteJSON(path, good); err != nil {
		t.Fatal(err)
	}
	var back Plan
	if err := ReadJSON(path, &back); err != nil || back.To != "0.2.0" || back.Files[0] != good.Files[0] {
		t.Fatalf("%+v %v", back, err)
	}
}
