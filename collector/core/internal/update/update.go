// Package update는 Collector 자체 업데이트를 다룬다(#44).
//
// 서명된 manifest(ed25519)를 받아 채널·OS·아키텍처·버전을 확인하고, 배포물을 받아 SHA-256을
// 검사한 뒤 바이너리만 꺼내 같은 디렉터리에서 원자적으로 바꾼다. 새 바이너리가 실행되지 않거나
// 재시작 후 상태 확인에 실패하면 이전 바이너리로 되돌린다. 신뢰할 공개 키는 빌드 때
// `-ldflags "-X .../update.TrustedKeys=<base64>[,<base64>]"`로 넣으며, 키가 없으면 업데이트하지 않는다.
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TrustedKeys는 쉼표로 구분한 ed25519 공개 키(base64)다. 릴리스 빌드가 채운다.
var TrustedKeys = ""

const (
	ManifestSchema   = 1
	MaxManifestBytes = 256 << 10
	MaxArtifactBytes = 200 << 20
)

// Error는 원인 코드만 담는다(URL·경로·응답 본문은 담지 않음).
type Error struct{ Code string }

func (e *Error) Error() string { return "update: " + e.Code }

func fail(code string) error { return &Error{Code: code} }

// Code는 err가 update 오류면 코드를, 아니면 "update_failed"를 돌려준다.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "update_failed"
}

type Artifact struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Format string `json:"format"`
}

type Manifest struct {
	Schema      int        `json:"schema"`
	Channel     string     `json:"channel"`
	Version     string     `json:"version"`
	PublishedAt time.Time  `json:"published_at"`
	Artifacts   []Artifact `json:"artifacts"`
}

// ParseKeys는 쉼표로 구분한 base64 공개 키를 읽는다. 잘못된 키는 버린다.
func ParseKeys(value string) []ed25519.PublicKey {
	var keys []ed25519.PublicKey
	for _, part := range strings.Split(value, ",") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part))
		if err == nil && len(raw) == ed25519.PublicKeySize {
			keys = append(keys, ed25519.PublicKey(raw))
		}
	}
	return keys
}

var semverPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)

// CompareSemver는 SemVer 2.0 우선순위로 a<b면 -1, 같으면 0, a>b면 1. 형식이 틀리면 ok=false.
func CompareSemver(a, b string) (result int, ok bool) {
	ma, mb := semverPattern.FindStringSubmatch(a), semverPattern.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return 0, false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return sign(x - y), true
		}
	}
	pa, pb := ma[4], mb[4]
	switch {
	case pa == pb:
		return 0, true
	case pa == "":
		return 1, true
	case pb == "":
		return -1, true
	}
	ia, ib := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(ia) && i < len(ib); i++ {
		na, ea := strconv.Atoi(ia[i])
		nb, eb := strconv.Atoi(ib[i])
		switch {
		case ea == nil && eb == nil && na != nb:
			return sign(na - nb), true
		case ea == nil && eb != nil:
			return -1, true
		case ea != nil && eb == nil:
			return 1, true
		case ia[i] != ib[i]:
			return sign(strings.Compare(ia[i], ib[i])), true
		}
	}
	return sign(len(ia) - len(ib)), true
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// Verify는 서명을 검사하고 manifest를 읽는다. 서명은 manifest 바이트 그대로에 대한 ed25519다.
func Verify(manifest []byte, signature string, keys []ed25519.PublicKey) (Manifest, error) {
	if len(keys) == 0 {
		return Manifest{}, fail("no_trusted_key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return Manifest{}, fail("bad_signature")
	}
	trusted := false
	for _, key := range keys {
		if ed25519.Verify(key, manifest, sig) {
			trusted = true
			break
		}
	}
	if !trusted {
		return Manifest{}, fail("bad_signature")
	}
	var m Manifest
	if err := json.Unmarshal(manifest, &m); err != nil || m.Schema != ManifestSchema {
		return Manifest{}, fail("bad_manifest")
	}
	if _, ok := CompareSemver(m.Version, m.Version); !ok {
		return Manifest{}, fail("bad_manifest")
	}
	return m, nil
}

// Options는 업데이트 확인 설정이다.
type Options struct {
	ManifestURL string
	Channel     string
	Current     string
	GOOS        string
	GOARCH      string
	Keys        []ed25519.PublicKey
	Client      *http.Client
	// AllowLoopbackHTTP는 테스트용으로 127.0.0.1의 http를 허용한다.
	AllowLoopbackHTTP bool
}

// Available은 설치할 수 있는 새 버전이다.
type Available struct {
	Version  string
	Artifact Artifact
}

func (o Options) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fail("bad_url")
	}
	if u.Scheme == "https" {
		return nil
	}
	if o.AllowLoopbackHTTP && u.Scheme == "http" && u.Hostname() == "127.0.0.1" {
		return nil
	}
	return fail("insecure_url")
}

