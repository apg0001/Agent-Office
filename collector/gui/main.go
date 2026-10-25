// agent-office-collector-gui는 Collector Core를 감싸는 Wails v2 데스크톱 창이다(#40, #41, #43, #159).
//
// GUI는 수집 루프를 품지 않는다. 등록·상태·일시중지·진단·Hook·자동 시작·로그아웃은 모두 Core
// 실행 파일(`agent-office-collector`) 명령으로 처리하므로, 창을 닫거나 GUI가 죽어도 자동 시작된 Core는
// 계속 수집한다. 시스템 트레이는 별도 프로세스(`agent-office-collector-tray`, ADR-0005)이고, GUI가 뜰 때
// 옆에 트레이 도우미가 있으면 함께 띄운다(이미 떠 있으면 도우미가 스스로 끝남). 창 닫기는 GUI 종료다.
//
// GUI는 사용자별로 하나만 뜬다(internal/guictl: 파일 잠금 + 요청 파일). 두 번째 실행은 Wails를 띄우지 않고
// 첫 GUI에 "창 보이기"(인자 없음) 또는 "종료"(--quit)를 남기고 끝난다.
//
// 빌드(Linux): go build -tags desktop,production[,webkit2_41] -o agent-office-collector-gui .
// 태그 없이 빌드하면 Wails가 실행 시 오류를 낸다(단위 테스트·vet은 태그 없이 동작).
//
// 인자:
//
//	--autostart  예전(#43) 로그인 자동 시작 항목으로 실행됨. 트레이 도우미가 있으면 트레이만 띄우고 끝낸다.
//	--quit       떠 있는 GUI 창 프로세스를 끝낸다(트레이 "종료"). 떠 있지 않으면 아무것도 하지 않는다.
package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/autostart"
	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/guictl"
	"github.com/apg0001/Agent-Office/collector/gui/internal/launch"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
	"github.com/apg0001/Agent-Office/collector/gui/internal/single"
)

//go:embed all:frontend/assets
var assets embed.FS

// QuitArg는 떠 있는 GUI 창 프로세스에 종료를 알리는 인자다.
const QuitArg = "--quit"

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// trayPath는 이 GUI와 함께 설치된 트레이 도우미 위치다(개발 빌드에는 없을 수 있음).
func trayPath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return corecli.FindSibling(exe, runtime.GOOS, corecli.TrayName)
}

func main() {
	args := os.Args[1:]
	quitOnly := hasArg(args, QuitArg)
	tray, trayOK := trayPath()
	if hasArg(args, autostart.Arg) && !quitOnly && trayOK {
		// 예전 자동 시작 항목(GUI --autostart): 창 대신 트레이만 띄운다. 새 항목은 트레이를 직접 띄운다.
		if err := launch.Detached(tray, autostart.Arg); err == nil {
			return
		}
	}

	// 단일 실행: 잠금을 못 잡으면 첫 GUI에 요청만 남기고 끝낸다. 잠금 폴더를 못 쓰면 단일 실행 없이 뜬다.
	var primary *guictl.Primary
	if p, err := prefs.Path(os.Getenv, os.UserConfigDir); err == nil {
		dir := filepath.Dir(p)
		pr, err := guictl.Acquire(dir)
		switch {
		case errors.Is(err, single.ErrHeld):
			verb := guictl.Show
			if quitOnly {
				verb = guictl.Quit
			}
			if err := guictl.Send(dir, verb); err != nil {
				os.Exit(1)
			}
			return
		case err == nil:
			primary = pr
			defer pr.Release()
		}
	}
	if quitOnly {
		// 떠 있는 GUI가 없다: 할 일이 없다.
		return
	}

	a := app.New(app.Options{
		OpenURL: func(ctx context.Context, url string) { wruntime.BrowserOpenURL(ctx, url) },
	})
	var (
		mu   sync.Mutex
		wctx context.Context
	)
	windowCtx := func() context.Context {
		mu.Lock()
		defer mu.Unlock()
		return wctx
	}
	startup := app.OnStartup(a)

	err := wails.Run(&options.App{
		Title:            "Agent Office Collector",
		Width:            980,
		Height:           760,
		MinWidth:         720,
		MinHeight:        560,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 247, G: 246, B: 242, A: 255},
		OnStartup: func(ctx context.Context) {
			startup(ctx)
			mu.Lock()
			wctx = ctx
			mu.Unlock()
			if primary != nil {
				go primary.Serve(ctx, func(v guictl.Verb) {
					c := windowCtx()
					switch v {
					case guictl.Quit:
						wruntime.Quit(c)
					case guictl.Show:
						wruntime.WindowShow(c)
						wruntime.WindowUnminimise(c)
					}
				})
			}
			if trayOK {
				// 트레이는 사용자별 단일 실행이라 이미 떠 있으면 곧바로 끝난다. 실패해도 창은 쓸 수 있다.
				_ = launch.Detached(tray)
			}
		},
		Bind: []interface{}{a},
	})
	if err != nil {
		// 오류 문자열에 경로가 들어갈 수 있어도 로컬 터미널에만 나간다.
		log.Fatal(err)
	}
}
