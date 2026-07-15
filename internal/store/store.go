// Package store provides streaming JSONL and optional SQLite persistence.
package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

const (
	jsonlBufferSize  = 256 << 10
	scannerMaxRecord = 16 << 20
	sqliteCloseGrace = 5 * time.Second
)

// SessionStore owns all files for one monitoring session.
//
// All public methods are safe for concurrent use. JSONL is the canonical data
// source; SQLite is an optional mirror and its errors are surfaced rather than
// silently discarded.
type SessionStore struct {
	mu               sync.Mutex
	Dir              string
	Session          model.Session
	samples          *jsonlWriter
	apps             *jsonlWriter
	events           *jsonlWriter
	tests            *jsonlWriter
	sqlite           sqliteMirror
	sampleLogging    bool
	logInterval      time.Duration
	pendingSample    *model.PowerSample
	lastSampleLogged time.Time
	closed           bool
}

// SessionOptions configures optional mirrors and the durable live-sample gate.
// A zero LogInterval with SampleLogging enabled preserves the direct-write
// behavior used by compatibility callers and tests.
type SessionOptions struct {
	SQLite        bool
	SampleLogging bool
	LogInterval   time.Duration
}

// SessionSnapshot is an immutable byte-range view of one session at a precise
// capture time. JSONL files may continue growing after capture; Limits ensures
// report readers never consume records written after the snapshot boundary.
type SessionSnapshot struct {
	Dir        string
	CapturedAt time.Time
	Limits     map[string]int64
}

// Limit returns the captured byte length for a canonical session file.
func (snapshot SessionSnapshot) Limit(name string) (int64, bool) {
	limit, ok := snapshot.Limits[name]
	return limit, ok
}

type jsonlWriter struct {
	file *os.File
	buf  *bufio.Writer
	enc  *json.Encoder
}

type sqliteMirror interface {
	write(string, time.Time, any) error
	flush() error
	close() error
}

// MirrorDegradedError reports that canonical JSONL persistence succeeded but
// the optional SQLite mirror was disabled after a runtime failure. Callers may
// surface this once without treating the monitoring session as lost.
type MirrorDegradedError struct {
	Operation string
	Err       error
}

func (e *MirrorDegradedError) Error() string {
	return fmt.Sprintf("SQLite mirror disabled during %s: %v", e.Operation, e.Err)
}

func (e *MirrorDegradedError) Unwrap() error { return e.Err }

func newJSONL(path string) (*jsonlWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	buffer := bufio.NewWriterSize(f, jsonlBufferSize)
	return &jsonlWriter{file: f, buf: buffer, enc: json.NewEncoder(buffer)}, nil
}

func (w *jsonlWriter) write(v any) error {
	if w == nil || w.file == nil || w.buf == nil || w.enc == nil {
		return errors.New("JSONL writer is not initialized")
	}
	return w.enc.Encode(v)
}

func (w *jsonlWriter) flush() error {
	if w == nil || w.buf == nil {
		return nil
	}
	return w.buf.Flush()
}

func (w *jsonlWriter) close() error {
	if w == nil {
		return nil
	}
	var errs []error
	if w.buf != nil {
		errs = append(errs, w.buf.Flush())
	}
	if w.file != nil {
		errs = append(errs, w.file.Sync(), w.file.Close())
	}
	return errors.Join(errs...)
}

// NewSession creates an isolated session directory with private permissions.
// Existing session IDs are rejected so two sessions can never append into the
// same JSONL files.
func NewSession(base string, session model.Session, withSQLite bool) (_ *SessionStore, retErr error) {
	return NewSessionWithOptions(base, session, SessionOptions{
		SQLite:        withSQLite,
		SampleLogging: true,
	})
}

