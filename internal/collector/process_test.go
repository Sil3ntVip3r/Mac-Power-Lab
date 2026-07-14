package collector

import (
	"fmt"
	"strings"
	"testing"
)

func TestParsePSOutputSortsAndBounds(t *testing.T) {
	var input strings.Builder
	for i := 1; i <= maxFallbackProcesses+100; i++ {
		fmt.Fprintf(&input, "%d 1 %.1f 0.1 %d /Applications/App%d.app/Contents/MacOS/App%d\n", i, float64(i), i, i, i)
	}

	got := parsePSOutput([]byte(input.String()))
	if len(got) != maxFallbackProcesses {
		t.Fatalf("len=%d want=%d", len(got), maxFallbackProcesses)
	}
	if got[0].PID != maxFallbackProcesses+100 {
		t.Fatalf("highest CPU PID=%d", got[0].PID)
	}
	if got[len(got)-1].PID != 101 {
		t.Fatalf("lowest retained PID=%d", got[len(got)-1].PID)
	}
}

func TestParsePSOutputSkipsMalformedRows(t *testing.T) {
	input := []byte(`bad row
0 1 10.0 0.1 10 zero
1 1 nope 0.1 10 invalid-cpu
2 1 20.0 0.1 nope invalid-rss
3 1 30.0 0.1 100 /usr/bin/valid
`)
	got := parsePSOutput(input)
	if len(got) != 1 || got[0].PID != 3 {
		t.Fatalf("got=%+v", got)
	}
}
