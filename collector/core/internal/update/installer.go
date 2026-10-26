package update

// 설치 파일 기반 업데이트(#180, ADR-0006).
//
// 설치 프로그램(.exe·.pkg·데스크톱 deb/rpm)으로 설치한 Collector는 Core만 바꾸지 않고(managed_by_package),
// 서명된 manifest의 installers 목록에서 플랫폼 설치 파일을 받아 크기·SHA-256을 검사한 뒤 OS 설치 수단으로
// 실행한다. 이 파일은 순수 로직(선택·검증·명령 구성·계획/결과 파일)만 담고, 실행은 cli가 한다.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// 설치 파일 종류.
const (
	KindNSIS = "nsis" // Windows 사용자별 NSIS 설치 프로그램(.exe, /S 무인)
	KindPkg  = "pkg"  // macOS 유니버설 .pkg(installer(8), 관리자 승인)
	KindDeb  = "deb"  // Linux 데스크톱 deb(apt-get, pkexec 승인)
	KindRPM  = "rpm"  // Linux 데스크톱 rpm(dnf, pkexec 승인)
)

// Linux 설치 파일은 Core 패키지와 데스크톱 패키지 두 개다(데스크톱이 같은 버전 Core에 의존).
const (
	PackageCore    = "agent-office-collector"
	PackageDesktop = "agent-office-collector-desktop"
	// ArchUniversal은 macOS 유니버설 .pkg처럼 아키텍처를 가리지 않는 설치 파일이다.
	ArchUniversal = "universal"
)

// Installer는 manifest의 설치 파일 하나다.
type Installer struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Kind    string `json:"kind"`
	Package string `json:"package,omitempty"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

var (
	safeFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,200}$`)
	hexSHA256    = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// FileName은 URL 마지막 경로 조각이다. 안전한 파일 이름이 아니면 "".
func (i Installer) FileName() string {
	u, err := url.Parse(i.URL)
	if err != nil {
		return ""
	}
	name := u.Path[strings.LastIndex(u.Path, "/")+1:]
	if !safeFileName.MatchString(name) || strings.Contains(name, "..") {
		return ""
	}
	return name
}

func validKind(goos, kind string) bool {
	switch goos {
	case "windows":
		return kind == KindNSIS
	case "darwin":
		return kind == KindPkg
	case "linux":
		return kind == KindDeb || kind == KindRPM
	}
	return false
}

// SelectInstallers는 goos·goarch·kind에 맞는 설치 파일을 고른다. Linux는 Core·데스크톱 두 패키지(이 순서),
// 나머지는 하나다. 아키텍처는 정확히 같거나 universal이어야 한다.
func SelectInstallers(m Manifest, goos, goarch, kind string) ([]Installer, error) {
	if !validKind(goos, kind) {
		return nil, fail("unsupported_installer")
	}
	byPkg := map[string]Installer{}
	var single []Installer
	for _, in := range m.Installers {
		if in.OS != goos || in.Kind != kind || (in.Arch != goarch && in.Arch != ArchUniversal) {
			continue
		}
		if kind == KindDeb || kind == KindRPM {
			if in.Package != PackageCore && in.Package != PackageDesktop {
				continue
			}
			if _, dup := byPkg[in.Package]; dup {
				return nil, fail("bad_manifest")
			}
			byPkg[in.Package] = in
			continue
		}
		single = append(single, in)
	}
	var out []Installer
	if kind == KindDeb || kind == KindRPM {
		core, ok1 := byPkg[PackageCore]
		desk, ok2 := byPkg[PackageDesktop]
		if !ok1 || !ok2 {
			return nil, fail("no_installer_for_platform")
		}
		out = []Installer{core, desk}
	} else {
		if len(single) == 0 {
			return nil, fail("no_installer_for_platform")
		}
		if len(single) > 1 {
			return nil, fail("bad_manifest")
		}
		out = single
	}
	for _, in := range out {
		if in.Size <= 0 || in.Size > MaxArtifactBytes || !hexSHA256.MatchString(in.SHA256) || in.FileName() == "" {
			return nil, fail("bad_manifest")
		}
	}
	return out, nil
}

