package benchmark

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestCatalogPlansAreValid(t *testing.T) {
	catalog := Catalog()
	if len(catalog) < 10 {
		t.Fatalf("catalog has %d entries; want at least 10", len(catalog))
	}
	seen := map[string]bool{}
	for _, definition := range catalog {
		if definition.ID == "" || definition.Name == "" || definition.Description == "" {
			t.Fatalf("incomplete definition: %+v", definition)
		}
		if seen[definition.ID] {
			t.Fatalf("duplicate benchmark ID %q", definition.ID)
		}
		seen[definition.ID] = true

		plan, err := PlanByID(definition.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Phases) == 0 {
			t.Fatalf("%s has no phases", definition.ID)
		}
		for _, phase := range plan.Phases {
			if err := validatePhase(phase); err != nil {
				t.Fatalf("%s: %v", definition.ID, err)
			}
		}
	}
}

func TestAdjustablePlanDuration(t *testing.T) {
	plan, err := PlanByID("cpu", 7*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Phases[0].Duration; got != 7*time.Minute {
		t.Fatalf("duration=%s", got)
	}
	if ExtremePlan(0).Phases[0].Duration != 15*time.Minute {
		t.Fatal("default extreme duration")
	}
}

func TestCustomPlanArbitraryComponents(t *testing.T) {
	plan, err := CustomPlan(CustomSpec{
		DisplayName:         "CPU and memory",
		RequiredPowerSource: "battery",
		CPU:                 true,
		Memory:              true,
		GPUProfile:          "high",
		MemoryMB:            8192,
		WorkloadDuration:    3 * time.Minute,
		BaselineDuration:    time.Minute,
		CooldownDuration:    2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredPowerSource != "Battery Power" {
		t.Fatalf("power source=%q", plan.RequiredPowerSource)
	}
	if len(plan.Phases) != 3 {
		t.Fatalf("phases=%d", len(plan.Phases))
	}
	workload := plan.Phases[1]
	if workload.Kind != "combined" || len(workload.Components) != 2 || workload.MemoryMB != 8192 {
		t.Fatalf("workload=%+v", workload)
	}
	if err := validatePhase(workload); err != nil {
		t.Fatal(err)
	}
}

func TestCustomPlanRejectsNoWorkload(t *testing.T) {
	_, err := CustomPlan(CustomSpec{WorkloadDuration: time.Minute})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

type fakeMonitor struct {
	mu       sync.Mutex
	sample   *model.PowerSample
	session  model.Session
	dir      string
	phase    string
	runs     []model.TestRun
	writeErr error
	flushes  int
	flushErr error
}

func (f *fakeMonitor) LastSample() *model.PowerSample {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sample == nil {
		return nil
	}
	copyValue := *f.sample
	return &copyValue
}
func (f *fakeMonitor) Session() model.Session { return f.session }
func (f *fakeMonitor) SessionDir() string     { return f.dir }
func (f *fakeMonitor) SetPhase(value string) {
	f.mu.Lock()
	f.phase = value
	f.mu.Unlock()
}
func (f *fakeMonitor) FlushPending() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return f.flushErr
}
func (f *fakeMonitor) WriteTestRun(value model.TestRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.runs = append(f.runs, value)
	return nil
}

func testControllerDependencies(workload func(context.Context, string, Phase, Options) error) controllerDependencies {
	return controllerDependencies{
		buildNative: func(context.Context, string, string) error { return nil },
		acquireLock: func(string) (func(), error) { return func() {}, nil },
		runWorkload: func(
			ctx context.Context,
			dir string,
			phase Phase,
			opts Options,
			_ priorityReporter,
		) error {
			return workload(ctx, dir, phase, opts)
		},
		startSleepLock: func(context.Context) (func(), error) { return func() {}, nil },
		now:            time.Now,
	}
}

func TestControllerPersistsObservedBenchmarkPriority(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	controller := newController(
		monitor,
		controllerDependencies{
			buildNative: func(context.Context, string, string) error { return nil },
			acquireLock: func(string) (func(), error) { return func() {}, nil },
			runWorkload: func(
				_ context.Context,
				_ string,
				_ Phase,
				_ Options,
				report priorityReporter,
			) error {
				report(model.BenchmarkPriorityObservation{
					CapturedAt:            time.Now(),
					Supported:             true,
					RequestedBackendNice:  -5,
					ObservedBackendNice:   -5,
					RequestedWorkloadNice: 0,
					Workloads: []model.ProcessPriorityObservation{{
						PID:   123,
						Label: "cpu_stress[1]",
						Nice:  0,
					}},
				})
				return nil
			},
			startSleepLock: func(context.Context) (func(), error) { return func() {}, nil },
			now:            time.Now,
		},
	)
	if err := controller.Run(
		context.Background(),
		Plan{Name: "test", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}},
		Options{ProcessNice: -5},
	); err != nil {
		t.Fatal(err)
	}
	if len(monitor.runs) != 2 {
		t.Fatalf("runs=%+v", monitor.runs)
	}
	priority := monitor.runs[1].Priority
	if priority == nil || priority.ObservedBackendNice != -5 || len(priority.Workloads) != 1 || priority.Workloads[0].Nice != 0 {
		t.Fatalf("priority=%+v", priority)
	}
	progress := controller.Progress()
	if progress.Priority == nil || progress.Priority.ObservedBackendNice != -5 || progress.Priority.Workloads[0].Nice != 0 {
		t.Fatalf("progress priority=%+v", progress.Priority)
	}
}

func TestControllerReportsFailedInsteadOfComplete(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	expected := errors.New("workload failed")
	controller := newController(
		monitor,
		testControllerDependencies(func(context.Context, string, Phase, Options) error {
			return expected
		}),
	)
	err := controller.Run(
		context.Background(),
		Plan{Name: "test", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}},
		Options{},
	)
	if !errors.Is(err, expected) {
		t.Fatalf("err=%v", err)
	}
	progress := controller.Progress()
	if progress.Running || progress.Status != "failed" || progress.Error == "" {
		t.Fatalf("progress=%+v", progress)
	}
	if len(monitor.runs) != 2 || monitor.runs[1].Status != "failed" {
		t.Fatalf("runs=%+v", monitor.runs)
	}
}

func TestControllerPropagatesTestRunWriteError(t *testing.T) {
	monitor := &fakeMonitor{
		sample:   &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session:  model.Session{ID: "session"},
		dir:      t.TempDir(),
		writeErr: errors.New("disk full"),
	}
	controller := newController(
		monitor,
		testControllerDependencies(func(context.Context, string, Phase, Options) error {
			t.Fatal("workload must not start when run metadata cannot be persisted")
			return nil
		}),
	)
	err := controller.Run(
		context.Background(),
		Plan{Name: "test", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}},
		Options{},
	)
	if err == nil || !strings.Contains(err.Error(), "record benchmark phase start") {
		t.Fatalf("err=%v", err)
	}
}

