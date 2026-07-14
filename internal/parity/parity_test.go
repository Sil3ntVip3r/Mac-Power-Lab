package parity

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestCompareSamples(t *testing.T) {
	current := model.PowerSample{}
	legacy := model.PowerSample{}
	current.Battery.Percent = 50
	legacy.Battery.Percent = 51
	current.Components.CPUWatts = 30
	legacy.Components.CPUWatts = 60

	differences, passed := compareSamples(current, legacy)
	if passed {
		t.Fatal("expected CPU difference to fail")
	}
	if len(differences) != 8 {
		t.Fatalf("differences=%d", len(differences))
	}
}

func TestWriteAtomicJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.json")
	if err := writeAtomicJSON(path, map[string]int{"value": 1}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\n  \"value\": 1\n}\n" {
		t.Fatalf("data=%q", data)
	}
}

func TestCollectPairRunsCollectorsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	collectorFn := func(name string) sampleCollector {
		return func(context.Context) (model.PowerSample, error) {
			started <- name
			<-release
			return model.PowerSample{}, nil
		}
	}
	done := make(chan struct{})
	go func() {
		_, _, _, _ = collectPair(context.Background(), collectorFn("go"), collectorFn("legacy"))
		close(done)
	}()
	seen := map[string]bool{<-started: true, <-started: true}
	if !seen["go"] || !seen["legacy"] {
		t.Fatalf("collectors did not both start: %v", seen)
	}
	close(release)
	<-done
}
