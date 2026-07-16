package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestNewSessionRejectsDuplicateID(t *testing.T) {
	base := t.TempDir()
	session := model.Session{ID: "same", StartedAt: time.Now()}
	first, err := NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := NewSession(base, session, false); err == nil {
		t.Fatal("expected duplicate session ID to fail")
	}
}

func TestFlushMakesJSONLVisible(t *testing.T) {
	base := t.TempDir()
	store, err := NewSession(base, model.Session{ID: "session", StartedAt: time.Now()}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteEvent(model.Event{Type: "test", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.Dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("flush did not publish buffered event")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseReportsSessionMetadataFailure(t *testing.T) {
	base := t.TempDir()
	store, err := NewSession(base, model.Session{ID: "session", StartedAt: time.Now()}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(store.Dir); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err == nil {
		t.Fatal("expected close to report session metadata failure")
	}
}

func TestReadJSONLRejectsNilCallback(t *testing.T) {
	err := ReadJSONL[model.Event](filepath.Join(t.TempDir(), "missing"), nil)
	if err == nil {
		t.Fatal("expected callback validation error")
	}
}

type failingMirror struct {
	writes int
	closed int
}

func (f *failingMirror) write(string, time.Time, any) error {
	f.writes++
	return os.ErrInvalid
}
func (f *failingMirror) flush() error { return os.ErrInvalid }
func (f *failingMirror) close() error {
	f.closed++
	return nil
}

func TestSQLiteMirrorDisablesAfterFirstRuntimeFailure(t *testing.T) {
	store, err := NewSession(t.TempDir(), model.Session{ID: "session", StartedAt: time.Now()}, false)
	if err != nil {
		t.Fatal(err)
	}
	mirror := &failingMirror{}
	store.sqlite = mirror

	err = store.WriteSample(model.PowerSample{Timestamp: time.Now()})
	var degraded *MirrorDegradedError
	if !errors.As(err, &degraded) {
		t.Fatalf("err=%v want MirrorDegradedError", err)
	}
	if mirror.writes != 1 || mirror.closed != 1 || store.sqlite != nil {
		t.Fatalf("mirror=%+v sqlite=%v", mirror, store.sqlite)
	}
	if err := store.WriteSample(model.PowerSample{Timestamp: time.Now()}); err != nil {
		t.Fatalf("canonical JSONL should continue after mirror disable: %v", err)
	}
	if mirror.writes != 1 {
		t.Fatalf("mirror retried after disable: %d", mirror.writes)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSnapshotPinsJSONLByteBoundary(t *testing.T) {
	st, err := NewSession(t.TempDir(), model.Session{ID: "snapshot", StartedAt: time.Now()}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.WriteSample(model.PowerSample{SessionID: "snapshot", Sequence: 1, Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	first, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteSample(model.PowerSample{SessionID: "snapshot", Sequence: 2, Timestamp: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	second, err := st.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	count := func(snapshot SessionSnapshot) int {
		t.Helper()
		limit, ok := snapshot.Limit("samples.jsonl")
		if !ok {
			t.Fatal("samples limit missing")
		}
		seen := 0
		if err := ReadJSONLPrefix[model.PowerSample](filepath.Join(snapshot.Dir, "samples.jsonl"), limit, func(model.PowerSample) error {
			seen++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return seen
	}
	if got := count(first); got != 1 {
		t.Fatalf("first snapshot records=%d want=1", got)
	}
	if got := count(second); got != 2 {
		t.Fatalf("second snapshot records=%d want=2", got)
	}
}

func TestLiveSampleGateRetainsOnlyLatestPendingSample(t *testing.T) {
	st, err := NewSessionWithOptions(
		t.TempDir(),
		model.Session{ID: "cadence", StartedAt: time.Now()},
		SessionOptions{SampleLogging: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Unix(1_700_000_000, 0)
	offer := func(sequence uint64, offset time.Duration) model.PowerSample {
		t.Helper()
		sample := model.PowerSample{
			SessionID: "cadence",
			Sequence:  sequence,
			Timestamp: base.Add(offset),
			Warnings:  []string{"original"},
		}
		if err := st.OfferSample(sample); err != nil {
			t.Fatal(err)
		}
		return sample
	}
	offer(1, 0)
	offer(2, time.Second)
	third := offer(3, 2*time.Second)
	third.Warnings[0] = "mutated-after-offer"

	// Buffer durability is independent from pending-sample publication.
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := countSamples(t, st.Dir); got != 0 {
		t.Fatalf("samples after periodic flush=%d want=0", got)
	}
	if st.pendingSample == nil || st.pendingSample.Sequence != 3 || st.pendingSample.Warnings[0] != "original" {
		t.Fatalf("pending sample=%+v", st.pendingSample)
	}

	// The manager owns the durable clock and explicitly writes the cadence-due
	// frame. The superseded pending frame is intentionally discarded.
	if err := st.WriteSample(model.PowerSample{
		SessionID: "cadence",
		Sequence:  4,
		Timestamp: base.Add(5 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := countSamples(t, st.Dir); got != 1 {
		t.Fatalf("samples at cadence boundary=%d want=1", got)
	}
	if st.pendingSample != nil {
		t.Fatalf("pending sample was not cleared: %+v", st.pendingSample)
	}
}

func TestSnapshotAndCloseFlushLatestPendingSample(t *testing.T) {
	st, err := NewSessionWithOptions(
		t.TempDir(),
		model.Session{ID: "boundaries", StartedAt: time.Now()},
		SessionOptions{SampleLogging: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_700_000_000, 0)
	for sequence := uint64(1); sequence <= 3; sequence++ {
		if err := st.OfferSample(model.PowerSample{
			SessionID: "boundaries",
			Sequence:  sequence,
			Timestamp: base.Add(time.Duration(sequence) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if got := countSamples(t, st.Dir); got != 1 {
		t.Fatalf("snapshot samples=%d want latest=1", got)
	}
	if err := st.OfferSample(model.PowerSample{
		SessionID: "boundaries",
		Sequence:  4,
		Timestamp: base.Add(4 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if got := countSamples(t, st.Dir); got != 2 {
		t.Fatalf("close samples=%d want=2", got)
	}
}

func TestLiveOnlyDropsDurableSamples(t *testing.T) {
	st, err := NewSessionWithOptions(
		t.TempDir(),
		model.Session{ID: "live-only", StartedAt: time.Now()},
		SessionOptions{SampleLogging: false},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.OfferSample(model.PowerSample{
		SessionID: "live-only",
		Sequence:  1,
		Timestamp: time.Now(),
		Attribution: model.AttributionResult{
			Apps: []model.AppPower{{Name: "App", Key: "app"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if got := countSamples(t, st.Dir); got != 0 {
		t.Fatalf("live-only samples=%d want=0", got)
	}
	apps, err := os.ReadFile(filepath.Join(st.Dir, "apps.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("live-only apps log is not empty: %q", apps)
	}
}

func TestAppLogFailureDoesNotDuplicateCanonicalSample(t *testing.T) {
	st, err := NewSessionWithOptions(
		t.TempDir(),
		model.Session{ID: "partial-write", StartedAt: time.Now()},
		SessionOptions{SampleLogging: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	base := time.Unix(1_700_000_000, 0)
	appWriter := st.apps
	st.apps = nil
	err = st.WriteSample(model.PowerSample{
		SessionID: "partial-write",
		Sequence:  1,
		Timestamp: base,
		Attribution: model.AttributionResult{
			Apps: []model.AppPower{{Key: "app", Name: "App"}},
		},
	})
	st.apps = appWriter
	if err == nil {
		t.Fatal("expected injected app writer failure")
	}
	if st.pendingSample != nil {
		t.Fatalf("canonical sample was incorrectly retained for retry: %+v", st.pendingSample)
	}
	if err := st.OfferSample(model.PowerSample{
		SessionID: "partial-write",
		Sequence:  2,
		Timestamp: base.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if got := countSamples(t, st.Dir); got != 2 {
		t.Fatalf("canonical samples=%d want exactly first+latest", got)
	}
}

func countSamples(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	if err := ReadJSONL[model.PowerSample](
		filepath.Join(dir, "samples.jsonl"),
		func(model.PowerSample) error {
			count++
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	return count
}
