package tail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// runAppendScenario는 ops 바이트로 합성 줄을 임의 조각으로 append하면서 poll·재시작을 섞는다.
// 모든 유효 줄이 순서대로 정확히 한 번 나와야 한다(중복·유실 0).
func runAppendScenario(t *testing.T, ops []byte) {
	cursor := 0
	next := func() int {
		if cursor >= len(ops) {
			return 0
		}
		b := ops[cursor]
		cursor++
		return int(b)
	}

	var stream []byte
	var want []int
	lines := 1 + next()%30
	for i := 0; i < lines; i++ {
		switch next() % 8 {
		case 0:
			stream = append(stream, "bad\n"...)
		case 1:
			stream = append(stream, bytes.Repeat([]byte("x"), 100)...)
			stream = append(stream, '\n')
		case 2:
			stream = append(stream, fmt.Sprintf("{\"n\":%d}\r\n", i)...)
			want = append(want, i)
		case 3:
			stream = append(stream, '\n')
		default:
			stream = append(stream, fmt.Sprintf("{\"n\":%d}\n", i)...)
			want = append(want, i)
		}
	}
	opts := Options{MaxLineBytes: 64}
	if next()%2 == 1 {
		opts.MaxBytesPerPoll = 1 // 최소 상한(줄 상한+4)으로 올라가 여러 poll로 나뉜다.
	}

	p := filepath.Join(t.TempDir(), "t.jsonl")
	writeFile(t, p, nil)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tl := New(p, Checkpoint{}, opts)
	defer func() { tl.Close() }()
	var got []int
	collect := func(res Result) {
		if res.Status != StatusOK || res.Reset != ResetNone {
			t.Fatalf("status = %q reset = %q", res.Status, res.Reset)
		}
		for _, r := range res.Records {
			var v struct{ N int }
			if err := json.Unmarshal(r, &v); err != nil {
				t.Fatalf("record is not the synthetic object")
			}
			got = append(got, v.N)
		}
	}

	written := 0
	for written < len(stream) {
		step := min(1+next()%17, len(stream)-written)
		if _, err := f.Write(stream[written : written+step]); err != nil {
			t.Fatal(err)
		}
		written += step
		if next()%4 == 0 {
			saved, err := MarshalCheckpoint(tl.Checkpoint())
			if err != nil {
				t.Fatal(err)
			}
			tl.Close()
			cp, err := UnmarshalCheckpoint(saved)
			if err != nil {
				t.Fatal(err)
			}
			tl = New(p, cp, opts)
		}
		collect(tl.Poll())
	}
	for i := 0; i < 100; i++ {
		res := tl.Poll()
		collect(res)
		if !res.More {
			break
		}
	}

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("emitted %v, want %v", got, want)
	}
	if tl.Checkpoint().Offset != int64(len(stream)) {
		t.Fatalf("offset = %d, want %d", tl.Checkpoint().Offset, len(stream))
	}
}

func FuzzTailAppends(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{29, 4, 4, 1, 4, 2, 0, 3, 1, 1, 2, 3, 5, 7, 11, 13, 0, 0})
	f.Add(bytes.Repeat([]byte{1, 16, 0}, 40))
	f.Add(bytes.Repeat([]byte{4, 2, 3}, 60))
	f.Fuzz(func(t *testing.T, ops []byte) {
		runAppendScenario(t, ops)
	})
}

func TestRandomAppendsNeverDuplicateOrLose(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		ops := make([]byte, 400)
		rng.Read(ops)
		t.Run(fmt.Sprint(seed), func(t *testing.T) { runAppendScenario(t, ops) })
	}
}
