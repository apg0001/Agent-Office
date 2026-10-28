package cli

import "testing"

// 파일 권한(EACCES)으로 기록 파일을 못 열었을 때만 조치 안내를 낸다(#184).
func TestDeniedEACCESHint(t *testing.T) {
	cases := []struct {
		counters map[string]int64
		want     bool
	}{
		{map[string]int64{"tail_denied_open_eacces": 2}, true},
		{map[string]int64{"tail_denied_stat_eacces": 1}, true},
		{map[string]int64{"tail_denied_open_eperm": 3}, false},
		{map[string]int64{"tail_denied_open_eacces": 0}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := deniedEACCES(c.counters); got != c.want {
			t.Errorf("%v: got %v", c.counters, got)
		}
	}
}
