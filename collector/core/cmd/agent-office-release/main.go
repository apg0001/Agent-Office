// Command agent-office-release는 Collector 업데이트 manifest 도구다(#44). 관리자만 쓴다.
//
//	agent-office-release keygen <비밀 키 파일>                     공개 키(base64)를 출력하고 비밀 키를 0600으로 저장
//	agent-office-release manifest <dist 디렉터리> <버전> <채널> <기준 URL>   SHA256SUMS로 manifest.json 생성
//	                                                                      (Core 압축본 artifacts + 설치 파일 installers, #180)
//	agent-office-release sign <비밀 키 파일> <manifest.json>           manifest.json.sig 생성
//
// 공개 키는 Collector 빌드에 `-ldflags "-X github.com/apg0001/Agent-Office/collector/core/internal/update.TrustedKeys=<공개 키>"`로 넣는다.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/update"
)

var (
	artifactName = regexp.MustCompile(`^agent-office-collector-(.+)-(linux|darwin|windows)-(amd64|arm64)\.(tar\.gz|zip)$`)
	// 설치 파일(#180). deb·rpm 파일 이름의 버전은 SemVer의 첫 `-`가 `.`이다(build.sh가 `~`를 `.`으로 바꿈).
	nsisName = regexp.MustCompile(`^agent-office-collector-setup-(.+)-windows-(amd64|arm64)\.exe$`)
	pkgName  = regexp.MustCompile(`^agent-office-collector-(.+)-macos-universal\.pkg$`)
	debName  = regexp.MustCompile(`^(agent-office-collector(?:-desktop)?)_(.+)_(amd64|arm64)\.deb$`)
	rpmName  = regexp.MustCompile(`^(agent-office-collector(?:-desktop)?)-(.+)-1\.(x86_64|aarch64)\.rpm$`)
)

// installerFor는 파일 이름이 이 버전의 설치 파일이면 manifest 항목(URL·해시·크기 제외)을 돌려준다.
func installerFor(name, version string) (update.Installer, bool) {
	pkgVersion := strings.Replace(version, "-", ".", 1)
	if m := nsisName.FindStringSubmatch(name); m != nil && m[1] == version {
		return update.Installer{OS: "windows", Arch: m[2], Kind: update.KindNSIS}, true
	}
	if m := pkgName.FindStringSubmatch(name); m != nil && m[1] == version {
		return update.Installer{OS: "darwin", Arch: update.ArchUniversal, Kind: update.KindPkg}, true
	}
	if m := debName.FindStringSubmatch(name); m != nil && m[2] == pkgVersion {
		return update.Installer{OS: "linux", Arch: m[3], Kind: update.KindDeb, Package: m[1]}, true
	}
	if m := rpmName.FindStringSubmatch(name); m != nil && m[2] == pkgVersion {
		arch := map[string]string{"x86_64": "amd64", "aarch64": "arm64"}[m[3]]
		return update.Installer{OS: "linux", Arch: arch, Kind: update.KindRPM, Package: m[1]}, true
	}
	return update.Installer{}, false
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("사용법: keygen | manifest | sign (main.go 주석 참고)")
	}
	switch {
	case args[0] == "keygen" && len(args) == 2:
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[1], []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return nil
	case args[0] == "manifest" && len(args) == 5:
		return manifest(args[1], args[2], args[3], strings.TrimRight(args[4], "/"))
	case args[0] == "sign" && len(args) == 3:
		raw, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) != ed25519.PrivateKeySize {
			return fmt.Errorf("비밀 키 형식이 아닙니다")
		}
		data, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(key), data))
		return os.WriteFile(args[2]+".sig", []byte(sig), 0o644)
	}
	return fmt.Errorf("사용법: keygen | manifest | sign (main.go 주석 참고)")
}

func manifest(dist, version, channel, base string) error {
	if channel != "stable" && channel != "beta" {
		return fmt.Errorf("채널은 stable 또는 beta")
	}
	if _, ok := update.CompareSemver(version, version); !ok {
		return fmt.Errorf("SemVer가 아닙니다: %s", version)
	}
	sums, err := os.Open(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		return err
	}
	defer sums.Close()
	m := update.Manifest{Schema: update.ManifestSchema, Channel: channel, Version: version, PublishedAt: time.Now().UTC()}
	scanner := bufio.NewScanner(sums)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		if in, ok := installerFor(fields[1], version); ok {
			info, err := os.Stat(filepath.Join(dist, fields[1]))
			if err != nil {
				return err
			}
			in.URL, in.SHA256, in.Size = base+"/"+fields[1], fields[0], info.Size()
			m.Installers = append(m.Installers, in)
			continue
		}
		match := artifactName.FindStringSubmatch(fields[1])
		if match == nil || match[1] != version {
			continue
		}
		info, err := os.Stat(filepath.Join(dist, fields[1]))
		if err != nil {
			return err
		}
		m.Artifacts = append(m.Artifacts, update.Artifact{
			OS: match[2], Arch: match[3], URL: base + "/" + fields[1],
			SHA256: fields[0], Size: info.Size(), Format: match[4],
		})
	}
	if len(m.Artifacts) == 0 && len(m.Installers) == 0 {
		return fmt.Errorf("SHA256SUMS에 이 버전의 배포물이 없습니다")
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dist, "manifest.json"), append(data, '\n'), 0o644)
}
