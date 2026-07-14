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