// InstallerUpdate는 설치할 수 있는 새 버전과 설치 파일이다.
type InstallerUpdate struct {
	Version    string
	Kind       string
	Installers []Installer
}

// CheckInstaller는 서명된 manifest에서 새 버전의 설치 파일을 찾는다. 새 버전이 없으면 nil.
func CheckInstaller(ctx context.Context, o Options, kind string) (*InstallerUpdate, error) {
	m, err := Fetch(ctx, o)
	if err != nil {
		return nil, err
	}
	if newer, err := o.Newer(m); err != nil || !newer {
		return nil, err
	}
	list, err := SelectInstallers(m, o.GOOS, o.GOARCH, kind)
	if err != nil {
		return nil, err
	}
	for _, in := range list {
		if err := o.checkURL(in.URL); err != nil {
			return nil, err
		}
	}
	return &InstallerUpdate{Version: m.Version, Kind: kind, Installers: list}, nil
}

// VerifyFile은 파일 크기와 SHA-256을 확인한다(링크·일반 파일 아님은 거부).
func VerifyFile(path, sum string, size int64) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fail("file_missing")
	}
	if !fi.Mode().IsRegular() || fi.Size() != size {
		return fail("checksum_mismatch")
	}
	f, err := os.Open(path)
	if err != nil {
		return fail("file_missing")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, MaxArtifactBytes+1)); err != nil {
		return fail("checksum_mismatch")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum) {
		return fail("checksum_mismatch")
	}
	return nil
}

// DownloadInstallers는 설치 파일을 dir에 받는다. 같은 이름의 파일이 이미 있고 해시가 맞으면 다시 받지 않는다
// (자동 다운로드 모드에서 받아 둔 파일). 임시 파일 → 검증 → rename으로 쓴다(0600).
func DownloadInstallers(ctx context.Context, o Options, list []Installer, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fail("write_failed")
	}
	var out []string
	for _, in := range list {
		dest := filepath.Join(dir, in.FileName())
		if VerifyFile(dest, in.SHA256, in.Size) == nil {
			out = append(out, dest)
			continue
		}
		data, err := o.get(ctx, in.URL, in.Size)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != in.Size || !strings.EqualFold(hex.EncodeToString(sum[:]), in.SHA256) {
			return nil, fail("checksum_mismatch")
		}
		if err := writeAtomic(dest, data, 0o600); err != nil {
			return nil, fail("write_failed")
		}
		out = append(out, dest)
	}
	return out, nil
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Remove(path) // Windows rename은 대상이 있으면 실패한다.
	return os.Rename(name, path)
}

// 권한 상승 방식. 기본은 OS 승인 창(macOS 관리자 암호 대화상자, Linux polkit pkexec)이다.
// ElevateSudoN은 테스트 러너 전용(암호 없는 sudo)으로, 환경 변수 AGENT_OFFICE_UPDATE_ELEVATE=sudo-n일 때만 쓴다.
const (
	ElevatePrompt = ""
	ElevateSudoN  = "sudo-n"
)

// InstallCommand는 설치 파일을 실행하는 명령이다. 경로는 인자로만 넘기고 셸 문자열에 끼워 넣지 않는다.
func InstallCommand(goos, kind, elevate string, files []string) ([]string, error) {
	if !validKind(goos, kind) || len(files) == 0 {
		return nil, fail("unsupported_installer")
	}
	for _, f := range files {
		if !filepath.IsAbs(f) || strings.ContainsAny(f, "\x00\r\n") {
			return nil, fail("bad_path")
		}
	}
	if elevate != ElevatePrompt && elevate != ElevateSudoN {
		return nil, fail("bad_elevate")
	}
	switch kind {
	case KindNSIS:
		// 사용자별 설치라 관리자 권한이 필요 없다. 설치 폴더는 HKCU InstallLocation을 그대로 쓴다(경로 고정).
		return []string{files[0], "/S"}, nil
	case KindPkg:
		if elevate == ElevateSudoN {
			return []string{"sudo", "-n", "/usr/sbin/installer", "-pkg", files[0], "-target", "/"}, nil
		}
		// 관리자 승인 창. 파일 경로는 argv로 넘기고 AppleScript가 quoted form으로 셸 인용한다.
		return []string{"/usr/bin/osascript",
			"-e", "on run argv",
			"-e", `do shell script "/usr/sbin/installer -pkg " & quoted form of (item 1 of argv) & " -target /" with prompt "Agent Office Collector 업데이트를 설치하려면 관리자 승인이 필요합니다." with administrator privileges`,
			"-e", "end run", files[0]}, nil
	case KindDeb:
		cmd := append([]string{"/usr/bin/apt-get", "install", "-y", "-q", "--no-install-recommends"}, files...)
		return elevated(elevate, cmd), nil
	case KindRPM:
		cmd := append([]string{"/usr/bin/dnf", "install", "-y"}, files...)
		return elevated(elevate, cmd), nil
	}
	return nil, fail("unsupported_installer")
}