func TestControllerCancellationPublishesStopped(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	started := make(chan struct{})
	controller := newController(
		monitor,
		testControllerDependencies(func(ctx context.Context, _ string, _ Phase, _ Options) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- controller.Run(
			ctx,
			Plan{Name: "test", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Minute}}},
			Options{},
		)
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	progress := controller.Progress()
	if progress.Running || progress.Status != "stopped" {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestControllerRejectsConcurrentRun(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	started := make(chan struct{})
	release := make(chan struct{})
	controller := newController(monitor, testControllerDependencies(func(context.Context, string, Phase, Options) error {
		close(started)
		<-release
		return nil
	}))
	first := make(chan error, 1)
	go func() {
		first <- controller.Run(context.Background(), Plan{Name: "one", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}}, Options{})
	}()
	<-started
	if err := controller.Run(context.Background(), Plan{Name: "two", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}}, Options{}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err=%v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestPreparationFailurePublishesFailedProgress(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	deps := testControllerDependencies(func(context.Context, string, Phase, Options) error { return nil })
	deps.buildNative = func(context.Context, string, string) error { return errors.New("compiler missing") }
	controller := newController(monitor, deps)
	err := controller.Run(context.Background(), Plan{Name: "test", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}}, Options{})
	if err == nil {
		t.Fatal("expected preparation failure")
	}
	progress := controller.Progress()
	if progress.Status != "failed" || progress.Running || !strings.Contains(progress.Error, "compiler missing") {
		t.Fatalf("progress=%+v", progress)
	}
}

func TestWorkloadCommandsAreRecordedAndRunIDIsUniqueLogNamespace(t *testing.T) {
	phase := Phase{Name: "CPU", Kind: "cpu", Duration: 5 * time.Second, RunID: "run-1"}
	commands, err := workloadCommands(phase, Options{BinDir: "/tmp/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0][0] != "/tmp/bin/cpu_stress" || commands[0][1] != "5" {
		t.Fatalf("commands=%v", commands)
	}
}

func TestControllerFlushesSamplesAroundEveryPhase(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	controller := newController(
		monitor,
		testControllerDependencies(func(context.Context, string, Phase, Options) error {
			return nil
		}),
	)
	plan := Plan{Name: "test", Phases: []Phase{
		{Name: "one", Kind: "cpu", Duration: time.Second},
		{Name: "two", Kind: "gpu", Duration: time.Second},
	}}
	if err := controller.Run(context.Background(), plan, Options{}); err != nil {
		t.Fatal(err)
	}
	monitor.mu.Lock()
	flushes := monitor.flushes
	phase := monitor.phase
	monitor.mu.Unlock()
	if flushes != 4 {
		t.Fatalf("flushes=%d want=4", flushes)
	}
	if phase != "" {
		t.Fatalf("phase=%q want empty", phase)
	}
}

func TestControllerDoesNotTransitionPhaseWhenBoundaryFlushFails(t *testing.T) {
	monitor := &fakeMonitor{
		sample:   &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session:  model.Session{ID: "session"},
		dir:      t.TempDir(),
		flushErr: errors.New("disk full"),
	}
	controller := newController(
		monitor,
		testControllerDependencies(func(context.Context, string, Phase, Options) error {
			t.Fatal("workload started after boundary flush failed")
			return nil
		}),
	)
	err := controller.Run(
		context.Background(),
		Plan{Name: "test", Phases: []Phase{{Name: "phase", Kind: "cpu", Duration: time.Second}}},
		Options{},
	)
	if err == nil || !strings.Contains(err.Error(), "flush samples before") {
		t.Fatalf("err=%v", err)
	}
}

func TestPhaseCompletionGraceScalesAndStaysBounded(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     time.Duration
	}{
		{name: "short", duration: time.Second, want: 30 * time.Second},
		{name: "extreme soak", duration: 10 * time.Minute, want: time.Minute},
		{name: "bounded", duration: 24 * time.Hour, want: 2 * time.Minute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := phaseCompletionGrace(test.duration); got != test.want {
				t.Fatalf("phaseCompletionGrace(%s)=%s want=%s", test.duration, got, test.want)
			}
		})
	}
}