// maxRedirects는 업데이트 다운로드가 따라갈 최대 리다이렉트 수다.
const maxRedirects = 5

// redirectingClient는 o.Client 설정을 쓰되 리다이렉트를 따라가는 복사본이다. GitHub Release
// 다운로드는 저장소 주소로 302를 돌려준다. 공통 클라이언트는 장치 토큰이 다른 호스트로 새지 않게
// 리다이렉트를 막지만, 업데이트 요청에는 인증 헤더가 없다. 각 단계 주소도 https(또는 허용한
// 루프백)인지 다시 검사한다.
func (o Options) redirectingClient() *http.Client {
	c := http.Client{}
	if o.Client != nil {
		c = *o.Client
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fail("too_many_redirects")
		}
		return o.checkURL(req.URL.String())
	}
	return &c
}

func (o Options) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if err := o.checkURL(raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, fail("bad_url")
	}
	resp, err := o.redirectingClient().Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			var e *Error
			if errors.As(uerr.Err, &e) {
				return nil, e
			}
		}
		return nil, fail("download_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fail(fmt.Sprintf("http_%d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fail("download_failed")
	}
	if int64(len(data)) > limit {
		return nil, fail("too_large")
	}
	return data, nil
}

// Check는 manifest와 `.sig`를 받아 검증하고, 현재보다 새 버전이 있으면 돌려준다(없으면 nil).
func Check(ctx context.Context, o Options) (*Available, error) {
	manifest, err := o.get(ctx, o.ManifestURL, MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	sig, err := o.get(ctx, o.ManifestURL+".sig", 1024)
	if err != nil {
		return nil, err
	}
	m, err := Verify(manifest, string(sig), o.Keys)
	if err != nil {
		return nil, err
	}
	if m.Channel != o.Channel {
		return nil, fail("channel_mismatch")
	}
	cmp, ok := CompareSemver(m.Version, o.Current)
	if !ok {
		return nil, fail("bad_version")
	}
	if cmp <= 0 {
		return nil, nil
	}
	for _, a := range m.Artifacts {
		if a.OS == o.GOOS && a.Arch == o.GOARCH {
			if err := o.checkURL(a.URL); err != nil {
				return nil, err
			}
			if a.Size <= 0 || a.Size > MaxArtifactBytes || len(a.SHA256) != 64 {
				return nil, fail("bad_manifest")
			}
			return &Available{Version: m.Version, Artifact: a}, nil
		}
	}
	return nil, fail("no_artifact_for_platform")
}

// Download는 배포물을 dir에 받아 크기와 SHA-256을 검사한다.
func Download(ctx context.Context, o Options, a Artifact, dir string) (string, error) {
	data, err := o.get(ctx, a.URL, a.Size)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != a.Size || !strings.EqualFold(hex.EncodeToString(sum[:]), a.SHA256) {
		return "", fail("checksum_mismatch")
	}
	path := filepath.Join(dir, "artifact."+strings.ReplaceAll(a.Format, ".", "-"))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fail("write_failed")
	}
	return path, nil
}

