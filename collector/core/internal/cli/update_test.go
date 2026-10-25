package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"strings"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

// 새 버전 바이너리 역할의 스크립트. `version`만 답한다.
func fakeBinary(version string) string {
	return "#!/bin/sh\necho \"agent-office-collector " + version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")\"\n"
}

func updateServer(t *testing.T, version string) (*httptest.Server, ed25519.PublicKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := fakeBinary(version)
	tw.WriteHeader(&tar.Header{Name: "pkg/agent-office-collector", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	files := map[string][]byte{"/a.tar.gz": archive.Bytes()}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive.Bytes())
	manifest, _ := json.Marshal(update.Manifest{
		Schema: 1, Channel: "beta", Version: version,
		Artifacts: []update.Artifact{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, URL: srv.URL + "/a.tar.gz",
			SHA256: hex.EncodeToString(sum[:]), Size: int64(archive.Len()), Format: "tar.gz",
		}},
	})
	files["/manifest.json"] = manifest
	files["/manifest.json.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, manifest)))
	return srv, pub
}

func TestUpdateCheckAndApply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("셸 스크립트로 새 바이너리를 흉내 낸다")
	}
	srv, pub := updateServer(t, "0.2.0")
	previous := update.TrustedKeys
	update.TrustedKeys = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { update.TrustedKeys = previous })

	te := newTestEnv(t, "update")
	te.env.Version = "0.1.0-beta.1"
	te.env.HTTP = srv.Client()
	exe := filepath.Join(t.TempDir(), "agent-office-collector")
	os.WriteFile(exe, []byte(fakeBinary("0.1.0-beta.1")), 0o755)
	te.env.Executable = func() (string, error) { return exe, nil }
	url := srv.URL + "/manifest.json"

	if code := te.run("", "update", "check", "--channel", "beta", "--manifest-url", url); code != ExitOK {
		t.Fatalf("check %d %s", code, te.stderr)
	}
	if !strings.Contains(te.stdout.String(), "새 버전 0.2.0") {
		t.Fatalf("stdout %s", te.stdout)
	}
	// --yes 없이는 적용하지 않는다(알림 후 사용자가 적용).
	if code := te.run("", "update", "apply", "--channel", "beta", "--manifest-url", url); code != ExitOK {
		t.Fatal(code)
	}
	if got, _ := os.ReadFile(exe); !strings.Contains(string(got), "0.1.0-beta.1") {
		t.Fatal("applied without --yes")
	}
	if code := te.run("", "update", "apply", "--channel", "beta", "--manifest-url", url, "--yes"); code != ExitOK {
		t.Fatalf("apply %d %s", code, te.stderr)
	}
	if got, _ := os.ReadFile(exe); !strings.Contains(string(got), "0.2.0") {
		t.Fatalf("not updated: %s", got)
	}
	if _, err := os.Stat(exe + ".bak"); err != nil {
		t.Fatal("backup missing")
	}
	te.env.Version = "0.2.0"
	if code := te.run("", "update", "check", "--channel", "beta", "--manifest-url", url); code != ExitOK || !strings.Contains(te.stdout.String(), "최신 버전") {
		t.Fatalf("after update: %d %s", code, te.stdout)
	}
}

func TestUpdateRefusesWithoutTrustedKey(t *testing.T) {
	srv, _ := updateServer(t, "0.2.0")
	previous := update.TrustedKeys
	update.TrustedKeys = ""
	t.Cleanup(func() { update.TrustedKeys = previous })
	te := newTestEnv(t, "nokey")
	te.env.HTTP = srv.Client()
	code := te.run("", "update", "check", "--channel", "beta", "--manifest-url", srv.URL+"/manifest.json")
	if code != ExitError || !strings.Contains(te.stderr.String(), "no_trusted_key") {
		t.Fatalf("%d %s", code, te.stderr)
	}
	if strings.Contains(te.stderr.String(), srv.URL) {
		t.Fatal("URL leaked in error")
	}
}
