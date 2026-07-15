// Package benchmark implements validated, cancellable benchmark state machines.
package benchmark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/priority"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

// Phase is one bounded benchmark workload.
type Phase struct {
	Name, Kind, Profile string
	Duration            time.Duration
	MemoryMB            int
	Components          []string
	RunID               string
}

// Plan is a validated sequence of phases.
type Plan struct {
	Name, RequiredPowerSource string
	Phases                    []Phase
}

// Options controls native build locations and test customization.
type Options struct {
	NativeDir, BinDir string
	ExtremeDuration   time.Duration
	MemoryMB          int
	GPUProfile        string
	ProcessNice       int
}

// Monitor is the narrow benchmark-facing monitor contract. Depending on an
// interface keeps the controller unit-testable and avoids coupling workload
// execution to collector implementation details.
type Monitor interface {
	LastSample() *model.PowerSample
	Session() model.Session
	SessionDir() string
	SetPhase(string)
	FlushPending() error
	WriteTestRun(model.TestRun) error
}

// Controller runs one plan at a time and publishes progress.
type controllerDependencies struct {
	buildNative    func(context.Context, string, string) error
	acquireLock    func(string) (func(), error)
	runWorkload    func(context.Context, string, Phase, Options) error
	startSleepLock func(context.Context) (func(), error)
	now            func() time.Time
}

func defaultControllerDependencies() controllerDependencies {
	return controllerDependencies{
		buildNative: BuildNative,
		acquireLock: acquireLock,
		runWorkload: runWorkload,
		startSleepLock: func(ctx context.Context) (func(), error) {
			cmd, err := startCaffeinate(ctx)
			if err != nil {
				return nil, err
			}
			return func() { stopCaffeinate(cmd) }, nil
		},
		now: time.Now,
	}
}

type Controller struct {
	manager     Monitor
	deps        controllerDependencies
	runMu       sync.Mutex
	runSequence uint64
	mu          sync.RWMutex
	progress    model.BenchmarkProgress
	updates     chan model.BenchmarkProgress
}

func New(manager Monitor) *Controller {
	return newController(manager, defaultControllerDependencies())
}

func newController(manager Monitor, deps controllerDependencies) *Controller {
	return &Controller{
		manager: manager,
		deps:    deps,
		updates: make(chan model.BenchmarkProgress, 16),
	}
}
func (c *Controller) Updates() <-chan model.BenchmarkProgress { return c.updates }
func (c *Controller) Progress() model.BenchmarkProgress {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.progress
}
func (c *Controller) set(p model.BenchmarkProgress) {
	c.mu.Lock()
	c.progress = p
	c.mu.Unlock()
	select {
	case c.updates <- p:
	default:
	}
}

func BatteryPlan() Plan {
	return Plan{
		Name:                "battery",
		RequiredPowerSource: "Battery Power",
		Phases: []Phase{
			{Name: "Idle baseline", Kind: "idle", Duration: 2 * time.Minute},
			{Name: "CPU stress", Kind: "cpu", Duration: 2 * time.Minute},
			{Name: "GPU stress", Kind: "gpu", Profile: "high", Duration: 2 * time.Minute},
			{Name: "Memory bandwidth", Kind: "memory", Duration: 2 * time.Minute},
			{Name: "Extreme CPU + GPU + memory", Kind: "extreme", Profile: "extreme", Duration: 3 * time.Minute, Components: []string{"cpu", "gpu", "memory"}},
		},
	}
}

func ACPlan() Plan {
	plan := BatteryPlan()
	plan.Name = "ac"
	plan.RequiredPowerSource = "AC Power"
	return plan
}

func ExtremePlan(d time.Duration) Plan {
	if d <= 0 {
		d = 15 * time.Minute
	}
	return Plan{
		Name: "extreme",
		Phases: []Phase{{
			Name:       "Extreme soak",
			Kind:       "extreme",
			Profile:    "extreme",
			Duration:   d,
			Components: []string{"cpu", "gpu", "memory"},
		}},
	}
}

