package report

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/store"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

func TestGenerate(t *testing.T) {
	base := t.TempDir()
	session := model.Session{Schema: version.SessionSchema, ID: "test", Version: version.Version, StartedAt: time.Now(), DataDirectory: base}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		ts := time.Now().Add(time.Duration(i) * time.Second)
		sample := model.PowerSample{Schema: version.PowerSampleSchema, Timestamp: ts, SessionID: "test", Sequence: uint64(i + 1), PrimaryLoadW: 40 + float64(i), Battery: model.BatterySample{NetWatts: -40, TemperatureC: 35}, Attribution: model.AttributionResult{Apps: []model.AppPower{{Schema: version.AppPowerSchema, Timestamp: ts, Key: "app", Name: "App", EstimatedDynamicW: 10, EstimatedEnergyWh: float64(i) * .01, Confidence: "high", AttributionSource: "test"}}}}
		if err := st.WriteSample(sample); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "sessions", "test")
	artifact, err := Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	summary := artifact.Summary
	if summary.SampleCount != 3 || summary.PeakPrimaryLoadW != 42 {
		t.Fatalf("summary=%+v", summary)
	}
	for _, path := range []string{artifact.SummaryPath, artifact.MarkdownPath, artifact.HTMLPath, filepath.Join(artifact.Directory, "artifact.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if filepath.Dir(artifact.Directory) != filepath.Join(dir, reportsDirectoryName) {
		t.Fatalf("artifact directory=%q", artifact.Directory)
	}
}

func TestGenerateUsesTimeWeightedAverageAndDeduplicatesRuns(t *testing.T) {
	base := t.TempDir()
	started := time.Unix(1_700_000_000, 0)
	session := model.Session{Schema: version.SessionSchema, ID: "weighted", Version: version.Version, StartedAt: started}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []model.PowerSample{
		{Schema: version.PowerSampleSchema, Timestamp: started, SessionID: session.ID, Sequence: 1, PrimaryLoadW: 10, Battery: model.BatterySample{NetWatts: -10}},
		{Schema: version.PowerSampleSchema, Timestamp: started.Add(time.Second), SessionID: session.ID, Sequence: 2, PrimaryLoadW: 20, Battery: model.BatterySample{NetWatts: -20}},
		{Schema: version.PowerSampleSchema, Timestamp: started.Add(9 * time.Second), SessionID: session.ID, Sequence: 3, PrimaryLoadW: 30, Battery: model.BatterySample{NetWatts: -30}},
	} {
		if err := st.WriteSample(sample); err != nil {
			t.Fatal(err)
		}
	}
	running := model.TestRun{ID: "run", SessionID: session.ID, Name: "CPU", Status: "running", StartedAt: started, RequestedSeconds: 60}
	final := running
	final.Status = "complete"
	final.EndedAt = started.Add(time.Minute)
	final.ActualSeconds = 60
	if err := st.WriteTestRun(running); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteTestRun(final); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	artifact, err := Generate(filepath.Join(base, "sessions", session.ID))
	if err != nil {
		t.Fatal(err)
	}
	summary := artifact.Summary
	wantAverage := (10.0*1 + 20.0*8) / 9
	if math.Abs(summary.AveragePrimaryLoadW-wantAverage) > 0.001 {
		t.Fatalf("average=%.4f want=%.4f", summary.AveragePrimaryLoadW, wantAverage)
	}
	if len(summary.TestRuns) != 1 || summary.TestRuns[0].Status != "complete" {
		t.Fatalf("test runs=%+v", summary.TestRuns)
	}
}

