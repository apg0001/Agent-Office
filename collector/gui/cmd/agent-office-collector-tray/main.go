// agent-office-collector-tray는 Collector 시스템 트레이(메뉴 막대) 도우미다(#159, ADR-0005).
//
// Wails 창(agent-office-collector-gui)과 다른 프로세스에서 fyne.io/systray가 메인 스레드·메시지 루프를
// 혼자 쓴다. 그래서 macOS(NSApplication)·Windows(메시지 루프)에서도 Wails와 루프를 다투지 않는다.
// 트레이는 Core 명령(`status --json`, `pause`, `resume`, `update check`, `logout`)만 쓰고, 창이 필요하면
// GUI 실행 파일을 띄운다(이미 떠 있으면 두 번째 GUI가 internal/guictl로 기존 창에 "보이기"를 남기고 끝난다).
//
// 사용법:
//
//	agent-office-collector-tray                  트레이 실행(사용자별 하나만, 두 번째는 곧바로 끝남)
//	agent-office-collector-tray --autostart      로그인 자동 시작으로 실행(동작은 같음)
//	agent-office-collector-tray autostart install|uninstall|status
//	                                             로그인 시 트레이 자동 시작 정의(소유 항목만 변경)
//	agent-office-collector-tray --self-test FILE 설치 검증용: 실제 트레이를 띄우고 메뉴 항목을 차례로
//	                                             눌러(브라우저 열기만 기록) 결과 JSON을 FILE에 쓰고 끝남
//	agent-office-collector-tray version
//
// 종료 코드: 0 정상, 2 사용법, 3 이미 실행 중, 1 그 밖의 실패(셀프 테스트 실패 포함).
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/apg0001/Agent-Office/collector/gui/internal/app"
	"github.com/apg0001/Agent-Office/collector/gui/internal/prefs"
	"github.com/apg0001/Agent-Office/collector/gui/internal/single"
)

// version은 빌드 때 -ldflags "-X main.version=..."로 넣는다.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	selfTest := ""
	switch {
	case len(args) == 0, len(args) == 1 && args[0] == "--autostart":
	case len(args) == 1 && (args[0] == "version" || args[0] == "--version"):
		fmt.Fprintf(stdout, "agent-office-collector-tray %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case len(args) == 2 && args[0] == "autostart":
		return autostartCmd(args[1], stdout, stderr)
	case len(args) == 2 && args[0] == "--self-test" && args[1] != "":
		selfTest = args[1]
	default:
		fmt.Fprintln(stderr, "사용법: agent-office-collector-tray [--autostart | --self-test 결과.json | autostart install|uninstall|status | version]")
		return 2
	}
	// systray는 오류를 log로 남긴다. 로컬 표준 오류에만 나가고 경로·토큰을 쓰지 않는다.
	log.SetFlags(0)
	log.SetPrefix("agent-office-collector-tray: ")

	lock, err := acquire()
	if err != nil {
		if err == single.ErrHeld {
			if selfTest != "" {
				fmt.Fprintln(stderr, "이미 트레이가 실행 중입니다")
			}
			return 3
		}
		fmt.Fprintln(stderr, "트레이 잠금을 만들 수 없습니다")
		return 1
	}
	defer lock.Release()
	return runTray(selfTest)
}

// acquire는 GUI 설정과 같은 폴더의 tray.lock을 잡는다.
func acquire() (*single.Lock, error) {
	p, err := prefs.Path(os.Getenv, os.UserConfigDir)
	if err != nil {
		return nil, err
	}
	return single.Acquire(filepath.Join(filepath.Dir(p), "tray.lock"))
}

func autostartCmd(sub string, stdout, stderr io.Writer) int {
	a := app.New(app.Options{})
	switch sub {
	case "install":
		return printResult(a.GUIAutostartInstall(true), stdout, stderr)
	case "uninstall":
		return printResult(a.GUIAutostartUninstall(true), stdout, stderr)
	case "status":
		v := a.GUIAutostartStatus()
		fmt.Fprintln(stdout, v.State)
		if !v.OK {
			return 1
		}
		return 0
	}
	fmt.Fprintln(stderr, "사용법: agent-office-collector-tray autostart install|uninstall|status")
	return 2
}

func printResult(r app.Result, stdout, stderr io.Writer) int {
	if !r.OK {
		fmt.Fprintln(stderr, r.Code+": "+r.Message)
		return 1
	}
	fmt.Fprintln(stdout, r.Code+": "+r.Message)
	return 0
}