// Run validates the environment and executes one plan. The controller itself
// enforces single-run ownership so callers outside the HTTP engine cannot mix
// progress or workload processes by invoking Run concurrently.
func (c *Controller) Run(ctx context.Context, plan Plan, opts Options) (runErr error) {
	if ctx == nil {
		return errors.New("benchmark context must not be nil")
	}
	if c.manager == nil {
		return errors.New("benchmark requires a running monitor")
	}
	if strings.TrimSpace(plan.Name) == "" {
		return errors.New("benchmark plan name is required")
	}
	if len(plan.Phases) == 0 {
		return errors.New("benchmark plan has no phases")
	}
	for index, phase := range plan.Phases {
		if err := validatePhase(phase); err != nil {
			return fmt.Errorf("validate phase %d (%s): %w", index+1, phase.Name, err)
		}
	}
	if !c.runMu.TryLock() {
		return errors.New("benchmark controller already running")
	}
	defer c.runMu.Unlock()
	c.runSequence++
	planSequence := c.runSequence

	c.set(model.BenchmarkProgress{
		Running:    true,
		Plan:       plan.Name,
		PhaseCount: len(plan.Phases),
		Status:     "preparing",
	})
	defer func() {
		progress := model.BenchmarkProgress{
			Running: false,
			Plan:    plan.Name,
			Status:  "complete",
		}
		switch {
		case runErr == nil:
			progress.Percent = 100
		case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded):
			progress.Status = "stopped"
			progress.Error = runErr.Error()
		default:
			progress.Status = "failed"
			progress.Error = runErr.Error()
		}
		c.manager.SetPhase("")
		c.set(progress)
	}()

	sample, err := waitForSample(ctx, c.manager, 15*time.Second)
	if err != nil {
		return err
	}
	if plan.RequiredPowerSource != "" && sample.Battery.PowerSource != plan.RequiredPowerSource {
		return fmt.Errorf(
			"benchmark %s requires %s, current source is %s",
			plan.Name,
			plan.RequiredPowerSource,
			sample.Battery.PowerSource,
		)
	}

	sessionDir := c.manager.SessionDir()
	if sessionDir == "" {
		return errors.New("benchmark session directory is unavailable")
	}
	release, err := c.deps.acquireLock(filepath.Join(sessionDir, "..", "benchmark.lock"))
	if err != nil {
		return fmt.Errorf("acquire benchmark lock: %w", err)
	}
	defer release()

	if err := c.deps.buildNative(ctx, opts.NativeDir, opts.BinDir); err != nil {
		return fmt.Errorf("prepare native workloads: %w", err)
	}
	releaseSleepLock, err := c.deps.startSleepLock(ctx)
	if err != nil {
		return fmt.Errorf("start sleep lock: %w", err)
	}
	defer releaseSleepLock()

	for index, originalPhase := range plan.Phases {
		if err := c.manager.FlushPending(); err != nil {
			return fmt.Errorf("flush samples before benchmark phase %d: %w", index+1, err)
		}
		runID := fmt.Sprintf(
			"%s_%06d_%03d",
			c.deps.now().UTC().Format("20060102_150405.000000000"),
			planSequence,
			index+1,
		)
		phase := originalPhase
		phase.RunID = runID
		commands, commandErr := workloadCommands(phase, opts)
		if commandErr != nil {
			return commandErr
		}
		started := c.deps.now()
		testRun := model.TestRun{
			Schema:           version.TestRunSchema,
			ID:               runID,
			SessionID:        c.manager.Session().ID,
			Name:             phase.Name,
			Plan:             plan.Name,
			Phase:            phase.Name,
			Status:           "running",
			StartedAt:        started,
			RequestedSeconds: phase.Duration.Seconds(),
			Commands:         cloneCommands(commands),
			Metadata: map[string]string{
				"kind":       phase.Kind,
				"profile":    phase.Profile,
				"components": strings.Join(phase.Components, ","),
				"memory_mb":  strconv.Itoa(phase.MemoryMB),
			},
		}
		if err := c.manager.WriteTestRun(testRun); err != nil {
			return fmt.Errorf("record benchmark phase start: %w", err)
		}
		c.manager.SetPhase(phase.Name)
		phaseErr := c.runPhase(ctx, plan, index, phase, opts)
		if flushErr := c.manager.FlushPending(); flushErr != nil {
			phaseErr = errors.Join(
				phaseErr,
				fmt.Errorf("flush samples after benchmark phase %d: %w", index+1, flushErr),
			)
		}
		testRun.EndedAt = c.deps.now()
		testRun.ActualSeconds = testRun.EndedAt.Sub(started).Seconds()
		switch {
		case phaseErr == nil:
			testRun.Status = "complete"
		case errors.Is(phaseErr, context.Canceled), errors.Is(phaseErr, context.DeadlineExceeded):
			testRun.Status = "stopped"
			testRun.Error = phaseErr.Error()
		default:
			testRun.Status = "failed"
			testRun.Error = phaseErr.Error()
		}
		writeErr := c.manager.WriteTestRun(testRun)
		c.manager.SetPhase("")
		if writeErr != nil {
			phaseErr = errors.Join(phaseErr, fmt.Errorf("record benchmark phase result: %w", writeErr))
		}
		if phaseErr != nil {
			return phaseErr
		}
	}
	return nil
}