func TestRunPhaseAcceptsSuccessfulExitAfterDeadline(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	deps := testControllerDependencies(func(ctx context.Context, _ string, _ Phase, _ Options) error {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		return nil
	})
	deps.phaseGrace = func(time.Duration) time.Duration { return 5 * time.Millisecond }
	controller := newController(monitor, deps)
	phase := Phase{Name: "phase", Kind: "cpu", Duration: 5 * time.Millisecond}

	if _, err := controller.runPhase(
		context.Background(),
		Plan{Name: "test", Phases: []Phase{phase}},
		0,
		phase,
		Options{},
	); err != nil {
		t.Fatalf("successful workload exit was reported as stopped: %v", err)
	}
}

func TestRunPhasePreservesParentCancellationAfterSuccessfulCleanup(t *testing.T) {
	monitor := &fakeMonitor{
		sample:  &model.PowerSample{Battery: model.BatterySample{PowerSource: "Battery Power"}},
		session: model.Session{ID: "session"},
		dir:     t.TempDir(),
	}
	started := make(chan struct{})
	deps := testControllerDependencies(func(ctx context.Context, _ string, _ Phase, _ Options) error {
		close(started)
		<-ctx.Done()
		return nil
	})
	controller := newController(monitor, deps)
	phase := Phase{Name: "phase", Kind: "cpu", Duration: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := controller.runPhase(
			ctx,
			Plan{Name: "test", Phases: []Phase{phase}},
			0,
			phase,
			Options{},
		)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation was lost: %v", err)
	}
}

func TestRunCommandsPreservesDeadlineForGracefulExit(t *testing.T) {
	const helperEnvironment = "MACPOWERLAB_GRACEFUL_WORKLOAD_HELPER"
	if os.Getenv(helperEnvironment) == "1" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals)
		<-signals
		os.Exit(0)
	}

	t.Setenv(helperEnvironment, "1")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := runCommands(
		ctx,
		t.TempDir(),
		"run",
		"phase",
		[][]string{{os.Args[0], "-test.run=^TestRunCommandsPreservesDeadlineForGracefulExit$"}},
		0,
		nil,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline-driven graceful exit was accepted: %v", err)
	}
}

func TestFinalizeWorkloadResultDistinguishesDeadlineOrder(t *testing.T) {
	if err := finalizeWorkloadResult(context.DeadlineExceeded, false, nil); err != nil {
		t.Fatalf("successful exit before deadline inherited cleanup deadline: %v", err)
	}

	syncErr := errors.New("sync failed")
	if err := finalizeWorkloadResult(context.DeadlineExceeded, false, syncErr); !errors.Is(err, syncErr) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("post-exit output failure returned %v", err)
	}

	if err := finalizeWorkloadResult(context.DeadlineExceeded, true, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline-driven graceful exit was accepted: %v", err)
	}

	exitErr := errors.New("exit status 1")
	if err := finalizeWorkloadResult(nil, false, exitErr); !errors.Is(err, exitErr) {
		t.Fatalf("child failure was lost: %v", err)
	}
	if err := finalizeWorkloadResult(context.DeadlineExceeded, true, exitErr); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, exitErr) {
		t.Fatalf("deadline or child failure was lost: %v", err)
	}
}