// BinaryName은 배포물 안의 Collector 실행 파일 이름이다.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "agent-office-collector.exe"
	}
	return "agent-office-collector"
}

// Extract는 압축본에서 Collector 실행 파일 하나만 dest로 꺼낸다. 링크·경로 탈출은 거부한다.
func Extract(archive, format, goos, dest string) error {
	name := BinaryName(goos)
	write := func(r io.Reader) error {
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return fail("write_failed")
		}
		if _, err := io.Copy(out, io.LimitReader(r, MaxArtifactBytes)); err != nil {
			out.Close()
			return fail("write_failed")
		}
		if err := out.Close(); err != nil {
			return fail("write_failed")
		}
		return nil
	}
	switch format {
	case "tar.gz":
		f, err := os.Open(archive)
		if err != nil {
			return fail("bad_archive")
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fail("bad_archive")
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return fail("binary_not_found")
			}
			if err != nil {
				return fail("bad_archive")
			}
			if h.Typeflag == tar.TypeReg && path(h.Name) == name {
				return write(tr)
			}
		}
	case "zip":
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return fail("bad_archive")
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Mode().IsRegular() && path(f.Name) == name {
				rc, err := f.Open()
				if err != nil {
					return fail("bad_archive")
				}
				defer rc.Close()
				return write(rc)
			}
		}
		return fail("binary_not_found")
	}
	return fail("bad_archive")
}

// path는 압축본 항목의 마지막 이름이다. 상위 디렉터리 이동이 있으면 빈 값.
func path(name string) string {
	clean := strings.ReplaceAll(name, `\`, "/")
	if strings.Contains(clean, "..") || strings.HasPrefix(clean, "/") {
		return ""
	}
	parts := strings.Split(clean, "/")
	return parts[len(parts)-1]
}

// Hooks는 Apply가 바깥과 상호작용하는 부분이다(테스트에서 바꾼다).
type Hooks struct {
	// Version은 staged 실행 파일의 `version` 출력을 돌려준다.
	Version func(exe string) (string, error)
	// Restart는 자동 시작 서비스를 다시 시작한다(서비스가 없으면 nil).
	Restart func() error
	// Healthy는 재시작 뒤 Collector가 정상인지 확인한다.
	Healthy func() error
}

// Apply는 newBinary를 current 자리로 원자적으로 바꾼다. 실패하면 되돌린다.
func Apply(current, newBinary, expectVersion string, h Hooks) error {
	if strings.HasPrefix(current, "/usr/bin/") || strings.HasPrefix(current, "/usr/sbin/") {
		// 패키지 관리자가 관리하는 파일은 패키지로 업데이트한다.
		return fail("managed_by_package")
	}
	out, err := h.Version(newBinary)
	if err != nil || !strings.Contains(out, " "+expectVersion+" ") && !strings.HasSuffix(strings.TrimSpace(out), " "+expectVersion) {
		return fail("new_binary_invalid")
	}
	staged := current + ".new"
	backup := current + ".bak"
	if err := copyFile(newBinary, staged); err != nil {
		return fail("not_writable")
	}
	_ = os.Remove(backup)
	if err := os.Rename(current, backup); err != nil {
		os.Remove(staged)
		return fail("not_writable")
	}
	if err := os.Rename(staged, current); err != nil {
		_ = os.Rename(backup, current)
		return fail("not_writable")
	}
	if err := h.Restart(); err == nil {
		if err := h.Healthy(); err == nil {
			return nil
		}
	}
	// 되돌린다: 이전 바이너리를 제자리로 놓고 다시 시작한다.
	_ = os.Remove(current)
	if err := os.Rename(backup, current); err != nil {
		return fail("rollback_failed")
	}
	_ = h.Restart()
	return fail("rolled_back")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