func waitForSample(ctx context.Context, monitor Monitor, timeout time.Duration) (*model.PowerSample, error) {
	if sample := monitor.LastSample(); sample != nil {
		return sample, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("monitor has not produced a power sample")
		case <-ticker.C:
			if sample := monitor.LastSample(); sample != nil {
				return sample, nil
			}
		}
	}
}

func (c *Controller) runPhase(
	ctx context.Context,
	plan Plan,
	index int,
	phase Phase,
	opts Options,
) error {
	phaseCtx, cancel := context.WithTimeout(ctx, phase.Duration+30*time.Second)
	defer cancel()
	started := c.deps.now()
	c.set(model.BenchmarkProgress{
		Running:       true,
		Plan:          plan.Name,
		Phase:         phase.Name,
		PhaseIndex:    index + 1,
		PhaseCount:    len(plan.Phases),
		PhaseStarted:  started,
		PhaseDuration: phase.Duration.Seconds(),
		Status:        "running",
	})

	done := make(chan error, 1)
	go func() {
		done <- c.deps.runWorkload(phaseCtx, c.manager.SessionDir(), phase, opts)
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-phaseCtx.Done():
			// Wait for workload cleanup. runCommands owns process Wait calls and
			// receives the same context, so this does not race with subprocess Wait.
			select {
			case err := <-done:
				if phaseCtx.Err() != nil {
					return errors.Join(phaseCtx.Err(), err)
				}
				return err
			case <-time.After(10 * time.Second):
				return fmt.Errorf("workload cleanup timed out: %w", phaseCtx.Err())
			}
		case now := <-ticker.C:
			elapsed := now.Sub(started).Seconds()
			remaining := mathMax(0, phase.Duration.Seconds()-elapsed)
			percent := mathMin(100, elapsed/phase.Duration.Seconds()*100)
			c.set(model.BenchmarkProgress{
				Running:       true,
				Plan:          plan.Name,
				Phase:         phase.Name,
				PhaseIndex:    index + 1,
				PhaseCount:    len(plan.Phases),
				PhaseStarted:  started,
				PhaseDuration: phase.Duration.Seconds(),
				Elapsed:       elapsed,
				Remaining:     remaining,
				Percent:       percent,
				Status:        "running",
			})
		}
	}
}

func validatePhase(p Phase) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("phase name is required")
	}
	if p.Duration < time.Second || p.Duration > 24*time.Hour {
		return fmt.Errorf("invalid phase duration: %s", p.Duration)
	}
	if p.MemoryMB < 0 || p.MemoryMB > 262144 {
		return fmt.Errorf("invalid phase memory allocation: %d MB", p.MemoryMB)
	}
	if p.Profile != "" && p.Profile != "normal" && p.Profile != "high" && p.Profile != "extreme" {
		return fmt.Errorf("unsupported GPU profile %q", p.Profile)
	}

	switch p.Kind {
	case "idle", "cpu", "gpu", "memory", "extreme", "combined":
	default:
		return fmt.Errorf("unsupported phase kind %q", p.Kind)
	}

	seen := make(map[string]bool, len(p.Components))
	for _, component := range p.Components {
		switch component {
		case "cpu", "gpu", "memory":
		default:
			return fmt.Errorf("unsupported workload component %q", component)
		}
		if seen[component] {
			return fmt.Errorf("duplicate workload component %q", component)
		}
		seen[component] = true
	}
	if p.Kind == "combined" && len(p.Components) < 2 {
		return errors.New("combined phase requires at least two components")
	}
	if p.Kind == "idle" && len(p.Components) > 0 {
		return errors.New("idle phase cannot define workload components")
	}
	return nil
}