// NewSessionWithOptions creates a session with explicit durable-sample
// behavior. Live-only sessions still retain private session/event/test files,
// but their sample and app JSONL files remain empty.
func NewSessionWithOptions(
	base string,
	session model.Session,
	options SessionOptions,
) (_ *SessionStore, retErr error) {
	if strings.TrimSpace(base) == "" {
		return nil, errors.New("session base directory is required")
	}
	if strings.TrimSpace(session.ID) == "" {
		return nil, errors.New("session ID is required")
	}
	if options.LogInterval < 0 {
		return nil, errors.New("sample log interval must not be negative")
	}
	if !options.SampleLogging && options.LogInterval != 0 {
		return nil, errors.New("sample log interval must be zero when sample logging is disabled")
	}

	sessionsDir := filepath.Join(base, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create sessions directory: %w", err)
	}
	dir := filepath.Join(sessionsDir, session.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create unique session directory: %w", err)
	}
	cleanupDir := true
	defer func() {
		if retErr != nil && cleanupDir {
			_ = os.RemoveAll(dir)
		}
	}()

	session.DataDirectory = dir
	s := &SessionStore{
		Dir:           dir,
		Session:       session,
		sampleLogging: options.SampleLogging,
		logInterval:   options.LogInterval,
	}
	cleanupWriters := func() {
		_ = errors.Join(
			s.samples.close(),
			s.apps.close(),
			s.events.close(),
			s.tests.close(),
		)
		if s.sqlite != nil {
			_ = s.sqlite.close()
		}
	}

	var err error
	if s.samples, err = newJSONL(filepath.Join(dir, "samples.jsonl")); err != nil {
		cleanupWriters()
		return nil, fmt.Errorf("create samples log: %w", err)
	}
	if s.apps, err = newJSONL(filepath.Join(dir, "apps.jsonl")); err != nil {
		cleanupWriters()
		return nil, fmt.Errorf("create apps log: %w", err)
	}
	if s.events, err = newJSONL(filepath.Join(dir, "events.jsonl")); err != nil {
		cleanupWriters()
		return nil, fmt.Errorf("create events log: %w", err)
	}
	if s.tests, err = newJSONL(filepath.Join(dir, "test_runs.jsonl")); err != nil {
		cleanupWriters()
		return nil, fmt.Errorf("create test-runs log: %w", err)
	}

	if options.SQLite {
		s.sqlite, err = newSQLite(filepath.Join(dir, "session.sqlite3"))
		if err != nil {
			var missing *exec.Error
			if !errors.As(err, &missing) {
				cleanupWriters()
				return nil, fmt.Errorf("initialize SQLite mirror: %w", err)
			}
			// sqlite3 is an optional capability. Absence is not session failure.
			s.sqlite = nil
		}
	}
	if err := s.writeSession(); err != nil {
		cleanupWriters()
		return nil, fmt.Errorf("write session metadata: %w", err)
	}
	cleanupDir = false
	return s, nil
}

func (s *SessionStore) WriteSample(v model.PowerSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	err := s.writeSampleLocked(v)
	if s.lastSampleLogged.Equal(v.Timestamp) {
		s.pendingSample = nil
	}
	return err
}

// OfferSample accepts every live sample while durably writing only at the
// configured cadence. Between writes it retains exactly one deep-copied latest
// sample, replacing any older pending value.
func (s *SessionStore) OfferSample(v model.PowerSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	if !s.sampleLogging {
		return nil
	}
	if !s.lastSampleLogged.IsZero() && !v.Timestamp.After(s.lastSampleLogged) {
		return fmt.Errorf(
			"live sample timestamp must advance beyond %s: %s",
			s.lastSampleLogged.Format(time.RFC3339Nano),
			v.Timestamp.Format(time.RFC3339Nano),
		)
	}
	due := s.logInterval == 0 ||
		s.lastSampleLogged.IsZero() ||
		!v.Timestamp.Before(s.lastSampleLogged.Add(s.logInterval))
	if !due {
		copyValue := clonePowerSample(v)
		s.pendingSample = &copyValue
		return nil
	}

	err := s.writeSampleLocked(v)
	if s.lastSampleLogged.Equal(v.Timestamp) {
		s.pendingSample = nil
	} else {
		copyValue := clonePowerSample(v)
		s.pendingSample = &copyValue
	}
	return err
}

func (s *SessionStore) writeSampleLocked(v model.PowerSample) error {
	if err := s.samples.write(v); err != nil {
		return fmt.Errorf("write sample JSONL: %w", err)
	}
	// samples.jsonl is the canonical sample stream. Advance its durable gate
	// immediately so a later app-row or optional-mirror failure cannot cause a
	// duplicate canonical sample on the next offer.
	s.lastSampleLogged = v.Timestamp
	for _, app := range v.Attribution.Apps {
		if err := s.apps.write(app); err != nil {
			return fmt.Errorf("write app JSONL: %w", err)
		}
	}
	if s.sqlite != nil {
		if err := s.sqlite.write("samples", v.Timestamp, v); err != nil {
			return s.disableSQLiteLocked("sample write", err)
		}
		for _, app := range v.Attribution.Apps {
			if err := s.sqlite.write("apps", v.Timestamp, app); err != nil {
				return s.disableSQLiteLocked("app write", err)
			}
		}
	}
	return nil
}

// FlushPendingSample durably writes the latest pending live sample regardless
// of cadence. It is reserved for semantic boundaries, not periodic buffer
// flushing, so a slow log cadence cannot accidentally become a two-second one.
func (s *SessionStore) FlushPendingSample() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.flushPendingSampleLocked()
}

