// Package config는 Collector 등록 정보(서버 주소·장치 ID·장치 토큰)와 일시중지 상태를 저장한다(#34).
//
// 파일은 0600으로 원자적으로 쓴다. 장치 토큰은 Secret 타입이라 fmt·로그로 출력해도 가려진다.
// 장치 토큰은 OS 보안 저장소(Keychain·Credential Manager·Secret Service)에 두고, 저장소를 쓸 수 없을
// 때만 이 파일에 둔다(token_store, #41). 자세한 규칙은 token.go.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/fsutil"
)

// SchemaVersion은 config.json 형식 버전이다.
const SchemaVersion = 1

// maxFileBytes는 config·pause 파일 읽기 상한이다.
const maxFileBytes = 64 << 10

var (
	// ErrNotRegistered는 config.json이 없을 때 반환한다.
	ErrNotRegistered = errors.New("config: collector is not registered")
	// ErrInvalid는 파일 형식·버전·값이 올바르지 않을 때 반환한다(파일 내용은 담지 않음).
	ErrInvalid = errors.New("config: invalid configuration file")
	// ErrInsecureURL은 루프백이 아닌 http 서버 주소다.
	ErrInsecureURL = errors.New("config: server url must use https (http is allowed only for loopback)")
)

// Secret은 출력 시 가려지는 문자열이다. JSON 직렬화에서는 값 그대로 저장된다(파일 0600).
type Secret string

func (Secret) String() string   { return "[redacted]" }
func (Secret) GoString() string { return "[redacted]" }

// Config는 등록 결과다.
type Config struct {
	SchemaVersion int    `json:"schema_version"`
	ServerURL     string `json:"server_url"`
	DeviceID      string `json:"device_id"`
	// DeviceToken은 TokenStore가 file일 때만 파일에 있다. keyring이면 비어 있고 OS 보안 저장소에 있다.
	DeviceToken Secret `json:"device_token,omitempty"`
	// TokenStore는 keyring / file이다. 비어 있으면 #41 이전 형식(파일 보관)이며 file과 같다.
	TokenStore   string    `json:"token_store,omitempty"`
	DeviceName   string    `json:"device_name"`
	RegisteredAt time.Time `json:"registered_at"`
	// ClaudeProjectsDir가 비어 있으면 OS 기본 위치(paths.Dirs.ClaudeProjects)를 감시한다.
	ClaudeProjectsDir string `json:"claude_projects_dir,omitempty"`
	// ClaudeVersion·ClaudeMode는 transcript 런타임 검증 수준 판정용이다(#31 버전 감지 전까지 수동).
	// 비어 있으면 미검증(unverified) 런타임으로 보고 confidence를 inferred로 낮춘다.
	ClaudeVersion string `json:"claude_version,omitempty"`
	ClaudeMode    string `json:"claude_mode,omitempty"`
	// ShareWorkspaceName은 이 PC에서 작업 폴더의 마지막 이름(basename)을 서버에 보낼지다(#181).
	// 값이 없으면 켜짐(기본, #221), `settings workspace-name off`로 끈 PC만 false가 저장된다. 꺼져 있으면
	// Hook은 payload의 cwd를 읽지 않는다. 전체 경로·상위 폴더는 켜져 있어도 보내지 않는다. Hook 프로세스가
	// 매번 읽으므로 run 재시작이 필요 없다(Same 비교 대상 아님). WorkspaceNameSharing으로 읽는다.
	ShareWorkspaceName *bool `json:"share_workspace_name,omitempty"`
}

// WorkspaceNameSharing은 작업 폴더 이름을 보낼지다. 명시적으로 끄지 않았으면 켜짐이다(#221).
func (c Config) WorkspaceNameSharing() bool {
	return c.ShareWorkspaceName == nil || *c.ShareWorkspaceName
}

// Load는 config.json을 읽는다. 파일이 없으면 ErrNotRegistered다.
func Load(path string) (Config, error) {
	data, err := readSmall(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, ErrNotRegistered
	}
	if err != nil {
		return Config{}, ErrInvalid
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, ErrInvalid
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save는 config.json을 0600으로 원자적으로 쓴다.
func Save(path string, c Config) error {
	c.SchemaVersion = SchemaVersion
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return ErrInvalid
	}
	return fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// Validate는 값 형식을 확인한다.
func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return ErrInvalid
	}
	if _, err := NormalizeServerURL(c.ServerURL); err != nil {
		return err
	}
	if !isUUID(c.DeviceID) || len(c.DeviceToken) > 512 {
		return ErrInvalid
	}
	switch c.TokenStore {
	case "", TokenStoreFile:
		if c.DeviceToken == "" {
			return ErrInvalid
		}
	case TokenStoreKeyring:
		if c.DeviceToken != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Same은 서버·장치·토큰이 같은지 비교한다(실행 루프의 재적재 판단).
func (c Config) Same(o Config) bool {
	return c.ServerURL == o.ServerURL && c.DeviceID == o.DeviceID && c.DeviceToken == o.DeviceToken &&
		c.storeKind() == o.storeKind() &&
		c.ClaudeProjectsDir == o.ClaudeProjectsDir && c.ClaudeVersion == o.ClaudeVersion && c.ClaudeMode == o.ClaudeMode
}

// NormalizeServerURL은 서버 origin을 검증하고 끝의 `/`를 지운다. 경로·쿼리·사용자 정보는 거부한다.
func NormalizeServerURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalid
	}
	if p := strings.TrimRight(u.Path, "/"); p != "" {
		return "", ErrInvalid
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return "", ErrInsecureURL
		}
	default:
		return "", ErrInvalid
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.")
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxFileBytes {
		return nil, ErrInvalid
	}
	buf := make([]byte, fi.Size())
	n, err := f.Read(buf)
	if err != nil && fi.Size() > 0 {
		return nil, err
	}
	return buf[:n], nil
}

// Pause는 수집 일시중지 상태다. 일시중지 중에는 transcript tail·Hook spool·전송·heartbeat를 모두 멈춘다.
type Pause struct {
	Paused    bool      `json:"paused"`
	ChangedAt time.Time `json:"changed_at"`
	// SkipBacklog는 재개 직후 일시중지 동안 쌓인 transcript 줄을 수집하지 않고 건너뛰라는 표시다.
	// `resume`이 켜고, 실행 루프가 건너뛰기를 마치면 끈다.
	SkipBacklog bool `json:"skip_backlog,omitempty"`
}

// LoadPause는 pause.json을 읽는다. 없으면 일시중지 아님이다. 읽을 수 없으면 안전하게 일시중지로 본다.
func LoadPause(path string) (Pause, error) {
	data, err := readSmall(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Pause{}, nil
	}
	if err != nil {
		return Pause{Paused: true}, ErrInvalid
	}
	var p Pause
	if err := json.Unmarshal(data, &p); err != nil {
		return Pause{Paused: true}, ErrInvalid
	}
	return p, nil
}

// SavePause는 일시중지 상태를 원자적으로 쓴다. 재개(paused=false)는 SkipBacklog를 켠다.
func SavePause(path string, paused bool, now time.Time) error {
	return WritePause(path, Pause{Paused: paused, ChangedAt: now.UTC(), SkipBacklog: !paused})
}

// WritePause는 상태를 그대로 쓴다.
func WritePause(path string, p Pause) error {
	data, _ := json.Marshal(p)
	return fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600)
}
