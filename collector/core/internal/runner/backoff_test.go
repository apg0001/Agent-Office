package runner

import (
	"testing"
	"time"
)

// 읽기 권한이 없는 transcript는 2초마다 다시 열지 않는다(#184).
func TestNextDeniedWaitBacksOff(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	var got time.Duration
	for i, w := range want {
		got = nextDeniedWait(got)
		if got != w {
			t.Fatalf("step %d: %v, want %v", i, got, w)
		}
	}
}
