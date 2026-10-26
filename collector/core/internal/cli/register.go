package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
)

const registerPath = "/api/v1/devices/register"

var versionPattern = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,64}$`)

// platformOf는 GOOS를 장치 API platform enum으로 바꾼다.
func platformOf(goos string) string {
	switch goos {
	case "windows", "linux":
		return goos
	case "darwin":
		return "macos"
	default:
		return "unknown"
	}
}

// readCodeLine은 표준 입력 첫 줄(최대 64바이트)을 읽는다. 등록 코드는 32자 이하다.
// 줄바꿈에서 멈추므로 입력을 닫지 않아도 된다.
func readCodeLine(r io.Reader) (string, error) {
	var line []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' || one[0] == '\r' {
				return string(line), nil
			}
			if len(line) == 64 {
				return "", errors.New("code line too long")
			}
			line = append(line, one[0])
		}
		if errors.Is(err, io.EOF) {
			return string(line), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func cmdRegister(args []string, env Env) int {
	fs := newFlags("register", env)
	server := fs.String("server", "", "Agent Office 서버 주소(https://...)")
	code := fs.String("code", "", "웹에서 받은 일회용 등록 코드(XXXX-XXXX)")
	name := fs.String("name", "", "이 PC의 표시 이름(1~80자)")
	codeStdin := fs.Bool("code-stdin", false, "등록 코드를 표준 입력 첫 줄에서 읽음(GUI용, 프로세스 목록에 코드가 보이지 않음)")
	force := fs.Bool("force", false, "이미 등록된 경우 새 장치로 다시 등록")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	if *codeStdin {
		if *code != "" {
			fmt.Fprintln(env.Stderr, "--code와 --code-stdin은 함께 쓸 수 없습니다")
			return ExitUsage
		}
		line, err := readCodeLine(env.Stdin)
		if err != nil {
			fmt.Fprintln(env.Stderr, "표준 입력에서 등록 코드를 읽지 못했습니다")
			return ExitUsage
		}
		*code = line
	}
	base, err := config.NormalizeServerURL(*server)
	if err != nil {
		if errors.Is(err, config.ErrInsecureURL) {
			fmt.Fprintln(env.Stderr, "서버 주소는 https여야 합니다(http는 localhost·127.x만 허용)")
		} else {
			fmt.Fprintln(env.Stderr, "서버 주소 형식이 올바르지 않습니다(예: https://office.example)")
		}
		return ExitUsage
	}
	trimmedName := strings.TrimSpace(*name)
	if n := utf8.RuneCountInString(trimmedName); n < 1 || n > 80 {
		fmt.Fprintln(env.Stderr, "--name은 1~80자여야 합니다")
		return ExitUsage
	}
	regCode := strings.TrimSpace(*code)
	if regCode == "" || len(regCode) > 32 {
		fmt.Fprintln(env.Stderr, "--code(또는 --code-stdin)에 웹에서 받은 등록 코드를 넣으세요")
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	prev, prevErr := config.Load(dirs.ConfigFile())
	if prevErr == nil && !*force {
		fmt.Fprintln(env.Stderr, "이미 등록되어 있습니다. 새 장치로 다시 등록하려면 --force를 붙이세요")
		return ExitError
	}

	body := map[string]string{"registration_code": regCode, "name": trimmedName, "platform": platformOf(env.GOOS)}
	if versionPattern.MatchString(env.Version) {
		body["collector_version"] = env.Version
	}
	data, _ := json.Marshal(body)
	ctx, cancel := context.WithCancel(env.Context)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+registerPath, bytes.NewReader(data))
	if err != nil {
		fmt.Fprintln(env.Stderr, "요청을 만들 수 없습니다")
		return ExitError
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.HTTP.Do(req)
	if err != nil {
		// Go 네트워크 오류 문자열에는 URL이 들어가므로 출력하지 않는다.
		fmt.Fprintln(env.Stderr, "서버에 연결할 수 없습니다(네트워크 오류 또는 시간 초과)")
		return ExitError
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
	case http.StatusBadRequest:
		fmt.Fprintln(env.Stderr, "등록 코드가 잘못되었거나 만료·사용되었습니다. 웹에서 새 코드를 받으세요")
		return ExitError
	case http.StatusConflict:
		fmt.Fprintln(env.Stderr, "같은 이름의 활성 장치가 이미 있습니다. 다른 --name을 쓰세요(코드는 소비되지 않았습니다)")
		return ExitError
	case http.StatusUnprocessableEntity:
		fmt.Fprintln(env.Stderr, "서버가 등록 요청 형식을 거부했습니다(422)")
		return ExitError
	case http.StatusTooManyRequests:
		fmt.Fprintln(env.Stderr, "요청이 너무 많습니다. 잠시 뒤 다시 시도하세요(429)")
		return ExitError
	default:
		fmt.Fprintf(env.Stderr, "서버 응답 상태 %d로 등록하지 못했습니다\n", resp.StatusCode)
		return ExitError
	}
	var out struct {
		DeviceID    string `json:"device_id"`
		DeviceToken string `json:"device_token"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		fmt.Fprintln(env.Stderr, "서버 응답 형식이 올바르지 않습니다")
		return ExitError
	}
	cfg := config.Config{
		SchemaVersion: config.SchemaVersion, ServerURL: base, DeviceID: out.DeviceID,
		DeviceName: trimmedName, RegisteredAt: env.Now().UTC(),
	}
	if prevErr == nil {
		cfg.ClaudeProjectsDir, cfg.ClaudeVersion, cfg.ClaudeMode = prev.ClaudeProjectsDir, prev.ClaudeVersion, prev.ClaudeMode
		// 작업 폴더 이름 동의(#181)는 같은 서버에 다시 등록할 때만 유지한다. 다른 서버는 새로 동의받는다.
		cfg.ShareWorkspaceName = prev.ShareWorkspaceName && prev.ServerURL == base
	}
	token := config.Secret(out.DeviceToken)
	check := cfg
	check.DeviceToken = token
	if err := check.Validate(); err != nil {
		fmt.Fprintln(env.Stderr, "서버 응답 형식이 올바르지 않습니다")
		return ExitError
	}
	saved, err := config.SaveWithToken(dirs.ConfigFile(), cfg, token, env.Secrets)
	if err != nil {
		fmt.Fprintln(env.Stderr, "설정 파일을 저장하지 못했습니다")
		return ExitError
	}
	if prevErr == nil && prev.DeviceID != saved.DeviceID {
		// 이전 장치의 보안 저장소 항목은 더 쓰지 않는다(서버 폐기는 하지 않음: 웹 /devices 또는 logout).
		_ = config.DeleteToken(prev, env.Secrets)
	}
	if saved.InKeyring() {
		fmt.Fprintf(env.Stdout, "등록했습니다. 장치 ID %s. 장치 토큰은 OS 보안 저장소에만 저장했습니다.\n", saved.DeviceID)
	} else {
		fmt.Fprintf(env.Stdout, "등록했습니다. 장치 ID %s. OS 보안 저장소를 쓸 수 없어 장치 토큰을 설정 파일(권한 0600)에 저장했습니다(대체 경로).\n", saved.DeviceID)
	}
	if prevErr == nil {
		fmt.Fprintln(env.Stdout, "이전 장치의 미전송 큐 이벤트는 새 장치로 보낼 수 없어 서버가 거부할 수 있습니다(dead-letter로 집계).")
	}
	fmt.Fprintln(env.Stdout, "실행 중인 Collector는 몇 초 안에 새 등록 정보로 다시 시작합니다.")
	return ExitOK
}
