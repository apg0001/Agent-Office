// agent-office-collector-gui는 Collector Core를 감싸는 Wails v2 데스크톱 창이다(#40, #41, #43, #159).
//
// GUI는 수집 루프를 품지 않는다. 등록·상태·일시중지·진단·Hook·자동 시작·로그아웃은 모두 Core
// 실행 파일(`agent-office-collector`) 명령으로 처리하므로, 창을 닫거나 GUI가 죽어도 자동 시작된 Core는
// 계속 수집한다. 시스템 트레이는 별도 프로세스(`agent-office-collector-tray`, ADR-0005)이고, GUI가 뜰 때
// 옆에 트레이 도우미가 있으면 함께 띄운다(이미 떠 있으면 도우미가 스스로 끝남). 창 닫기는 GUI 종료다.
//
// 빌드(Linux): go build -tags desktop,production[,webkit2_41] -o agent-office-collector-gui .
// 태그 없이 빌드하면 Wails가 실행 시 오류를 낸다(단위 테스트·vet은 태그 없이 동작).
//
// 인자:
//
//	--autostart  예전(#43) 로그인 자동 시작 항목으로 실행됨. 트레이 도우미가 있으면 트레이만 띄우고 끝낸다.
//	--quit       떠 있는 GUI 창 프로세스를 끝낸다(트레이 "종료"). 떠 있지 않으면 창 없이 떴다가 끝난다.
package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/autostart"
	"github.com/apg0001/Agent-Office/collector/gui/internal/corecli"
	"github.com/apg0001/Agent-Office/collector/gui/internal/launch"
)

//go:embed all:frontend/assets
var assets embed.FS

// singleInstanceID는 GUI 중복 실행 방지 식별자다(트레이 "설정" + 수동 실행 시 기존 창을 띄움).
const singleInstanceID = "io.github.apg0001.agent-office.collector-gui"

// QuitArg는 떠 있는 GUI 창 프로세스에 종료를 알리는 인자다(Wails 단일 실행으로 첫 프로세스에 전달됨).
const QuitArg = "--quit"

// quitGrace는 --quit으로 떴는데 떠 있던 GUI가 없을 때 스스로 끝나기까지의 여유다.
const quitGrace = 5 * time.Second

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
	showWindow := func() {
		if c := windowCtx(); c != nil {
			wruntime.WindowShow(c)
			wruntime.WindowUnminimise(c)
		}
	}
	startup := app.OnStartup(a)

	err := wails.Run(&options.App{
		Title:            "Agent Office Collector",
		Width:            980,
		Height:           760,
		MinWidth:         720,
		MinHeight:        560,
		StartHidden:      quitOnly,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 247, G: 246, B: 242, A: 255},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				if hasArg(d.Args, QuitArg) {
					if c := windowCtx(); c != nil {
						wruntime.Quit(c)
					}
					return
				}
				showWindow()
			},
		},
		OnStartup: func(ctx context.Context) {
			startup(ctx)
			mu.Lock()
			wctx = ctx
			mu.Unlock()
			if quitOnly {
				// 떠 있던 GUI가 없었다(있었다면 Wails 단일 실행이 그쪽에 --quit을 넘기고 이 프로세스를 끝냄).
				// 아무것도 보이지 않고 끝낸다. Linux는 OnStartup이 GTK 메인 루프보다 먼저 돌아 Quit이
				// 무시될 수 있으므로(gtk_main_quit: main_loops != NULL) 잠시 뒤 프로세스를 직접 끝낸다.
				go func() {
					wruntime.Quit(ctx)
					time.Sleep(quitGrace)
					os.Exit(0)
				}()
				return
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
