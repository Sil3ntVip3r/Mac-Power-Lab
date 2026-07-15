package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/benchmark"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/store"
)

type fakeEngineMonitor struct {
	mu           sync.Mutex
	startErr     error
	started      int
	stopped      int
	phase        string
	sample       *model.PowerSample
	session      model.Session
	dir          string
	sessionStore *store.SessionStore
}

func (f *fakeEngineMonitor) Start(context.Context, map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	return f.startErr
}
func (f *fakeEngineMonitor) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped++
	return nil
}
func (f *fakeEngineMonitor) LastSample() *model.PowerSample {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sample == nil {
		return nil
	}
	value := *f.sample
	return &value
}
func (f *fakeEngineMonitor) Session() model.Session { return f.session }
func (f *fakeEngineMonitor) SessionDir() string     { return f.dir }
func (f *fakeEngineMonitor) SetPhase(value string) {
	f.mu.Lock()
	f.phase = value
	f.mu.Unlock()
}
func (f *fakeEngineMonitor) WriteTestRun(model.TestRun) error { return nil }
func (f *fakeEngineMonitor) Snapshot() (store.SessionSnapshot, error) {
	if f.sessionStore != nil {
		return f.sessionStore.Snapshot()
	}
	return store.SnapshotDir(f.dir)
}

type fakeEngineBenchmark struct {
	runs    atomic.Int64
	started chan int64
	mu      sync.RWMutex
	status  model.BenchmarkProgress
}

func newFakeEngineBenchmark() *fakeEngineBenchmark {
	return &fakeEngineBenchmark{started: make(chan int64, 16)}
}
func (f *fakeEngineBenchmark) Run(ctx context.Context, plan benchmark.Plan, _ benchmark.Options) error {
	id := f.runs.Add(1)
	f.mu.Lock()
	f.status = model.BenchmarkProgress{Running: true, Plan: plan.Name, Status: "running"}
	f.mu.Unlock()
	f.started <- id
	<-ctx.Done()
	f.mu.Lock()
	f.status = model.BenchmarkProgress{Running: false, Plan: plan.Name, Status: "stopped"}
	f.mu.Unlock()
	return ctx.Err()
}
func (f *fakeEngineBenchmark) Progress() model.BenchmarkProgress {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.status
}

func testEngineDependencies(factory func() (monitor, error), runner benchmarkRunner) engineDependencies {
	return engineDependencies{
		newMonitor:   func(config.Config) (monitor, error) { return factory() },
		newBenchmark: func(benchmark.Monitor) benchmarkRunner { return runner },
		ensureSudo:   func(context.Context, bool) error { return nil },
	}
}

func TestConcurrentStartMonitorCreatesOneManager(t *testing.T) {
	var creates atomic.Int64
	fake := &fakeEngineMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	engine := newEngine(
		config.Default(),
		testEngineDependencies(func() (monitor, error) {
			creates.Add(1)
			return fake, nil
		}, newFakeEngineBenchmark()),
	)

	var wait sync.WaitGroup
	errorsCh := make(chan error, 32)
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsCh <- engine.StartMonitor(context.Background())
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := creates.Load(); got != 1 {
		t.Fatalf("manager creations=%d, want 1", got)
	}
	fake.mu.Lock()
	started := fake.started
	fake.mu.Unlock()
	if started != 1 {
		t.Fatalf("monitor starts=%d, want 1", started)
	}
	if err := engine.StopMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFailedMonitorStartCanBeRetried(t *testing.T) {
	var attempts atomic.Int64
	runner := newFakeEngineBenchmark()
	engine := newEngine(
		config.Default(),
		testEngineDependencies(func() (monitor, error) {
			attempt := attempts.Add(1)
			fake := &fakeEngineMonitor{
				sample:  &model.PowerSample{},
				session: model.Session{ID: "session"},
				dir:     t.TempDir(),
			}
			if attempt == 1 {
				fake.startErr = errors.New("startup failed")
			}
			return fake, nil
		}, runner),
	)
	if err := engine.StartMonitor(context.Background()); err == nil {
		t.Fatal("expected first startup to fail")
	}
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
}

func TestStopMonitorCancelsAndWaitsForBenchmark(t *testing.T) {
	fake := &fakeEngineMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	runner := newFakeEngineBenchmark()
	engine := newEngine(
		config.Default(),
		testEngineDependencies(func() (monitor, error) { return fake, nil }, runner),
	)
	if err := engine.StartBenchmark(context.Background(), "quick", 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("benchmark did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.StopMonitor(ctx); err != nil {
		t.Fatal(err)
	}
	status := engine.Status()
	if status.MonitorRunning || status.Benchmark.Running {
		t.Fatalf("status=%+v", status)
	}
	fake.mu.Lock()
	stopped := fake.stopped
	fake.mu.Unlock()
	if stopped != 1 {
		t.Fatalf("monitor stops=%d", stopped)
	}
}

func TestCompletedRunCannotClearNewBenchmark(t *testing.T) {
	fake := &fakeEngineMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	runner := newFakeEngineBenchmark()
	engine := newEngine(
		config.Default(),
		testEngineDependencies(func() (monitor, error) { return fake, nil }, runner),
	)
	if err := engine.StartBenchmark(context.Background(), "quick", 0); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	if err := engine.StopBenchmark(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := engine.StartBenchmark(context.Background(), "quick", 0); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	engine.mu.RLock()
	second := engine.run
	engine.mu.RUnlock()
	if second == nil {
		t.Fatal("second run was cleared")
	}
	if err := engine.StopBenchmark(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEngineGenerateReportCreatesCumulativeArtifacts(t *testing.T) {
	base := t.TempDir()
	session := model.Session{ID: "report-session", StartedAt: time.Now()}
	st, err := store.NewSession(base, session, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fake := &fakeEngineMonitor{
		sample:       &model.PowerSample{},
		session:      st.Session,
		dir:          st.Dir,
		sessionStore: st,
	}
	engine := newEngine(
		config.Default(),
		testEngineDependencies(func() (monitor, error) { return fake, nil }, newFakeEngineBenchmark()),
	)
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}

	write := func(sequence uint64) {
		t.Helper()
		if err := st.WriteSample(model.PowerSample{
			Timestamp: time.Now().Add(time.Duration(sequence) * time.Second),
			SessionID: session.ID,
			Sequence:  sequence,
		}); err != nil {
			t.Fatal(err)
		}
	}
	write(1)
	first, err := engine.GenerateReport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	write(2)
	second, err := engine.GenerateReport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.HTMLPath == second.HTMLPath {
		t.Fatal("report generation overwrote the first artifact")
	}
	if first.Summary.SampleCount != 1 || second.Summary.SampleCount != 2 {
		t.Fatalf("sample counts first=%d second=%d", first.Summary.SampleCount, second.Summary.SampleCount)
	}
}

func TestCancelledEngineContextRejectsNewMonitor(t *testing.T) {
	fake := &fakeEngineMonitor{
		sample:  &model.PowerSample{},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	engine := newEngine(
		config.Default(),
		testEngineDependencies(
			func() (monitor, error) { return fake, nil },
			newFakeEngineBenchmark(),
		),
	)
	root, cancel := context.WithCancel(context.Background())
	engine.bindContext(root)
	cancel()
	if err := engine.StartMonitor(context.Background()); err == nil {
		t.Fatal("expected cancelled engine context to reject startup")
	}
	fake.mu.Lock()
	started := fake.started
	fake.mu.Unlock()
	if started != 0 {
		t.Fatalf("monitor started %d times", started)
	}
}
