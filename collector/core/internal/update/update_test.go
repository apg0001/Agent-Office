package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.10.0", "1.9.0", 1},
		{"1.0.0", "1.0.0-beta.1", 1},
		{"1.0.0-beta.2", "1.0.0-beta.10", -1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-beta.1", "1.0.0-beta", 1},
		{"1.0.0-1", "1.0.0-alpha", -1},
	}
	for _, c := range cases {
		got, ok := CompareSemver(c.a, c.b)
		if !ok || got != c.want {
			t.Errorf("Compare(%s,%s)=%d,%v want %d", c.a, c.b, got, ok, c.want)
		}
	}
	if _, ok := CompareSemver("v1.0", "1.0.0"); ok {
		t.Fatal("invalid semver accepted")
	}
}

type release struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newRelease(t *testing.T) release {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return release{pub, priv}
}

func (r release) sign(data []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(r.priv, data))
}

func TestVerifySignature(t *testing.T) {
	r, other := newRelease(t), newRelease(t)
	m, _ := json.Marshal(Manifest{Schema: 1, Channel: "beta", Version: "0.2.0"})
	if _, err := Verify(m, r.sign(m), []ed25519.PublicKey{other.pub, r.pub}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		data []byte
		sig  string
		keys []ed25519.PublicKey
	}{
		"no_trusted_key": {m, r.sign(m), nil},
		"bad_signature":  {m, other.sign(m), []ed25519.PublicKey{r.pub}},
	}
	for code, c := range cases {
		if _, err := Verify(c.data, c.sig, c.keys); Code(err) != code {
			t.Errorf("%s: got %v", code, err)
		}
	}
	tampered := bytes.Replace(m, []byte("0.2.0"), []byte("9.9.9"), 1)
	if _, err := Verify(tampered, r.sign(m), []ed25519.PublicKey{r.pub}); Code(err) != "bad_signature" {
		t.Fatalf("tampered manifest: %v", err)
	}
	if keys := ParseKeys(base64.StdEncoding.EncodeToString(r.pub) + ", junk ,"); len(keys) != 1 {
		t.Fatalf("keys %d", len(keys))
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

type server struct {
	*httptest.Server
	files map[string][]byte
}

func newServer(t *testing.T) *server {
	s := &server{files: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := s.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) publish(t *testing.T, r release, m Manifest) {
	data, _ := json.Marshal(m)
	s.files["/manifest.json"] = data
	s.files["/manifest.json.sig"] = []byte(r.sign(data))
}

func options(s *server, r release) Options {
	return Options{
		ManifestURL: s.URL + "/manifest.json", Channel: "beta", Current: "0.1.0-beta.1",
		GOOS: "linux", GOARCH: "arm64", Keys: []ed25519.PublicKey{r.pub}, Client: s.Client(),
		AllowLoopbackHTTP: true,
	}
}

func artifactFor(s *server, data []byte) Artifact {
	sum := sha256.Sum256(data)
	s.files["/a.tar.gz"] = data
	return Artifact{OS: "linux", Arch: "arm64", URL: s.URL + "/a.tar.gz", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), Format: "tar.gz"}
}

func TestCheckAndDownload(t *testing.T) {
	r, s := newRelease(t), newServer(t)
	archive := tarGz(t, map[string]string{"pkg/agent-office-collector": "#!new", "pkg/README.txt": "r"})
	a := artifactFor(s, archive)
	s.publish(t, r, Manifest{Schema: 1, Channel: "beta", Version: "0.2.0", PublishedAt: time.Now(), Artifacts: []Artifact{a}})
	ctx := context.Background()

	avail, err := Check(ctx, options(s, r))
	if err != nil || avail == nil || avail.Version != "0.2.0" {
		t.Fatalf("check %+v %v", avail, err)
	}
	dir := t.TempDir()
	path, err := Download(ctx, options(s, r), avail.Artifact, dir)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := Extract(path, "tar.gz", "linux", bin); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(bin); string(got) != "#!new" {
		t.Fatalf("extracted %q", got)
	}

	o := options(s, r)
	o.Current = "0.2.0"
	if avail, err := Check(ctx, o); err != nil || avail != nil {
		t.Fatalf("same version: %+v %v", avail, err)
	}
	o = options(s, r)
	o.Channel = "stable"
	if _, err := Check(ctx, o); Code(err) != "channel_mismatch" {
		t.Fatalf("channel: %v", err)
	}
	o = options(s, r)
	o.GOOS = "windows"
	if _, err := Check(ctx, o); Code(err) != "no_artifact_for_platform" {
		t.Fatalf("platform: %v", err)
	}
	o = options(s, r)
	o.AllowLoopbackHTTP = false
	if _, err := Check(ctx, o); Code(err) != "insecure_url" {
		t.Fatalf("http: %v", err)
	}
	bad := a
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := Download(ctx, options(s, r), bad, dir); Code(err) != "checksum_mismatch" {
		t.Fatalf("checksum: %v", err)
	}
}

func TestExtractRejectsTraversalAndFindsZip(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "evil.tar.gz")
	os.WriteFile(evil, tarGz(t, map[string]string{"../agent-office-collector": "x"}), 0o600)
	if err := Extract(evil, "tar.gz", "linux", filepath.Join(dir, "out")); Code(err) != "binary_not_found" {
		t.Fatalf("traversal: %v", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("pkg/agent-office-collector.exe")
	w.Write([]byte("MZnew"))
	zw.Close()
	z := filepath.Join(dir, "a.zip")
	os.WriteFile(z, buf.Bytes(), 0o600)
	out := filepath.Join(dir, "bin.exe")
	if err := Extract(z, "zip", "windows", out); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "MZnew" {
		t.Fatalf("zip %q", got)
	}
}

func setupBinaries(t *testing.T) (current, next string) {
	dir := t.TempDir()
	current = filepath.Join(dir, "agent-office-collector")
	next = filepath.Join(dir, "download", "agent-office-collector")
	os.MkdirAll(filepath.Dir(next), 0o700)
	os.WriteFile(current, []byte("old"), 0o755)
	os.WriteFile(next, []byte("new"), 0o755)
	return current, next
}

func versionOf(v string) func(string) (string, error) {
	return func(string) (string, error) { return "agent-office-collector " + v + " (linux/arm64)\n", nil }
}

func TestApplySwapsAndKeepsBackup(t *testing.T) {
	current, next := setupBinaries(t)
	restarts := 0
	err := Apply(current, next, "0.2.0", Hooks{
		Version: versionOf("0.2.0"),
		Restart: func() error { restarts++; return nil },
		Healthy: func() error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(current); string(got) != "new" {
		t.Fatalf("current %q", got)
	}
	if got, _ := os.ReadFile(current + ".bak"); string(got) != "old" {
		t.Fatalf("backup %q", got)
	}
	if restarts != 1 {
		t.Fatalf("restarts %d", restarts)
	}
}

func TestApplyRollsBackOnUnhealthyRestart(t *testing.T) {
	current, next := setupBinaries(t)
	restarts := 0
	err := Apply(current, next, "0.2.0", Hooks{
		Version: versionOf("0.2.0"),
		Restart: func() error { restarts++; return nil },
		Healthy: func() error { return errors.New("not running") },
	})
	if Code(err) != "rolled_back" {
		t.Fatalf("err %v", err)
	}
	if got, _ := os.ReadFile(current); string(got) != "old" {
		t.Fatalf("not restored: %q", got)
	}
	if restarts != 2 {
		t.Fatalf("restarts %d (new then old)", restarts)
	}
}

func TestApplyRefusesBadBinaryAndPackageInstalls(t *testing.T) {
	current, next := setupBinaries(t)
	hooks := Hooks{Version: versionOf("0.1.9"), Restart: func() error { return nil }, Healthy: func() error { return nil }}
	if err := Apply(current, next, "0.2.0", hooks); Code(err) != "new_binary_invalid" {
		t.Fatalf("version mismatch: %v", err)
	}
	if got, _ := os.ReadFile(current); string(got) != "old" {
		t.Fatal("current changed on refusal")
	}
	if err := Apply("/usr/bin/agent-office-collector", next, "0.2.0", hooks); Code(err) != "managed_by_package" {
		t.Fatalf("package: %v", err)
	}
	if err := Apply("/Applications/Agent Office Collector.app/Contents/MacOS/agent-office-collector", next, "0.2.0", hooks); Code(err) != "managed_by_package" {
		t.Fatalf("app bundle: %v", err)
	}
	// 설치 프로그램 배치(트레이 도우미가 옆에 있음)도 설치 프로그램으로 업데이트한다.
	if err := os.WriteFile(filepath.Join(filepath.Dir(current), "agent-office-collector-tray.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(current, next, "0.2.0", hooks); Code(err) != "managed_by_package" {
		t.Fatalf("installer layout: %v", err)
	}
}

// GitHub Release 다운로드는 302로 저장소 주소를 돌려준다. CLI 공통 클라이언트처럼 리다이렉트를
// 막는 클라이언트를 넘겨도 업데이트는 따라가되, 각 단계가 안전한 주소인지 다시 검사한다.
func TestGetFollowsSafeRedirectsOnly(t *testing.T) {
	r, s := newRelease(t), newServer(t)
	a := artifactFor(s, []byte("bin"))
	s.publish(t, r, Manifest{Schema: 1, Channel: "beta", Version: "0.2.0", PublishedAt: time.Now(), Artifacts: []Artifact{a}})
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/loop":
			http.Redirect(w, req, "/loop", http.StatusFound)
		case "/insecure":
			http.Redirect(w, req, "http://example.invalid/manifest.json", http.StatusFound)
		default:
			http.Redirect(w, req, s.URL+req.URL.Path, http.StatusFound)
		}
	}))
	t.Cleanup(redir.Close)
	o := options(s, r)
	o.Client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	o.ManifestURL = redir.URL + "/manifest.json"
	if avail, err := Check(context.Background(), o); err != nil || avail == nil || avail.Version != "0.2.0" {
		t.Fatalf("redirected manifest: %+v %v", avail, err)
	}
	for path, code := range map[string]string{"/loop": "too_many_redirects", "/insecure": "insecure_url"} {
		_, err := o.get(context.Background(), redir.URL+path, 1<<10)
		var e *Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("%s: %v", path, err)
		}
	}
}