func (s *SessionStore) flushPendingSampleLocked() error {
	if s.pendingSample == nil {
		return nil
	}
	pending := clonePowerSample(*s.pendingSample)
	err := s.writeSampleLocked(pending)
	if s.lastSampleLogged.Equal(pending.Timestamp) {
		s.pendingSample = nil
	}
	return err
}

func (s *SessionStore) WriteEvent(v model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	if err := s.events.write(v); err != nil {
		return fmt.Errorf("write event JSONL: %w", err)
	}
	if s.sqlite != nil {
		if err := s.sqlite.write("events", v.Timestamp, v); err != nil {
			return s.disableSQLiteLocked("event write", err)
		}
	}
	return nil
}

func (s *SessionStore) WriteTestRun(v model.TestRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	if err := s.tests.write(v); err != nil {
		return fmt.Errorf("write test-run JSONL: %w", err)
	}
	if s.sqlite != nil {
		if err := s.sqlite.write("test_runs", v.StartedAt, v); err != nil {
			return s.disableSQLiteLocked("test-run write", err)
		}
	}
	return nil
}

func (s *SessionStore) disableSQLiteLocked(operation string, cause error) error {
	if s.sqlite == nil {
		return nil
	}
	sink := s.sqlite
	s.sqlite = nil
	closeErr := sink.close()
	return &MirrorDegradedError{
		Operation: operation,
		Err:       errors.Join(cause, closeErr),
	}
}

// Snapshot flushes canonical JSONL writers and captures byte boundaries while
// holding the store mutex. The monitor may resume writing immediately afterward;
// report readers remain pinned to these limits and therefore see a consistent,
// cumulative view through CapturedAt without pausing collection for the full
// report-generation duration.
func (s *SessionStore) Snapshot() (SessionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		if err := s.flushPendingSampleLocked(); err != nil {
			return SessionSnapshot{}, fmt.Errorf("flush pending session sample: %w", err)
		}
		if err := errors.Join(
			s.samples.flush(),
			s.apps.flush(),
			s.events.flush(),
			s.tests.flush(),
		); err != nil {
			return SessionSnapshot{}, fmt.Errorf("flush session snapshot: %w", err)
		}
	}
	return snapshotDir(s.Dir, time.Now())
}

// SnapshotDir captures an already-closed or externally selected session.
func SnapshotDir(dir string) (SessionSnapshot, error) {
	return snapshotDir(dir, time.Now())
}

