package collector

import (
	"bufio"
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	plistx "github.com/Sil3ntVip3r/Mac-Power-Lab/internal/plist"
)

func fixture(t testing.TB, name string) []byte {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePowermetrics(t *testing.T) {
	value, err := plistx.Parse(fixture(t, "powermetrics.plist"))
	if err != nil {
		t.Fatal(err)
	}
	s := ParsePowermetrics(value.(map[string]any))
	if s.Components.CPUWatts != 18.5 ||
		s.Components.GPUWatts != 12.25 ||
		s.Components.PackageWatts != 36 {
		t.Fatalf("components=%+v", s.Components)
	}
	if len(s.Components.Clusters) != 2 {
		t.Fatalf("clusters=%d", len(s.Components.Clusters))
	}
	if len(s.Processes) != 2 || s.Processes[0].CoalitionID == 0 {
		t.Fatalf("processes=%+v", s.Processes)
	}
	if s.Thermal.MacOSPressure != "Nominal" {
		t.Fatalf("thermal=%q", s.Thermal.MacOSPressure)
	}
}

func TestReadNULFrameBoundsAndResynchronizes(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("12345\x00ok\x00"))
	if _, err := readNULFrame(reader, 4); err == nil {
		t.Fatal("expected oversized frame error")
	}
	frame, err := readNULFrame(reader, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame) != "ok" {
		t.Fatalf("frame=%q", frame)
	}
}

func TestFrequencyMHzUsesExplicitUnits(t *testing.T) {
	if got := frequencyMHzFromMap(
		map[string]any{"frequency_mhz": float64(3800)},
		[]string{"frequency_mhz"}, nil, nil,
	); got != 3800 {
		t.Fatalf("MHz value converted incorrectly: %.1f", got)
	}
	if got := frequencyMHzFromMap(
		map[string]any{"freq_hz": float64(3_800_000_000)},
		nil, nil, []string{"freq_hz"},
	); got != 3800 {
		t.Fatalf("Hz value converted incorrectly: %.1f", got)
	}
}

func TestParseActivitiesNormalizesTotalsByElapsedTime(t *testing.T) {
	root := map[string]any{
		"tasks": []any{
			map[string]any{
				"name":              "App",
				"pid":               int64(10),
				"gputime_ms":        float64(200),
				"intr_wakeups":      float64(20),
				"diskio_bytesread":  float64(1000),
				"bytes_received":    float64(4000),
				"cputime_sample_ms": float64(500),
				"energy_impact":     float64(5),
			},
		},
	}
	activities := parseActivities(root, 2)
	if len(activities) != 1 {
		t.Fatalf("activities=%+v", activities)
	}
	activity := activities[0]
	if activity.GPUTimeMSPerS != 100 ||
		activity.InterruptWakeupsPerS != 10 ||
		activity.DiskReadBytesPerS != 500 ||
		activity.NetworkReadBytesPerS != 2000 ||
		activity.CPUTimeMSPerS != 250 {
		t.Fatalf("activity=%+v", activity)
	}
}

func TestParsePowermetricsUsesExplicitMilliwattUnits(t *testing.T) {
	root := map[string]any{
		"elapsed_ns": float64(5_000_000_000),
		"processor": map[string]any{
			"cpu_power":      float64(901.32),
			"gpu_power":      float64(162.35),
			"combined_power": float64(1063.66),
			"clusters": []any{
				map[string]any{"name": "E", "power": float64(2500)},
			},
		},
	}
	snapshot := ParsePowermetrics(root)
	assertClose := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("%s=%v want=%v", name, got, want)
		}
	}
	assertClose("CPU", snapshot.Components.CPUWatts, 0.90132)
	assertClose("GPU", snapshot.Components.GPUWatts, 0.16235)
	assertClose("package", snapshot.Components.PackageWatts, 1.06366)
	if len(snapshot.Components.Clusters) != 1 {
		t.Fatalf("clusters=%+v", snapshot.Components.Clusters)
	}
	assertClose("cluster", snapshot.Components.Clusters[0].PowerWatts, 2.5)
}

func TestComponentPowerUsesMillijouleEnergyFallback(t *testing.T) {
	// 628 mJ over 504 ms is approximately 1.246 W, matching the raw
	// powermetrics cpu_power value from a real Apple Silicon sample.
	got := componentPower(
		map[string]any{"cpu_energy": float64(628)},
		0.504191708,
		"cpu_power",
		"cpu_energy",
	)
	want := 0.628 / 0.504191708
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("power=%v want=%v", got, want)
	}
}

func BenchmarkParsePowermetricsFixture(b *testing.B) {
	value, err := plistx.Parse(fixture(b, "powermetrics.plist"))
	if err != nil {
		b.Fatal(err)
	}
	root := value.(map[string]any)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParsePowermetrics(root)
	}
}

func TestPresentZeroEnergyImpactDoesNotFallBackToRaw(t *testing.T) {
	activities := parseActivities(map[string]any{
		"tasks": []any{map[string]any{
			"name":                "Idle",
			"pid":                 int64(1),
			"energy_impact":       float64(50),
			"energy_impact_per_s": float64(0),
		}},
	}, 1)
	if len(activities) != 1 {
		t.Fatalf("activities=%+v", activities)
	}
	if activities[0].EnergyImpactPerS != 0 {
		t.Fatalf("rate=%v want=0", activities[0].EnergyImpactPerS)
	}
}

func TestPublishLatestReplacesBufferedSnapshot(t *testing.T) {
	ch := make(chan PowermetricsSnapshot, 1)
	ch <- PowermetricsSnapshot{Status: "old"}
	publishLatest(context.Background(), ch, PowermetricsSnapshot{Status: "new"})
	if got := (<-ch).Status; got != "new" {
		t.Fatalf("status=%q want=new", got)
	}
}
