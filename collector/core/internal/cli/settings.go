package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/apg0001/Agent-Office/collector/core/internal/config"
)

// workspaceNameJSON은 `settings workspace-name status --json` 출력이다.
type workspaceNameJSON struct {
	WorkspaceNameSharing bool `json:"workspace_name_sharing"`
}

// cmdSettings는 PC별 사용자 설정을 바꾼다. 지금은 작업 폴더 이름 전송 동의(#181)뿐이다.
//
//	settings workspace-name on|off|status [--json]
func cmdSettings(args []string, env Env) int {
	if len(args) < 2 || args[0] != "workspace-name" {
		usage(env.Stderr)
		return ExitUsage
	}
	action := args[1]
	fs := newFlags("settings workspace-name", env)
	asJSON := fs.Bool("json", false, "JSON으로 출력(status)")
	if err := fs.Parse(args[2:]); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	if action != "on" && action != "off" && action != "status" {
		usage(env.Stderr)
		return ExitUsage
	}
	if *asJSON && action != "status" {
		fmt.Fprintln(env.Stderr, "--json은 status에서만 쓸 수 있습니다")
		return ExitUsage
	}
	dirs, err := env.Dirs()
	if err != nil {
		fmt.Fprintln(env.Stderr, "사용자 디렉터리를 찾을 수 없습니다")
		return ExitError
	}
	cfg, err := config.Load(dirs.ConfigFile())
	if errors.Is(err, config.ErrNotRegistered) {
		fmt.Fprintln(env.Stderr, "먼저 register로 이 PC를 등록하세요")
		return ExitError
	}
	if err != nil {
		fmt.Fprintln(env.Stderr, "설정 파일을 읽을 수 없습니다(register --force 필요)")
		return ExitError
	}
	if action != "status" {
		want := action == "on"
		if cfg.ShareWorkspaceName != want {
			cfg.ShareWorkspaceName = want
			if err := config.Save(dirs.ConfigFile(), cfg); err != nil {
				fmt.Fprintln(env.Stderr, "설정을 저장하지 못했습니다")
				return ExitError
			}
		}
	}
	if *asJSON {
		data, _ := json.Marshal(workspaceNameJSON{WorkspaceNameSharing: cfg.ShareWorkspaceName})
		fmt.Fprintln(env.Stdout, string(data))
		return ExitOK
	}
	if cfg.ShareWorkspaceName {
		fmt.Fprintln(env.Stdout, "작업 폴더 이름 보내기: 켜짐. 이 PC의 Hook 이벤트에 작업 폴더의 마지막 이름(예: Agent-Office)만 붙여 보냅니다. 전체 경로·상위 폴더·대화 제목·프롬프트는 보내지 않습니다.")
	} else {
		fmt.Fprintln(env.Stdout, "작업 폴더 이름 보내기: 꺼짐(기본). Hook은 작업 폴더 경로를 읽지 않습니다. 이미 서버에 표시된 이름은 세션이 끝날 때까지 남을 수 있습니다.")
	}
	return ExitOK
}