func snapshotDir(dir string, capturedAt time.Time) (SessionSnapshot, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "." || dir == "" {
		return SessionSnapshot{}, errors.New("session snapshot directory is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if !info.IsDir() {
		return SessionSnapshot{}, errors.New("session snapshot path must be a directory")
	}

	limits := make(map[string]int64, 4)
	for _, name := range []string{"samples.jsonl", "apps.jsonl", "events.jsonl", "test_runs.jsonl"} {
		fileInfo, statErr := os.Stat(filepath.Join(dir, name))
		switch {
		case statErr == nil:
			if !fileInfo.Mode().IsRegular() {
				return SessionSnapshot{}, fmt.Errorf("snapshot source is not a regular file: %s", name)
			}
			limits[name] = fileInfo.Size()
		case errors.Is(statErr, os.ErrNotExist):
			continue
		default:
			return SessionSnapshot{}, fmt.Errorf("stat snapshot source %s: %w", name, statErr)
		}
	}
	return SessionSnapshot{Dir: dir, CapturedAt: capturedAt, Limits: limits}, nil
}

// Flush persists all buffered data without closing the session.
func (s *SessionStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	var errs []error
	errs = append(errs,
		s.samples.flush(),
		s.apps.flush(),
		s.events.flush(),
		s.tests.flush(),
	)
	if s.sqlite != nil {
		if err := s.sqlite.flush(); err != nil {
			errs = append(errs, s.disableSQLiteLocked("flush", err))
		}
	}
	return errors.Join(errs...)
}

func (s *SessionStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	pendingErr := s.flushPendingSampleLocked()
	s.closed = true
	s.Session.EndedAt = time.Now()

	var errs []error
	errs = append(errs, pendingErr)
	errs = append(errs, s.writeSession())
	errs = append(errs,
		s.samples.close(),
		s.apps.close(),
		s.events.close(),
		s.tests.close(),
	)
	if s.sqlite != nil {
		errs = append(errs, s.sqlite.close())
	}
	return errors.Join(errs...)
}

func clonePowerSample(value model.PowerSample) model.PowerSample {
	value.Components.Clusters = append([]model.ClusterSample(nil), value.Components.Clusters...)
	value.Attribution.Apps = append([]model.AppPower(nil), value.Attribution.Apps...)
	if value.CollectorStatus != nil {
		statuses := make(map[string]string, len(value.CollectorStatus))
		for key, item := range value.CollectorStatus {
			statuses[key] = item
		}
		value.CollectorStatus = statuses
	}
	value.Warnings = append([]string(nil), value.Warnings...)
	return value
}

func (s *SessionStore) writeSession() error {
	return atomicJSON(filepath.Join(s.Dir, "session.json"), s.Session)
}

func atomicJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ReadJSONL streams a complete JSONL file one object at a time without
// unbounded memory.
func ReadJSONL[T any](path string, fn func(T) error) error {
	return ReadJSONLPrefix(path, -1, fn)
}

// ReadJSONLPrefix reads at most limit bytes. A non-negative limit should come
// from SessionSnapshot and is guaranteed to end at a flushed record boundary.
// Passing -1 reads the complete file.
func ReadJSONLPrefix[T any](path string, limit int64, fn func(T) error) error {
	if fn == nil {
		return errors.New("JSONL callback is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = f
	if limit >= 0 {
		reader = io.LimitReader(f, limit)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), scannerMaxRecord)
	line := 0
	for scanner.Scan() {
		line++
		var value T
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return fmt.Errorf("%s line %d: %w", path, line, err)
		}
		if err := fn(value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type sqliteSink struct {
	cmd    *exec.Cmd
	in     *bufio.Writer
	stdin  io.WriteCloser
	stderr *execx.LimitedBuffer
	mu     sync.Mutex
	closed bool
}

func newSQLite(path string) (*sqliteSink, error) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, path)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stderr := execx.NewLimitedBuffer(1 << 20)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = pipe.Close()
		return nil, err
	}
	sink := &sqliteSink{
		cmd:    cmd,
		in:     bufio.NewWriterSize(pipe, 128<<10),
		stdin:  pipe,
		stderr: stderr,
	}
	const schema = "PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; " +
		"CREATE TABLE IF NOT EXISTS records(" +
		"kind TEXT NOT NULL, timestamp TEXT NOT NULL, json TEXT NOT NULL); " +
		"CREATE INDEX IF NOT EXISTS records_kind_time ON records(kind,timestamp);\n"
	if _, err := sink.in.WriteString(schema); err != nil {
		_ = sink.close()
		return nil, err
	}
	if err := sink.in.Flush(); err != nil {
		_ = sink.close()
		return nil, err
	}
	return sink, nil
}

func (s *sqliteSink) write(kind string, ts time.Time, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("SQLite sink closed")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	kind = strings.ReplaceAll(kind, "'", "''")
	payload := strings.ReplaceAll(string(b), "'", "''")
	_, err = fmt.Fprintf(
		s.in,
		"INSERT INTO records(kind,timestamp,json) VALUES('%s','%s','%s');\n",
		kind,
		ts.Format(time.RFC3339Nano),
		payload,
	)
	return err
}

func (s *sqliteSink) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.in.Flush()
}

func (s *sqliteSink) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	var writeErr error
	if s.in != nil {
		if _, err := s.in.WriteString("PRAGMA wal_checkpoint(TRUNCATE);\n.quit\n"); err != nil {
			writeErr = errors.Join(writeErr, err)
		}
		if err := s.in.Flush(); err != nil {
			writeErr = errors.Join(writeErr, err)
		}
	}
	if s.stdin != nil {
		if err := s.stdin.Close(); err != nil {
			writeErr = errors.Join(writeErr, err)
		}
	}
	s.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(sqliteCloseGrace):
		_ = s.cmd.Process.Kill()
		waitErr = <-done
		waitErr = errors.Join(errors.New("sqlite3 close timed out"), waitErr)
	}
	if waitErr != nil {
		waitErr = fmt.Errorf("sqlite3 failed: %w: %s", waitErr, strings.TrimSpace(string(s.stderr.Bytes())))
	}
	return errors.Join(writeErr, waitErr)
}

// LatestSessionDir returns the most recently modified session directory.
func LatestSessionDir(base string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(base, "sessions"))
	if err != nil {
		return "", err
	}
	var latest string
	var latestTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = filepath.Join(base, "sessions", entry.Name())
		}
	}
	if latest == "" {
		return "", errors.New("no sessions found")
	}
	return latest, nil
}

// ContextFlush periodically flushes buffers for crash resilience. Errors are
// reported through onError when provided.
func (s *SessionStore) ContextFlush(
	ctx context.Context,
	interval time.Duration,
	onError func(error),
) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Flush(); err != nil && onError != nil {
				onError(fmt.Errorf("flush session store: %w", err))
			}
		}
	}
}
