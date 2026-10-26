// Package prefs는 GUI 자체 설정(gui.json)이다. 서버 주소·PC 이름 초안, 첫 실행 마법사 상태, 업데이트
// 설정(#180)만 담는다.
// 장치 토큰·등록 코드·계정 비밀번호는 저장하지 않는다(토큰은 Core 설정 파일 소관).
package prefs

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// SchemaVersion은 gui.json 형식 버전이다.
const SchemaVersion = 1

// EnvHome은 Core와 같은 재정의 변수다. 있으면 `$AGENT_OFFICE_COLLECTOR_HOME/gui/gui.json`.
const EnvHome = "AGENT_OFFICE_COLLECTOR_HOME"

// Prefs는 GUI 설정이다.
type Prefs struct {
	SchemaVersion int    `json:"schema_version"`
	ServerURL     string `json:"server_url,omitempty"`
	DeviceName    string `json:"device_name,omitempty"`
	// Setup은 첫 실행 마법사 상태다: ""(아직) / done / dismissed(나중에).
	Setup string `json:"setup,omitempty"`
	// UpdateMode는 notify(기본, 알림 후 승인) / download(자동 다운로드 후 승인) / auto(자동 적용) / manual(수동 확인만)이다.
	UpdateMode string `json:"update_mode,omitempty"`
	// UpdateChannel은 ""(Core 버전으로 자동: 사전 배포면 beta) / stable / beta다.
	UpdateChannel string `json:"update_channel,omitempty"`
	// LastVersion은 트레이가 마지막으로 후처리를 마친 Core 버전이다(설치·업데이트 감지).
	LastVersion string `json:"last_version,omitempty"`
}

// 업데이트 모드(명세 §10: 기본은 알림 후 업데이트 버튼).
const (
	UpdateNotify   = "notify"
	UpdateDownload = "download"
	UpdateAuto     = "auto"
	UpdateManual   = "manual"
)

// Mode는 저장된 업데이트 모드다. 비었거나 모르는 값이면 기본(notify).
func (p Prefs) Mode() string {
	switch p.UpdateMode {
	case UpdateNotify, UpdateDownload, UpdateAuto, UpdateManual:
		return p.UpdateMode
	}
	return UpdateNotify
}

// ValidMode는 저장할 수 있는 모드인지다.
func ValidMode(m string) bool {
	return m == UpdateNotify || m == UpdateDownload || m == UpdateAuto || m == UpdateManual
}

// ValidChannel은 저장할 수 있는 채널 값인지다(""은 자동).
func ValidChannel(c string) bool { return c == "" || c == "stable" || c == "beta" }

// ErrInvalid: 파일을 읽었지만 형식이 틀렸다.
var ErrInvalid = errors.New("prefs: invalid file")

// Path는 OS 사용자 설정 위치의 gui.json이다.
func Path(getenv func(string) string, userConfigDir func() (string, error)) (string, error) {
	if h := getenv(EnvHome); h != "" {
		return filepath.Join(h, "gui", "gui.json"), nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AgentOffice", "CollectorGUI", "gui.json"), nil
}

// Load는 설정을 읽는다. 파일이 없으면 빈 설정과 nil을 돌려준다.
func Load(path string) (Prefs, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Prefs{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return Prefs{SchemaVersion: SchemaVersion}, err
	}
	if !fi.Mode().IsRegular() {
		return Prefs{SchemaVersion: SchemaVersion}, ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return Prefs{SchemaVersion: SchemaVersion}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return Prefs{SchemaVersion: SchemaVersion}, err
	}
	var p Prefs
	if json.Unmarshal(data, &p) != nil || p.SchemaVersion != SchemaVersion {
		return Prefs{SchemaVersion: SchemaVersion}, ErrInvalid
	}
	return p, nil
}

// Save는 같은 디렉터리 임시 파일 → fsync → rename으로 원자적으로 쓴다(디렉터리 0700, 파일 0600).
// 대상이 심볼릭 링크 등 일반 파일이 아니면 쓰지 않는다.
func Save(path string, p Prefs) error {
	p.SchemaVersion = SchemaVersion
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return ErrInvalid
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gui.json.tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