func TestGenerateSkipsSleepSizedIntegrationGap(t *testing.T) {
	base := t.TempDir()
	started := time.Unix(1_700_000_000, 0)
	session := model.Session{ID: "gap", StartedAt: started}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []model.PowerSample{
		{Timestamp: started, SessionID: session.ID, PrimaryLoadW: 100, Battery: model.BatterySample{NetWatts: -100}},
		{Timestamp: started.Add(time.Hour), SessionID: session.ID, PrimaryLoadW: 100, Battery: model.BatterySample{NetWatts: -100}},
	} {
		if err := st.WriteSample(sample); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(filepath.Join(base, "sessions", session.ID))
	if err != nil {
		t.Fatal(err)
	}
	summary := artifact.Summary
	if summary.EnergyDischargedWh != 0 {
		t.Fatalf("sleep gap was integrated: %.3f Wh", summary.EnergyDischargedWh)
	}
	if len(summary.Warnings) == 0 {
		t.Fatal("expected integration-gap warning")
	}
}

func TestGenerateCreatesImmutableCumulativeSnapshots(t *testing.T) {
	base := t.TempDir()
	started := time.Unix(1_700_000_000, 0)
	session := model.Session{Schema: version.SessionSchema, ID: "cumulative", Version: version.Version, StartedAt: started}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	writeSample := func(sequence uint64, load float64) {
		t.Helper()
		if err := st.WriteSample(model.PowerSample{
			Schema:       version.PowerSampleSchema,
			Timestamp:    started.Add(time.Duration(sequence-1) * time.Minute),
			SessionID:    session.ID,
			Sequence:     sequence,
			PrimaryLoadW: load,
			Battery:      model.BatterySample{NetWatts: -load},
		}); err != nil {
			t.Fatal(err)
		}
	}

	writeSample(1, 10)
	firstSnapshot, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	first, err := GenerateSnapshot(firstSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	firstHTML, err := os.ReadFile(first.HTMLPath)
	if err != nil {
		t.Fatal(err)
	}

	writeSample(2, 20)
	secondSnapshot, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateSnapshot(secondSnapshot)
	if err != nil {
		t.Fatal(err)
	}

	if first.Directory == second.Directory || first.HTMLPath == second.HTMLPath {
		t.Fatalf("reports were overwritten: first=%q second=%q", first.Directory, second.Directory)
	}
	if first.Summary.SampleCount != 1 || second.Summary.SampleCount != 2 {
		t.Fatalf("cumulative counts first=%d second=%d", first.Summary.SampleCount, second.Summary.SampleCount)
	}
	unchanged, err := os.ReadFile(first.HTMLPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(firstHTML) {
		t.Fatal("first report changed after generating the second report")
	}
	latest, err := Latest(filepath.Join(base, "sessions", session.ID))
	if err != nil {
		t.Fatal(err)
	}
	if latest.HTMLPath != second.HTMLPath {
		t.Fatalf("latest=%q want=%q", latest.HTMLPath, second.HTMLPath)
	}
}

func TestGeneratePropagatesMalformedAppLog(t *testing.T) {
	base := t.TempDir()
	session := model.Session{ID: "malformed", StartedAt: time.Now()}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteSample(model.PowerSample{Timestamp: time.Now(), SessionID: session.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "sessions", session.ID)
	if err := os.WriteFile(filepath.Join(dir, "apps.jsonl"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(dir); err == nil {
		t.Fatal("expected malformed app log error")
	}
}

func TestGenerateIncludesCollectorErrorEvents(t *testing.T) {
	base := t.TempDir()
	session := model.Session{ID: "events", StartedAt: time.Now()}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteSample(model.PowerSample{Timestamp: time.Now(), SessionID: session.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteEvent(model.Event{Timestamp: time.Now(), SessionID: session.ID, Type: "collector_error", Message: "powermetrics restarted"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	artifact, err := Generate(filepath.Join(base, "sessions", session.ID))
	if err != nil {
		t.Fatal(err)
	}
	summary := artifact.Summary
	found := false
	for _, warning := range summary.Warnings {
		if warning == "collector_error: powermetrics restarted" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings=%v", summary.Warnings)
	}
}

func TestGenerateRejectsMismatchedSessionSample(t *testing.T) {
	base := t.TempDir()
	session := model.Session{ID: "expected", StartedAt: time.Now()}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteSample(model.PowerSample{Timestamp: time.Now(), SessionID: "other"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(filepath.Join(base, "sessions", session.ID)); err == nil {
		t.Fatal("expected session mismatch error")
	}
}

func TestGenerateRejectsOutOfOrderSamples(t *testing.T) {
	base := t.TempDir()
	start := time.Now()
	session := model.Session{ID: "ordered", StartedAt: start}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, timestamp := range []time.Time{start.Add(time.Second), start} {
		if err := st.WriteSample(model.PowerSample{Timestamp: timestamp, SessionID: session.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(filepath.Join(base, "sessions", session.ID)); err == nil {
		t.Fatal("expected out-of-order error")
	}
}
