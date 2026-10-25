// Command agent-office-collector는 GUI와 분리된 headless Collector다(#34).
// 사용법은 `agent-office-collector help`와 docs/installation/collector-cli.md를 본다.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/apg0001/Agent-Office/collector/core/internal/cli"
)

// version은 릴리스 빌드에서 -ldflags "-X main.version=..."으로 넣는다.
var version = "0.0.0-dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Main(os.Args[1:], cli.Env{Version: version, Context: ctx})
	stop()
	os.Exit(code)
}
