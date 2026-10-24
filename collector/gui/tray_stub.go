//go:build !(desktop && linux)

package main

// newTrayHost는 트레이 미지원 빌드에서 nil이다(#43).
//
// macOS·Windows: Wails v2에는 트레이 API가 없고, fyne.io/systray를 같은 프로세스에 붙이면 macOS는
// NSApplication 메인 루프를, Windows는 메시지 루프 스레드를 Wails와 다투게 돼 검증 없이는 넣지 않는다.
// 이 경우 창 닫기는 GUI 종료이고(Core는 계속 수집), 상태는 창에서 본다. ADR-0004 참고.
func newTrayHost() trayHost { return nil }
