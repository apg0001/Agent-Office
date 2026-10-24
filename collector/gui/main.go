// agent-office-collector-gui는 Collector Core를 감싸는 Wails v2 데스크톱 창이다(#40, #41, #43).
//
// GUI는 수집 루프를 품지 않는다. 등록·상태·일시중지·진단·Hook·자동 시작·로그아웃은 모두 Core
// 실행 파일(`agent-office-collector`) 명령으로 처리하므로, 창을 닫거나 GUI가 죽어도 자동 시작된 Core는
// 계속 수집한다. Linux에서는 트레이(StatusNotifierItem)를 띄우고, 트레이가 보이면 창 닫기는 숨기기다.
//
// 빌드(Linux): go build -tags desktop,production -o agent-office-collector-gui .
// 태그 없이 빌드하면 Wails가 실행 시 오류를 낸다(단위 테스트·vet은 태그 없이 동작).
//
// 인자: --autostart  로그인 자동 시작(#43)으로 실행됨. 트레이가 보이면 창 없이 시작한다.
package main

import (
	"context"
	"embed"
	"log"
	"os"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/autostart"
	"github.com/apg0001/Agent-Office/collector/gui/internal/tray"
)

//go:embed all:frontend/assets
var assets embed.FS

// trayHost는 OS 트레이 구현이다(tray_linux.go, 그 외 OS는 nil).
type trayHost interface {
	tray.Host
	Start(ctrl *tray.Controller)
	Stop()
}

// singleInstanceID는 GUI 중복 실행 방지 식별자다(자동 시작 + 수동 실행 시 기존 창을 띄움).
const singleInstanceID = "io.github.apg0001.agent-office.collector-gui"

func hasArg(args []string, want string) bool {
	for _, a := range args[1:] {
		if a == want {
			return true
		}
	}
	return false
}

func main() {
	a := app.New(app.Options{
		OpenURL: func(ctx context.Context, url string) { runtime.BrowserOpenURL(ctx, url) },
	})
	host := newTrayHost()
	var th tray.Host
	if host != nil {
		th = host
	}
	ctrl := tray.New(a, th)

	var (
		mu   sync.Mutex
		wctx context.Context
	)
	windowCtx := func() context.Context {
		mu.Lock()
		defer mu.Unlock()
		return wctx
	}
	ctrl.ShowWindow = func() {
		if c := windowCtx(); c != nil {
			runtime.WindowShow(c)
			runtime.WindowUnminimise(c)
		}
	}
	ctrl.QuitGUI = func() {
		if c := windowCtx(); c != nil {
			runtime.Quit(c)
		}
	}
	startup := app.OnStartup(a)

	err := wails.Run(&options.App{
		Title:            "Agent Office Collector",
		Width:            980,
		Height:           760,
		MinWidth:         720,
		MinHeight:        560,
		StartHidden:      ctrl.StartHidden(hasArg(os.Args, autostart.Arg)),
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 247, G: 246, B: 242, A: 255},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { ctrl.ShowWindow() },
		},
		OnStartup: func(ctx context.Context) {
			startup(ctx)
			mu.Lock()
			wctx = ctx
			mu.Unlock()
			if host != nil {
				host.Start(ctrl)
				go ctrl.Run(ctx)
			}
		},
		OnBeforeClose: func(ctx context.Context) bool {
			return ctrl.BeforeClose(func() { runtime.WindowHide(ctx) })
		},
		OnShutdown: func(context.Context) {
			if host != nil {
				host.Stop()
			}
		},
		Bind: []interface{}{a},
	})
	if err != nil {
		// 오류 문자열에 경로가 들어갈 수 있어도 로컬 터미널에만 나간다.
		log.Fatal(err)
	}
}
