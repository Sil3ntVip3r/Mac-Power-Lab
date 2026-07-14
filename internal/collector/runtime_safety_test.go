package collector

import (
	"strings"
	"testing"
	"time"
)

func TestContinuousPowermetricsCandidatesExcludeTasks(t *testing.T) {
	c := NewPowermetricsCollector(2 * time.Second)
	for _, args := range c.commandCandidates() {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "tasks") {
			t.Fatalf("continuous sampler must not include tasks: %s", joined)
		}
		if strings.Contains(joined, "--show-process-energy") {
			t.Fatalf("continuous sampler must not include process flags: %s", joined)
		}
	}
}

func TestParseActivitiesIsBounded(t *testing.T) {
	rows := make([]any, 0, 700)
	for i := 0; i < 700; i++ {
		rows = append(rows, map[string]any{
			"name":                "process",
			"pid":                 int64(i + 1),
			"energy_impact_per_s": float64(700 - i),
		})
	}
	got := parseActivities(map[string]any{"tasks": rows}, 1)
	if len(got) != 512 {
		t.Fatalf("len=%d want=512", len(got))
	}
}