func runWorkload(ctx context.Context, sessionDir string, phase Phase, opts Options) error {
	if phase.Kind == "idle" {
		timer := time.NewTimer(phase.Duration)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	commands, err := workloadCommands(phase, opts)
	if err != nil {
		return err
	}
	return runCommands(ctx, sessionDir, phase.RunID, phase.Name, commands, opts.ProcessNice)
}

func workloadCommands(phase Phase, opts Options) ([][]string, error) {
	if phase.Kind == "idle" {
		return nil, nil
	}
	bin := opts.BinDir
	if bin == "" {
		bin = "bin/native"
	}
	seconds := strconv.Itoa(maxInt(1, int(phase.Duration.Seconds())))
	memory := phase.MemoryMB
	if memory <= 0 {
		memory = opts.MemoryMB
	}
	profile := phase.Profile
	if profile == "" {
		profile = opts.GPUProfile
	}
	if profile == "" {
		profile = "high"
	}

	components := append([]string(nil), phase.Components...)
	if len(components) == 0 {
		switch phase.Kind {
		case "cpu", "gpu", "memory":
			components = []string{phase.Kind}
		case "extreme":
			components = []string{"cpu", "gpu", "memory"}
			if phase.Profile == "" {
				profile = "extreme"
			}
		case "combined":
			return nil, errors.New("combined workload has no components")
		default:
			return nil, fmt.Errorf("unsupported workload kind %q", phase.Kind)
		}
	}

	commands := make([][]string, 0, len(components))
	for _, component := range components {
		switch component {
		case "cpu":
			commands = append(commands, []string{filepath.Join(bin, "cpu_stress"), seconds})
		case "gpu":
			commands = append(commands, []string{filepath.Join(bin, "gpu_stress"), seconds, profile})
		case "memory":
			args := []string{filepath.Join(bin, "memory_stress"), seconds}
			if memory > 0 {
				args = append(args, strconv.Itoa(memory))
			}
			commands = append(commands, args)
		default:
			return nil, fmt.Errorf("unsupported workload component %q", component)
		}
	}
	if len(commands) == 0 {
		return nil, errors.New("workload produced no commands")
	}
	return commands, nil
}

func cloneCommands(commands [][]string) [][]string {
	if commands == nil {
		return nil
	}
	copyValue := make([][]string, len(commands))
	for index, command := range commands {
		copyValue[index] = append([]string(nil), command...)
	}
	return copyValue
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(value)
}

type workloadProcess struct {
	label string
	cmd   *exec.Cmd
	log   *os.File
	copy  sync.WaitGroup
	err   chan error
}

func runCommands(
	ctx context.Context,
	sessionDir, runID, name string,
	commands [][]string,
	processNice int,
) error {
	if len(commands) == 0 {
		return errors.New("no workload commands")
	}
	if strings.TrimSpace(runID) == "" {
		return errors.New("workload run ID is required")
	}
	logDir := filepath.Join(sessionDir, "workloads", stringsMap(runID))
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create workload log directory: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	processes := make([]*workloadProcess, 0, len(commands))
	cleanupStarted := func() error {
		cancel()
		var cleanupErr error
		for _, process := range processes {
			if stopErr := execx.StopGroup(process.cmd, 3*time.Second); stopErr != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("stop %s: %w", process.label, stopErr))
			}
			process.copy.Wait()
			if syncErr := process.log.Sync(); syncErr != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("sync %s log: %w", process.label, syncErr))
			}
			if closeErr := process.log.Close(); closeErr != nil {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close %s log: %w", process.label, closeErr))
			}
		}
		return cleanupErr
	}

	safeName := stringsMap(name)
	for index, args := range commands {
		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			return errors.Join(
				fmt.Errorf("workload command %d is empty", index+1),
				cleanupStarted(),
			)
		}
		logPath := filepath.Join(logDir, fmt.Sprintf("%s_%d.log", safeName, index+1))
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return errors.Join(
				fmt.Errorf("create workload log %s: %w", logPath, err),
				cleanupStarted(),
			)
		}
		cmd, stdout, stderr, err := priority.StartNormalized(
			runCtx,
			processNice,
			args[0],
			args[1:]...,
		)
		if err != nil {
			closeErr := logFile.Close()
			return errors.Join(
				fmt.Errorf("start workload %q: %w", args[0], err),
				closeErr,
				cleanupStarted(),
			)
		}
		process := &workloadProcess{
			label: fmt.Sprintf("%s[%d]", filepath.Base(args[0]), index+1),
			cmd:   cmd,
			log:   logFile,
			err:   make(chan error, 3),
		}
		writer := &lockedWriter{w: logFile}
		process.copy.Add(2)
		go func() {
			defer process.copy.Done()
			_, copyErr := io.Copy(writer, stdout)
			process.err <- copyErr
		}()
		go func() {
			defer process.copy.Done()
			_, copyErr := io.Copy(writer, stderr)
			process.err <- copyErr
		}()
		processes = append(processes, process)
	}

	type waitResult struct {
		label string
		err   error
	}
	waits := make(chan waitResult, len(processes))
	for _, process := range processes {
		go func(process *workloadProcess) {
			waits <- waitResult{label: process.label, err: process.cmd.Wait()}
		}(process)
	}

	var result error
	for range processes {
		waited := <-waits
		if waited.err != nil && runCtx.Err() == nil {
			result = errors.Join(result, fmt.Errorf("workload %s: %w", waited.label, waited.err))
			cancel()
		}
	}
	for _, process := range processes {
		process.copy.Wait()
		close(process.err)
		for copyErr := range process.err {
			if copyErr != nil && !errors.Is(copyErr, os.ErrClosed) {
				result = errors.Join(result, fmt.Errorf("copy %s output: %w", process.label, copyErr))
			}
		}
		if syncErr := process.log.Sync(); syncErr != nil {
			result = errors.Join(result, fmt.Errorf("sync %s log: %w", process.label, syncErr))
		}
		if closeErr := process.log.Close(); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close %s log: %w", process.label, closeErr))
		}
	}
	if ctx.Err() != nil {
		return errors.Join(ctx.Err(), result)
	}
	return result
}

