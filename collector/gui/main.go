// agent-office-collector-gui는 Collector Core를 감싸는 Wails v2 데스크톱 창이다(#40, #41).
//
// GUI는 수집 루프를 품지 않는다. 등록·상태·일시중지·진단·Hook·자동 시작은 모두 Core
// 실행 파일(`agent-office-collector`) 명령으로 처리하므로, 창을 닫아도 자동 시작된 Core는 계속 수집한다.
//
// 빌드(Linux): go build -tags desktop,production -o agent-office-collector-gui .
// 태그 없이 빌드하면 Wails가 실행 시 오류를 낸다(단위 테스트·vet은 태그 없이 동작).
package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
)

//go:embed all:frontend/assets
var assets embed.FS

func main() {
	a := app.New(app.Options{
		OpenURL: func(ctx context.Context, url string) { runtime.BrowserOpenURL(ctx, url) },
	})
	err := wails.Run(&options.App{
		Title:            "Agent Office Collector",
		Width:            980,
		Height:           760,
		MinWidth:         720,
		MinHeight:        560,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 247, G: 246, B: 242, A: 255},
		OnStartup:        app.OnStartup(a),
		Bind:             []interface{}{a},
	})
	if err != nil {
		// 오류 문자열에 경로가 들어갈 수 있어도 로컬 터미널에만 나간다.
		log.Fatal(err)
	}
}
