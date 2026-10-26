package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

func TestManifestIncludesInstallers(t *testing.T) {
	dist := t.TempDir()
	const v = "0.2.0-beta.3"
	names := []string{
		"agent-office-collector-0.2.0-beta.3-linux-arm64.tar.gz",
		"agent-office-collector-setup-0.2.0-beta.3-windows-amd64.exe",
		"agent-office-collector-0.2.0-beta.3-macos-universal.pkg",
		"agent-office-collector_0.2.0.beta.3_arm64.deb",
		"agent-office-collector-desktop_0.2.0.beta.3_arm64.deb",
		"agent-office-collector-desktop-0.2.0.beta.3-1.x86_64.rpm",
		"agent-office-collector-0.2.0.beta.3-1.x86_64.rpm",
		"agent-office-collector-desktop_0.1.0.beta.1_arm64.deb", // 다른 버전은 넣지 않는다
	}
	sums := ""
	for _, n := range names {
		os.WriteFile(filepath.Join(dist, n), []byte(n), 0o644)
		s := sha256.Sum256([]byte(n))
		sums += fmt.Sprintf("%s  %s\n", hex.EncodeToString(s[:]), n)
	}
	os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(sums), 0o644)
	if err := manifest(dist, v, "beta", "https://example.invalid/r"); err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	data, _ := os.ReadFile(filepath.Join(dist, "manifest.json"))
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Artifacts) != 1 || len(m.Installers) != 6 {
		t.Fatalf("artifacts %d installers %d", len(m.Artifacts), len(m.Installers))
	}
	for _, c := range []struct{ goos, arch, kind string }{
		{"windows", "amd64", update.KindNSIS}, {"darwin", "arm64", update.KindPkg}, {"darwin", "amd64", update.KindPkg},
		{"linux", "arm64", update.KindDeb}, {"linux", "amd64", update.KindRPM},
	} {
		if _, err := update.SelectInstallers(m, c.goos, c.arch, c.kind); err != nil {
			t.Fatalf("%v: %v", c, err)
		}
	}
}