// BuildNative compiles deterministic CPU/memory/Metal workloads on macOS.
func BuildNative(ctx context.Context, nativeDir, binDir string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("native benchmark workloads must be built on macOS")
	}
	if nativeDir == "" {
		nativeDir = "native"
	}
	if binDir == "" {
		binDir = "bin/native"
	}

	// Signed SwiftUI bundles ship prebuilt workloads. Recompiling into
	// Contents/Resources would mutate the signed bundle.
	prebuilt := true
	for _, name := range []string{"cpu_stress", "memory_stress", "gpu_stress"} {
		info, err := os.Stat(filepath.Join(binDir, name))
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			prebuilt = false
			break
		}
	}
	if prebuilt {
		return nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	jobs := [][]string{{"/usr/bin/clang", "-O3", "-pthread", filepath.Join(nativeDir, "cpu_stress.c"), "-lm", "-o", filepath.Join(binDir, "cpu_stress")}, {"/usr/bin/clang", "-O3", filepath.Join(nativeDir, "memory_stress.c"), "-o", filepath.Join(binDir, "memory_stress")}, {"/usr/bin/clang", "-O3", "-fobjc-arc", "-framework", "Foundation", "-framework", "Metal", filepath.Join(nativeDir, "gpu_stress.m"), "-o", filepath.Join(binDir, "gpu_stress")}}
	for _, j := range jobs {
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, err := execx.Run(c, 8<<20, j[0], j[1:]...)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
func startCaffeinate(ctx context.Context) (*exec.Cmd, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	if ctx == nil {
		return nil, errors.New("caffeinate context must not be nil")
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/caffeinate", "-dimsu")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start caffeinate sleep lock: %w", err)
	}
	return cmd, nil
}

func stopCaffeinate(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}
func stringsMap(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