func elevated(elevate string, cmd []string) []string {
	if elevate == ElevateSudoN {
		return append([]string{"sudo", "-n", "DEBIAN_FRONTEND=noninteractive"}, cmd...)
	}
	return append([]string{"/usr/bin/pkexec", "/usr/bin/env", "DEBIAN_FRONTEND=noninteractive"}, cmd...)
}

// DetectKind는 현재 설치가 어떤 설치 파일로 업데이트되는지다. 알 수 없으면 "".
// has는 명령이 성공하는지다(Linux 패키지 조회).
func DetectKind(goos string, has func(args ...string) bool) string {
	switch goos {
	case "windows":
		return KindNSIS
	case "darwin":
		return KindPkg
	case "linux":
		if has("dpkg-query", "-W", "-f=${Status}", PackageDesktop) {
			return KindDeb
		}
		if has("rpm", "-q", PackageDesktop) {
			return KindRPM
		}
	}
	return ""
}

// PlanSchema는 업데이트 계획·결과 파일 형식 버전이다.
const PlanSchema = 1

// PlanFile은 실행할 설치 파일과 기대 해시다.
type PlanFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Plan은 Core가 검증을 마친 뒤 업데이트 도우미(`update run-plan`)에게 넘기는 계획이다. 사용자 캐시
// 폴더(0700)에 0600으로 쓴다. 도우미는 실행 직전에 해시를 다시 확인한다.
type Plan struct {
	Schema  int        `json:"schema"`
	From    string     `json:"from"`
	To      string     `json:"to"`
	GOOS    string     `json:"goos"`
	Kind    string     `json:"kind"`
	Elevate string     `json:"elevate,omitempty"`
	Files   []PlanFile `json:"files"`
	// Core는 설치 뒤 버전을 확인할 Core 실행 파일(설치 위치는 고정)이다.
	Core     string `json:"core"`
	Relaunch string `json:"relaunch,omitempty"`
	Result   string `json:"result,omitempty"`
}

// Validate는 계획 형식을 확인한다.
func (p Plan) Validate() error {
	if p.Schema != PlanSchema || len(p.Files) == 0 || !validKind(p.GOOS, p.Kind) {
		return fail("bad_plan")
	}
	if _, ok := CompareSemver(p.To, p.To); !ok {
		return fail("bad_plan")
	}
	for _, f := range p.Files {
		if !filepath.IsAbs(f.Path) || !hexSHA256.MatchString(f.SHA256) || f.Size <= 0 {
			return fail("bad_plan")
		}
	}
	if !filepath.IsAbs(p.Core) {
		return fail("bad_plan")
	}
	for _, x := range []string{p.Relaunch, p.Result} {
		if x != "" && !filepath.IsAbs(x) {
			return fail("bad_plan")
		}
	}
	return nil
}

// WriteJSON은 v를 path에 원자적으로 쓴다(0600, 폴더 0700).
func WriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

// ReadJSON은 64KiB 이하 일반 파일을 읽는다.
func ReadJSON(path string, v any) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Size() > 64<<10 {
		return errors.New("update: not a small regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Outcome은 업데이트 도우미가 남기는 결과다. 코드·버전·시각만 담는다(경로·출력 원문 없음).
type Outcome struct {
	Schema     int       `json:"schema"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Code       string    `json:"code"`
	FinishedAt time.Time `json:"finished_at"`
}

// CopyFile은 src를 dst(0755)로 복사한다(업데이트 도우미 준비).
func CopyFile(src, dst string) error { return copyFile(src, dst) }
