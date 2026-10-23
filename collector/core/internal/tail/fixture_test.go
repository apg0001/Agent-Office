package tail

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/apg0001/Agent-Office/collector/core/internal/adapter"
	"github.com/apg0001/Agent-Office/collector/core/internal/adapter/claude"
)

var fixtureDir = filepath.Join("..", "..", "..", "..", "tests", "fixtures", "collector", "claude", "dual-source")

// #20 합성 transcript를 Claude 배치(projects/<p>/<s>.jsonl, .../subagents/<a>.jsonl)로 놓고
// 탐색 → tail → 어댑터로 얻은 후보가 줄을 직접 읽어 얻은 후보와 같은지 확인한다.
func TestFixtureTranscriptsThroughDiscoverTailAndAdapter(t *testing.T) {
	root := t.TempDir()
	layout := [][2]string{
		{"transcript.jsonl", "proj/s.jsonl"},
		{"subagent-transcript.jsonl", "proj/s/subagents/a.jsonl"},
	}
	rt := claude.VerifiedTargets[0].Runtime
	var direct []adapter.Candidate
	for _, pair := range layout {
		src, rel := pair[0], pair[1]
		data, err := os.ReadFile(filepath.Join(fixtureDir, src))
		if err != nil {
			t.Fatal(err)
		}
		mkfile(t, root, rel)
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), data)
		for _, line := range bytes.Split(data, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			c, _ := claude.ParseTranscriptLine(line, rt)
			direct = append(direct, c...)
		}
	}

	d, err := Discover(root, DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 2 || d.Files[0].Kind != KindMain || d.Files[1].Kind != KindSubagent {
		t.Fatalf("files = %+v", d.Files)
	}
	var tailed []adapter.Candidate
	// 정렬 순서가 direct와 같다(main → subagent).
	for _, f := range d.Files {
		res := PollOnce(f.Path(root), Checkpoint{}, Options{})
		if res.Status != StatusOK || res.InvalidLines != 0 || res.PendingPartialBytes != 0 {
			t.Fatalf("result = %+v", res)
		}
		for _, r := range res.Records {
			c, _ := claude.ParseTranscriptLine(r, rt)
			tailed = append(tailed, c...)
		}
	}
	if len(direct) == 0 || !reflect.DeepEqual(direct, tailed) {
		t.Fatalf("tailed %d candidates, direct %d", len(tailed), len(direct))
	}
}
