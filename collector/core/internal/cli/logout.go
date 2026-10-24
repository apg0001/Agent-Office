package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
)

// revokePath는 장치 토큰으로 자기 장치를 폐기하는 서버 API다(apps/server devices.py `revoke_self`).
const revokePath = "/api/v1/devices/revoke"

// cmdLogout은 서버에서 이 장치를 폐기하고 로컬 장치 토큰(보안 저장소 항목·config.json)을 지운다(#41).
//
// 기본은 서버 폐기가 확인돼야(204, 또는 이미 폐기돼 401·403) 로컬을 지운다. 네트워크 오류면 아무것도
// 지우지 않고 실패한다. 서버에 닿을 수 없을 때는 --local-only로 로컬만 지우고 웹 /devices에서 폐기한다.
// 실행 중인 `run`은 config.json이 사라진 것을 몇 초 안에 감지해 전송·heartbeat를 멈춘다.
// 로컬 큐(정규화된 이벤트)는 지우지 않는다. 다시 등록하면 새 장치로는 보낼 수 없어 서버가 거부한다.
func cmdLogout(args []string, env Env) int {
	fs := newFlags("logout", env)
	localOnly := fs.Bool("local-only", false, "서버에 폐기 요청을 보내지 않고 로컬 토큰·등록 정보만 지움")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	cfg, err := config.Load(dirs.ConfigFile())
	switch {
	case errors.Is(err, config.ErrNotRegistered):
		fmt.Fprintln(env.Stdout, "등록되어 있지 않습니다. 지울 장치 토큰이 없습니다.")
		return ExitOK
	case err != nil:
		if !*localOnly {
			fmt.Fprintln(env.Stderr, "설정 파일이 손상돼 서버 폐기를 요청할 수 없습니다. --local-only로 로컬 등록 정보만 지우고 웹 /devices에서 폐기하세요")
			return ExitError
		}
		if err := removeConfig(dirs.ConfigFile()); err != nil {
			fmt.Fprintln(env.Stderr, "설정 파일을 지우지 못했습니다")
			return ExitError
		}
		fmt.Fprintln(env.Stdout, "손상된 로컬 등록 정보를 지웠습니다. 서버에서는 웹 /devices로 이 PC를 폐기하세요.")
		return ExitOK
	}

	serverMsg := "서버 폐기 요청은 보내지 않았습니다(--local-only). 웹 /devices에서 이 PC를 폐기하세요."
	if !*localOnly {
		token, err := cfg.Token(env.Secrets)
		if err != nil {
			fmt.Fprintln(env.Stderr, "OS 보안 저장소에서 장치 토큰을 읽지 못해 서버 폐기를 요청할 수 없습니다. 저장소 잠금을 해제하고 다시 시도하거나 --local-only를 쓰세요")
			return ExitError
		}
		switch code := revokeOnServer(env, cfg.ServerURL, token); code {
		case http.StatusNoContent, http.StatusOK:
			serverMsg = "서버에서 이 장치를 폐기했습니다."
		case http.StatusUnauthorized, http.StatusForbidden:
			serverMsg = "서버가 이 장치 토큰을 이미 거부합니다(폐기됨 또는 만료). 로컬 정보만 지웁니다."
		case 0:
			fmt.Fprintln(env.Stderr, "서버에 연결할 수 없어 폐기하지 못했습니다. 로컬 정보는 그대로 두었습니다. 나중에 다시 시도하거나 --local-only를 쓰세요")
			return ExitError
		default:
			fmt.Fprintf(env.Stderr, "서버 응답 상태 %d로 폐기하지 못했습니다. 로컬 정보는 그대로 두었습니다\n", code)
			return ExitError
		}
	}

	keyringErr := config.DeleteToken(cfg, env.Secrets)
	if err := removeConfig(dirs.ConfigFile()); err != nil {
		fmt.Fprintln(env.Stderr, "설정 파일을 지우지 못했습니다")
		return ExitError
	}
	fmt.Fprintln(env.Stdout, serverMsg)
	if keyringErr != nil {
		fmt.Fprintln(env.Stdout, "OS 보안 저장소 항목은 지우지 못했습니다(저장소 잠김 또는 없음). 서버에서 폐기됐다면 그 토큰은 더 쓸 수 없습니다.")
	}
	fmt.Fprintln(env.Stdout, "로컬 장치 토큰과 등록 정보를 지웠습니다. 실행 중인 Collector는 몇 초 안에 전송을 멈춥니다.")
	return ExitOK
}

func removeConfig(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// revokeOnServer는 상태 코드를 돌려준다. 네트워크 오류·시간 초과는 0이다(오류 문자열은 URL을 담아 버림).
func revokeOnServer(env Env, base string, token config.Secret) int {
	ctx, cancel := context.WithTimeout(env.Context, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+revokePath, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	return resp.StatusCode
}
