package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	stopErr      error
	started      int
	stopped      int
	flushed      int
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
	return f.stopErr
}
func (f *fakeEngineMonitor) FlushPending() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed++
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
		setPriority:  func(context.Context, int) error { return nil },
		saveSettings: func(string, model.RuntimeSettings) error { return nil },
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

func TestApplyRuntimeSettingsRestartsIntoFreshSessionAndPersists(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	var (
		mu        sync.Mutex
		configs   []config.Config
		monitors  []*fakeEngineMonitor
		savedPath string
		saved     model.RuntimeSettings
	)
	deps := engineDependencies{
		newMonitor: func(value config.Config) (monitor, error) {
			mu.Lock()
			defer mu.Unlock()
			index := len(monitors) + 1
			fake := &fakeEngineMonitor{
				sample: &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
				session: model.Session{
					ID:              fmt.Sprintf("session-%d", index),
					RuntimeSettings: value.Runtime,
				},
				dir: t.TempDir(),
			}
			configs = append(configs, value)
			monitors = append(monitors, fake)
			return fake, nil
		},
		newBenchmark: func(benchmark.Monitor) benchmarkRunner { return newFakeEngineBenchmark() },
		ensureSudo:   func(context.Context, bool) error { return nil },
		setPriority:  func(context.Context, int) error { return nil },
		saveSettings: func(path string, settings model.RuntimeSettings) error {
			savedPath = path
			saved = settings
			return nil
		},
	}
	engine := newEngine(cfg, deps)
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings, err := config.SettingsForProfile(config.ProfileBalanced)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := engine.ApplyRuntimeSettings(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted {
		t.Fatal("running monitor was not reported as restarted")
	}
	mu.Lock()
	if len(configs) != 2 || len(monitors) != 2 {
		t.Fatalf("configs=%d monitors=%d", len(configs), len(monitors))
	}
	first, second := monitors[0], monitors[1]
	mu.Unlock()
	first.mu.Lock()
	firstStopped, firstFlushed := first.stopped, first.flushed
	first.mu.Unlock()
	if firstStopped != 1 || firstFlushed != 1 {
		t.Fatalf("old monitor stopped=%d flushed=%d", firstStopped, firstFlushed)
	}
	second.mu.Lock()
	secondStarted := second.started
	second.mu.Unlock()
	if secondStarted != 1 {
		t.Fatalf("new monitor starts=%d", secondStarted)
	}
	if savedPath != cfg.DataDir || !config.RuntimeSettingsEqual(saved, settings) {
		t.Fatalf("saved path=%q settings=%+v", savedPath, saved)
	}
	if !config.RuntimeSettingsEqual(engine.RuntimeSettings(), settings) {
		t.Fatalf("effective settings=%+v", engine.RuntimeSettings())
	}
	status := engine.Status()
	if status.Session == nil || status.Session.ID != "session-2" {
		t.Fatalf("status session=%+v", status.Session)
	}
}

func TestApplyRuntimeSettingsPersistenceFailureLeavesOldMonitorRunning(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	var (
		monitors []*fakeEngineMonitor
		saves    []model.RuntimeSettings
	)
	deps := engineDependencies{
		newMonitor: func(value config.Config) (monitor, error) {
			fake := &fakeEngineMonitor{
				sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
				session: model.Session{ID: fmt.Sprintf("session-%d", len(monitors)+1), RuntimeSettings: value.Runtime},
				dir:     t.TempDir(),
			}
			monitors = append(monitors, fake)
			return fake, nil
		},
		newBenchmark: func(benchmark.Monitor) benchmarkRunner { return newFakeEngineBenchmark() },
		ensureSudo:   func(context.Context, bool) error { return nil },
		setPriority:  func(context.Context, int) error { return nil },
		saveSettings: func(_ string, settings model.RuntimeSettings) error {
			saves = append(saves, settings)
			return errors.New("disk full")
		},
	}
	engine := newEngine(cfg, deps)
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings, _ := config.SettingsForProfile(config.ProfileLowOverhead)
	if _, err := engine.ApplyRuntimeSettings(context.Background(), settings); err == nil || !strings.Contains(err.Error(), "persist runtime settings") {
		t.Fatalf("err=%v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("monitor creations=%d want only the original", len(monitors))
	}
	monitors[0].mu.Lock()
	stopped := monitors[0].stopped
	monitors[0].mu.Unlock()
	if stopped != 0 {
		t.Fatalf("old monitor stopped after persistence failure: %d", stopped)
	}
	status := engine.Status()
	if status.Session == nil || status.Session.ID != "session-1" {
		t.Fatalf("status session=%+v", status.Session)
	}
	if len(saves) != 1 || !config.RuntimeSettingsEqual(saves[0], settings) {
		t.Fatalf("persistence attempts=%+v", saves)
	}
	if !config.RuntimeSettingsEqual(engine.RuntimeSettings(), cfg.Runtime) {
		t.Fatalf("runtime changed after failed persistence: %+v", engine.RuntimeSettings())
	}
}

func TestApplyRuntimeSettingsStopFailureDoesNotStartRollbackMonitor(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	var (
		monitors []*fakeEngineMonitor
		saves    []model.RuntimeSettings
	)
	deps := engineDependencies{
		newMonitor: func(value config.Config) (monitor, error) {
			fake := &fakeEngineMonitor{
				stopErr: errors.New("monitor shutdown timed out"),
				sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
				session: model.Session{ID: "session-1", RuntimeSettings: value.Runtime},
				dir:     t.TempDir(),
			}
			monitors = append(monitors, fake)
			return fake, nil
		},
		newBenchmark: func(benchmark.Monitor) benchmarkRunner { return newFakeEngineBenchmark() },
		ensureSudo:   func(context.Context, bool) error { return nil },
		setPriority:  func(context.Context, int) error { return nil },
		saveSettings: func(_ string, settings model.RuntimeSettings) error {
			saves = append(saves, settings)
			return nil
		},
	}
	engine := newEngine(cfg, deps)
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings, _ := config.SettingsForProfile(config.ProfileBalanced)
	if _, err := engine.ApplyRuntimeSettings(context.Background(), settings); err == nil || !strings.Contains(err.Error(), "monitor shutdown timed out") {
		t.Fatalf("err=%v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("monitor creations=%d; rollback must not overlap unresolved shutdown", len(monitors))
	}
	if len(saves) != 2 ||
		!config.RuntimeSettingsEqual(saves[0], settings) ||
		!config.RuntimeSettingsEqual(saves[1], cfg.Runtime) {
		t.Fatalf("persistence attempts=%+v", saves)
	}
	status := engine.Status()
	if status.Session == nil || status.Session.ID != "session-1" || !status.MonitorRunning {
		t.Fatalf("old monitor should remain published: %+v", status)
	}
}

func TestApplyRuntimeSettingsStartFailureRestoresPreviousMonitor(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	var (
		monitors []*fakeEngineMonitor
		saves    []model.RuntimeSettings
	)
	deps := engineDependencies{
		newMonitor: func(value config.Config) (monitor, error) {
			fake := &fakeEngineMonitor{
				sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
				session: model.Session{ID: fmt.Sprintf("session-%d", len(monitors)+1), RuntimeSettings: value.Runtime},
				dir:     t.TempDir(),
			}
			if len(monitors) == 1 {
				fake.startErr = errors.New("new monitor startup failed")
			}
			monitors = append(monitors, fake)
			return fake, nil
		},
		newBenchmark: func(benchmark.Monitor) benchmarkRunner { return newFakeEngineBenchmark() },
		ensureSudo:   func(context.Context, bool) error { return nil },
		setPriority:  func(context.Context, int) error { return nil },
		saveSettings: func(_ string, settings model.RuntimeSettings) error {
			saves = append(saves, settings)
			return nil
		},
	}
	engine := newEngine(cfg, deps)
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
	settings, _ := config.SettingsForProfile(config.ProfileLowOverhead)
	if _, err := engine.ApplyRuntimeSettings(context.Background(), settings); err == nil || !strings.Contains(err.Error(), "new monitor startup failed") {
		t.Fatalf("err=%v", err)
	}
	if len(monitors) != 3 {
		t.Fatalf("monitor creations=%d want old, failed-new, rollback", len(monitors))
	}
	status := engine.Status()
	if status.Session == nil || status.Session.ID != "session-3" {
		t.Fatalf("rollback status=%+v", status.Session)
	}
	if len(saves) != 2 ||
		!config.RuntimeSettingsEqual(saves[0], settings) ||
		!config.RuntimeSettingsEqual(saves[1], cfg.Runtime) {
		t.Fatalf("persistence attempts=%+v", saves)
	}
	if !config.RuntimeSettingsEqual(engine.RuntimeSettings(), cfg.Runtime) {
		t.Fatalf("runtime changed after rollback: %+v", engine.RuntimeSettings())
	}
}

func TestGenerateReportRejectsLiveOnlyRunningSession(t *testing.T) {
	cfg := config.Default()
	settings, err := config.SettingsForProfile(config.ProfileLiveOnly)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Runtime = settings
	fake := &fakeEngineMonitor{
		sample:  &model.PowerSample{},
		session: model.Session{ID: "live-only", RuntimeSettings: settings},
		dir:     t.TempDir(),
	}
	engine := newEngine(
		cfg,
		testEngineDependencies(func() (monitor, error) { return fake, nil }, newFakeEngineBenchmark()),
	)
	if err := engine.StartMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.GenerateReport(context.Background()); !errors.Is(err, ErrHistoricalLoggingDisabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyRuntimeSettingsStoppedRollsBackPriorityAndPersistence(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	var (
		priorities []int
		saves      []model.RuntimeSettings
	)
	deps := testEngineDependencies(
		func() (monitor, error) { return nil, errors.New("monitor should not be created") },
		newFakeEngineBenchmark(),
	)
	deps.setPriority = func(_ context.Context, value int) error {
		priorities = append(priorities, value)
		return nil
	}
	deps.saveSettings = func(_ string, settings model.RuntimeSettings) error {
		saves = append(saves, settings)
		if len(saves) == 1 {
			return errors.New("directory sync failed")
		}
		return nil
	}
	engine := newEngine(cfg, deps)
	settings, _ := config.SettingsForProfile(config.ProfileLowOverhead)
	if _, err := engine.ApplyRuntimeSettings(context.Background(), settings); err == nil {
		t.Fatal("expected persistence failure")
	}
	if len(priorities) != 2 || priorities[0] != settings.ProcessNice || priorities[1] != cfg.Runtime.ProcessNice {
		t.Fatalf("priority transitions=%v", priorities)
	}
	if len(saves) != 2 ||
		!config.RuntimeSettingsEqual(saves[0], settings) ||
		!config.RuntimeSettingsEqual(saves[1], cfg.Runtime) {
		t.Fatalf("persistence attempts=%+v", saves)
	}
	if !config.RuntimeSettingsEqual(engine.RuntimeSettings(), cfg.Runtime) {
		t.Fatalf("effective settings changed: %+v", engine.RuntimeSettings())
	}
}

func TestApplyRuntimeSettingsRejectsRunningBenchmarkWithoutSideEffects(t *testing.T) {
	fake := &fakeEngineMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	runner := newFakeEngineBenchmark()
	saves := atomic.Int64{}
	deps := testEngineDependencies(func() (monitor, error) { return fake, nil }, runner)
	deps.saveSettings = func(string, model.RuntimeSettings) error {
		saves.Add(1)
		return nil
	}
	engine := newEngine(config.Default(), deps)
	if err := engine.StartBenchmark(context.Background(), "quick", 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("benchmark did not start")
	}
	settings, _ := config.SettingsForProfile(config.ProfileBalanced)
	if _, err := engine.ApplyRuntimeSettings(context.Background(), settings); !errors.Is(err, ErrBenchmarkRunning) {
		t.Fatalf("err=%v", err)
	}
	if saves.Load() != 0 {
		t.Fatalf("settings were persisted %d times", saves.Load())
	}
	fake.mu.Lock()
	stopped := fake.stopped
	fake.mu.Unlock()
	if stopped != 0 {
		t.Fatalf("monitor stopped during rejected update: %d", stopped)
	}
	if err := engine.StopMonitor(context.Background()); err != nil {
		t.Fatal(err)
	}
}
