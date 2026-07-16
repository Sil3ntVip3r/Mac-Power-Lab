// Package server exposes a token-authenticated loopback API for SwiftUI.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/benchmark"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/collector"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/priority"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/report"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/store"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

const (
	maxRequestBody  = 1 << 20
	maxEngineErrors = 100
)

type monitor interface {
	benchmark.Monitor
	Start(context.Context, map[string]string) error
	Stop(context.Context) error
	FlushPending() error
	Snapshot() (store.SessionSnapshot, error)
	CadenceDiagnostics() model.CadenceDiagnostics
}

type benchmarkRunner interface {
	Run(context.Context, benchmark.Plan, benchmark.Options) error
	Progress() model.BenchmarkProgress
}

type engineDependencies struct {
	newMonitor   func(config.Config) (monitor, error)
	newBenchmark func(benchmark.Monitor) benchmarkRunner
	ensureSudo   func(context.Context, bool) error
	setPriority  func(context.Context, int) error
	saveSettings func(string, model.RuntimeSettings) error
}

func defaultDependencies() engineDependencies {
	return engineDependencies{
		newMonitor: func(cfg config.Config) (monitor, error) {
			return collector.NewManager(cfg)
		},
		newBenchmark: func(value benchmark.Monitor) benchmarkRunner {
			return benchmark.New(value)
		},
		ensureSudo:   execx.EnsureSudo,
		setPriority:  priority.Set,
		saveSettings: config.SaveRuntimeSettings,
	}
}

// ErrBenchmarkRunning prevents an implicit benchmark cancellation during a
// settings-driven monitor restart.
var ErrBenchmarkRunning = errors.New("cannot apply runtime settings while a benchmark is running")

// ErrHistoricalLoggingDisabled prevents an empty or misleading historical
// report from being generated for a live-only session.
var ErrHistoricalLoggingDisabled = errors.New("durable logging is disabled; enable logging before generating a historical report")

type benchmarkRun struct {
	id     uint64
	cancel context.CancelFunc
	done   chan struct{}
}

// Engine owns monitor and benchmark lifecycles for the local API.
//
// lifecycle serializes rare start/stop transitions. mu protects published
// pointers and status data. Slow monitor or benchmark operations never execute
// while mu is held.
type Engine struct {
	cfg  config.Config
	deps engineDependencies

	lifecycle sync.Mutex
	reportMu  sync.Mutex
	mu        sync.RWMutex
	rootCtx   context.Context

	manager       monitor
	bench         benchmarkRunner
	monitorCancel context.CancelFunc
	run           *benchmarkRun
	nextRunID     uint64
	lastSession   string
	errors        []string
}

func NewEngine(cfg config.Config) *Engine {
	return newEngine(cfg, defaultDependencies())
}

func newEngine(cfg config.Config, deps engineDependencies) *Engine {
	return &Engine{cfg: cfg, deps: deps, rootCtx: context.Background()}
}

func (e *Engine) bindContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	e.mu.RLock()
	running := e.manager != nil
	e.mu.RUnlock()
	if !running {
		e.rootCtx = ctx
	}
}

func (e *Engine) StartMonitor(ctx context.Context) error {
	if ctx == nil {
		return errors.New("start-monitor context must not be nil")
	}
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	return e.startMonitorLocked(ctx)
}

func (e *Engine) startMonitorLocked(startupCtx context.Context) error {
	e.mu.RLock()
	alreadyRunning := e.manager != nil
	cfg := e.cfg
	e.mu.RUnlock()
	if alreadyRunning {
		return nil
	}
	manager, controller, monitorCancel, err := e.startMonitorComponentsLocked(
		startupCtx,
		cfg,
		map[string]string{"mode": "api"},
	)
	if err != nil {
		return err
	}
	e.publishMonitor(manager, controller, monitorCancel)
	return nil
}

func (e *Engine) startMonitorComponentsLocked(
	startupCtx context.Context,
	cfg config.Config,
	metadata map[string]string,
) (monitor, benchmarkRunner, context.CancelFunc, error) {
	if startupCtx == nil {
		return nil, nil, nil, errors.New("start-monitor context must not be nil")
	}
	if err := startupCtx.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("start-monitor context is done: %w", err)
	}
	if runtime.GOOS == "darwin" {
		sudoCtx, cancel := context.WithTimeout(startupCtx, 10*time.Second)
		err := e.deps.ensureSudo(sudoCtx, false)
		cancel()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	priorityCtx, priorityCancel := context.WithTimeout(startupCtx, 15*time.Second)
	err := e.deps.setPriority(priorityCtx, cfg.Runtime.ProcessNice)
	priorityCancel()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("apply process nice %d: %w", cfg.Runtime.ProcessNice, err)
	}
	manager, err := e.deps.newMonitor(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create monitor: %w", err)
	}

	root := e.rootCtx
	if root == nil {
		root = startupCtx
	}
	if err := root.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("engine context is done: %w", err)
	}
	monitorCtx, monitorCancel := context.WithCancel(root)
	if err := manager.Start(monitorCtx, metadata); err != nil {
		monitorCancel()
		return nil, nil, nil, fmt.Errorf("start monitor: %w", err)
	}
	controller := e.deps.newBenchmark(manager)
	return manager, controller, monitorCancel, nil
}

func (e *Engine) publishMonitor(
	manager monitor,
	controller benchmarkRunner,
	monitorCancel context.CancelFunc,
) {
	e.mu.Lock()
	e.manager = manager
	e.bench = controller
	e.monitorCancel = monitorCancel
	e.lastSession = manager.SessionDir()
	e.mu.Unlock()
}

func (e *Engine) StopMonitor(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stop-monitor context must not be nil")
	}
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()

	if err := e.stopBenchmarkLocked(ctx); err != nil {
		return err
	}
	return e.stopMonitorOnlyLocked(ctx)
}

func (e *Engine) stopMonitorOnlyLocked(ctx context.Context) error {
	e.mu.RLock()
	manager := e.manager
	cancel := e.monitorCancel
	e.mu.RUnlock()
	if manager == nil {
		return nil
	}
	if err := manager.FlushPending(); err != nil {
		return fmt.Errorf("flush pending monitor sample: %w", err)
	}
	if cancel != nil {
		cancel()
	}
	if err := manager.Stop(ctx); err != nil {
		// Keep the manager published when shutdown did not complete. Reporting it
		// as stopped would allow a second monitor to be launched over a live one.
		return err
	}

	e.mu.Lock()
	if e.manager == manager {
		e.lastSession = manager.SessionDir()
		e.manager = nil
		e.bench = nil
		e.monitorCancel = nil
	}
	e.mu.Unlock()
	return nil
}

// RuntimeSettings returns the current effective settings, including startup
// CLI overrides that were applied after persisted settings were loaded.
func (e *Engine) RuntimeSettings() model.RuntimeSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg.Runtime
}

// RuntimeProfiles returns the complete stable preset catalog.
func (e *Engine) RuntimeProfiles() []config.ProfileDefinition {
	return config.Profiles()
}

// ApplyRuntimeSettings persists settings atomically and, when monitoring is
// active, moves collection to a fresh session. A benchmark is never cancelled
// implicitly. The new document is published before the restart so a persistence
// failure cannot leave an untracked replacement monitor running. Any failed
// restart restores the previous document before attempting a rollback session.
func (e *Engine) ApplyRuntimeSettings(
	ctx context.Context,
	settings model.RuntimeSettings,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("settings context must not be nil")
	}
	if err := config.ValidateRuntimeSettings(settings); err != nil {
		return false, err
	}
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()

	e.mu.RLock()
	oldCfg := e.cfg
	manager := e.manager
	run := e.run
	e.mu.RUnlock()
	if run != nil {
		return false, ErrBenchmarkRunning
	}
	if config.RuntimeSettingsEqual(oldCfg.Runtime, settings) {
		if err := e.deps.saveSettings(oldCfg.DataDir, settings); err != nil {
			return false, fmt.Errorf("persist runtime settings: %w", err)
		}
		return false, nil
	}

	newCfg := oldCfg
	newCfg.Runtime = settings
	if manager == nil {
		if runtime.GOOS == "darwin" && settings.ProcessNice < oldCfg.Runtime.ProcessNice {
			sudoCtx, sudoCancel := context.WithTimeout(ctx, 10*time.Second)
			sudoErr := e.deps.ensureSudo(sudoCtx, false)
			sudoCancel()
			if sudoErr != nil {
				return false, fmt.Errorf("refresh sudo credentials for process priority: %w", sudoErr)
			}
		}
		priorityCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		priorityErr := e.deps.setPriority(priorityCtx, settings.ProcessNice)
		cancel()
		if priorityErr != nil {
			return false, fmt.Errorf("apply process nice %d: %w", settings.ProcessNice, priorityErr)
		}
		if err := e.deps.saveSettings(oldCfg.DataDir, settings); err != nil {
			restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 15*time.Second)
			restoreErr := e.deps.setPriority(restoreCtx, oldCfg.Runtime.ProcessNice)
			restoreCancel()
			persistRestoreErr := e.deps.saveSettings(oldCfg.DataDir, oldCfg.Runtime)
			return false, errors.Join(
				fmt.Errorf("persist runtime settings: %w", err),
				wrapOptional("restore prior process nice", restoreErr),
				wrapOptional("restore prior persisted runtime settings", persistRestoreErr),
			)
		}
		e.mu.Lock()
		e.cfg = newCfg
		e.mu.Unlock()
		return false, nil
	}

	oldSession := manager.Session()

	// Persist before stopping the active monitor. This makes persistence failure
	// side-effect free and removes the dangerous state where an unpersisted
	// replacement monitor must be stopped before rollback can begin.
	if err := e.deps.saveSettings(oldCfg.DataDir, settings); err != nil {
		return false, fmt.Errorf("persist runtime settings before restart: %w", err)
	}

	if err := e.stopMonitorOnlyLocked(ctx); err != nil {
		persistRestoreErr := e.deps.saveSettings(oldCfg.DataDir, oldCfg.Runtime)
		return false, errors.Join(
			fmt.Errorf("stop monitor for settings restart: %w", err),
			wrapOptional("restore prior persisted runtime settings", persistRestoreErr),
		)
	}

	metadata := map[string]string{
		"mode":                "api",
		"restart_reason":      "runtime_settings",
		"previous_session_id": oldSession.ID,
	}
	newManager, newBenchmark, newCancel, err := e.startMonitorComponentsLocked(
		ctx,
		newCfg,
		metadata,
	)
	if err != nil {
		persistRestoreErr := e.deps.saveSettings(oldCfg.DataDir, oldCfg.Runtime)
		return false, e.restorePreviousMonitorLocked(
			oldCfg,
			oldSession.ID,
			errors.Join(
				fmt.Errorf("start monitor with new runtime settings: %w", err),
				wrapOptional("restore prior persisted runtime settings", persistRestoreErr),
			),
		)
	}

	e.mu.Lock()
	e.cfg = newCfg
	e.mu.Unlock()
	e.publishMonitor(newManager, newBenchmark, newCancel)
	return true, nil
}

func (e *Engine) restorePreviousMonitorLocked(
	oldCfg config.Config,
	previousSessionID string,
	cause error,
) error {
	restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	manager, controller, monitorCancel, restoreErr := e.startMonitorComponentsLocked(
		restoreCtx,
		oldCfg,
		map[string]string{
			"mode":                "api",
			"restart_reason":      "runtime_settings_rollback",
			"previous_session_id": previousSessionID,
		},
	)
	if restoreErr == nil {
		e.publishMonitor(manager, controller, monitorCancel)
	}
	return errors.Join(cause, wrapOptional("restore previous monitor configuration", restoreErr))
}

func wrapOptional(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func (e *Engine) StartBenchmark(ctx context.Context, name string, duration time.Duration) error {
	plan, err := benchmark.PlanByID(name, duration)
	if err != nil {
		return err
	}
	return e.startPlan(ctx, plan)
}

// StartCustomBenchmark validates and starts a user-defined benchmark plan.
func (e *Engine) StartCustomBenchmark(ctx context.Context, spec benchmark.CustomSpec) error {
	plan, err := benchmark.CustomPlan(spec)
	if err != nil {
		return err
	}
	return e.startPlan(ctx, plan)
}

func (e *Engine) startPlan(ctx context.Context, plan benchmark.Plan) error {
	if ctx == nil {
		return errors.New("start-benchmark context must not be nil")
	}
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()

	if err := e.startMonitorLocked(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	if e.run != nil {
		e.mu.Unlock()
		return errors.New("benchmark already running")
	}
	controller := e.bench
	cfg := e.cfg
	root := e.rootCtx
	if root == nil {
		root = ctx
	}
	if err := root.Err(); err != nil {
		e.mu.Unlock()
		return fmt.Errorf("engine context is done: %w", err)
	}
	benchmarkCtx, cancel := context.WithCancel(root)
	e.nextRunID++
	run := &benchmarkRun{id: e.nextRunID, cancel: cancel, done: make(chan struct{})}
	e.run = run
	e.mu.Unlock()

	go func() {
		err := controller.Run(
			benchmarkCtx,
			plan,
			benchmark.Options{
				NativeDir:   cfg.NativeDir,
				BinDir:      cfg.NativeBinDir,
				GPUProfile:  "high",
				ProcessNice: cfg.Runtime.ProcessNice,
			},
		)
		close(run.done)
		e.mu.Lock()
		if e.run == run {
			e.run = nil
		}
		if err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			e.appendErrorLocked(err.Error())
		}
		e.mu.Unlock()
	}()
	return nil
}

func (e *Engine) StopBenchmark(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stop-benchmark context must not be nil")
	}
	e.lifecycle.Lock()
	defer e.lifecycle.Unlock()
	return e.stopBenchmarkLocked(ctx)
}

func (e *Engine) stopBenchmarkLocked(ctx context.Context) error {
	e.mu.RLock()
	run := e.run
	e.mu.RUnlock()
	if run == nil {
		return nil
	}
	run.cancel()
	select {
	case <-run.done:
		e.mu.Lock()
		if e.run == run {
			e.run = nil
		}
		e.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop benchmark: %w", ctx.Err())
	}
}

// Close stops the benchmark before stopping the monitor.
func (e *Engine) Close(ctx context.Context) error {
	return e.StopMonitor(ctx)
}

func (e *Engine) Status() model.Status {
	e.mu.RLock()
	manager := e.manager
	controller := e.bench
	errorsCopy := append([]string(nil), e.errors...)
	e.mu.RUnlock()

	status := model.Status{
		Schema:         version.StatusSchema,
		Version:        version.Version,
		MonitorRunning: manager != nil,
		Capabilities: map[string]bool{
			"darwin":       runtime.GOOS == "darwin",
			"powermetrics": fileExists("/usr/bin/powermetrics"),
			"sqlite3":      commandExists("sqlite3"),
		},
		Errors: errorsCopy,
	}
	if manager != nil {
		session := manager.Session()
		status.Session = &session
		status.LastSample = manager.LastSample()
		cadence := manager.CadenceDiagnostics()
		status.Cadence = &cadence
	}
	if controller != nil {
		status.Benchmark = controller.Progress()
	}
	return status
}

// GenerateReport captures a cumulative byte-boundary snapshot and writes a new
// immutable timestamped artifact. Reports are serialized so latest.json always
// points to the newest completed artifact.
func (e *Engine) GenerateReport(ctx context.Context) (report.Artifact, error) {
	if ctx == nil {
		return report.Artifact{}, errors.New("report context must not be nil")
	}
	e.reportMu.Lock()
	defer e.reportMu.Unlock()
	if err := ctx.Err(); err != nil {
		return report.Artifact{}, err
	}

	e.mu.RLock()
	manager := e.manager
	directory := e.lastSession
	dataDir := e.cfg.DataDir
	loggingEnabled := e.cfg.Runtime.LoggingEnabled
	e.mu.RUnlock()

	if manager != nil && !loggingEnabled {
		return report.Artifact{}, ErrHistoricalLoggingDisabled
	}

	var (
		snapshot store.SessionSnapshot
		err      error
	)
	if manager != nil {
		snapshot, err = manager.Snapshot()
	} else {
		if directory == "" {
			directory, err = store.LatestSessionDir(dataDir)
		}
		if err == nil {
			snapshot, err = store.SnapshotDir(directory)
		}
	}
	if err != nil {
		return report.Artifact{}, fmt.Errorf("capture report snapshot: %w", err)
	}
	return report.GenerateSnapshot(snapshot)
}

// LatestReport returns the newest completed artifact for the current or most
// recent session.
func (e *Engine) LatestReport() (report.Artifact, error) {
	directory := e.LastSessionDir()
	if directory == "" {
		return report.Artifact{}, os.ErrNotExist
	}
	return report.Latest(directory)
}

func (e *Engine) LastSessionDir() string {
	e.mu.RLock()
	manager := e.manager
	lastSession := e.lastSession
	dataDir := e.cfg.DataDir
	e.mu.RUnlock()
	if manager != nil {
		return manager.SessionDir()
	}
	if lastSession != "" {
		return lastSession
	}
	directory, _ := store.LatestSessionDir(dataDir)
	return directory
}

func (e *Engine) appendError(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	e.appendErrorLocked(err.Error())
	e.mu.Unlock()
}

func (e *Engine) appendErrorLocked(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	e.errors = append(e.errors, message)
	if len(e.errors) > maxEngineErrors {
		e.errors = append([]string(nil), e.errors[len(e.errors)-maxEngineErrors:]...)
	}
}

// BenchmarkStartRequest is the validated API payload for preset or custom plans.
type BenchmarkStartRequest struct {
	Name            string                  `json:"name"`
	DurationSeconds float64                 `json:"duration_seconds,omitempty"`
	Custom          *CustomBenchmarkRequest `json:"custom,omitempty"`
}

// CustomBenchmarkRequest is the JSON representation of benchmark.CustomSpec.
type CustomBenchmarkRequest struct {
	DisplayName         string  `json:"display_name"`
	RequiredPowerSource string  `json:"required_power_source"`
	CPU                 bool    `json:"cpu"`
	GPU                 bool    `json:"gpu"`
	Memory              bool    `json:"memory"`
	GPUProfile          string  `json:"gpu_profile"`
	MemoryMB            int     `json:"memory_mb"`
	WorkloadSeconds     float64 `json:"workload_seconds"`
	BaselineSeconds     float64 `json:"baseline_seconds"`
	CooldownSeconds     float64 `json:"cooldown_seconds"`
}

func (r CustomBenchmarkRequest) spec() (benchmark.CustomSpec, error) {
	for name, value := range map[string]float64{
		"workload_seconds": r.WorkloadSeconds,
		"baseline_seconds": r.BaselineSeconds,
		"cooldown_seconds": r.CooldownSeconds,
	} {
		if value < 0 || value > 86400 {
			return benchmark.CustomSpec{}, fmt.Errorf("%s must be between 0 and 86400", name)
		}
	}
	if r.WorkloadSeconds < 1 {
		return benchmark.CustomSpec{}, errors.New("workload_seconds must be at least 1")
	}
	return benchmark.CustomSpec{
		DisplayName:         r.DisplayName,
		RequiredPowerSource: r.RequiredPowerSource,
		CPU:                 r.CPU,
		GPU:                 r.GPU,
		Memory:              r.Memory,
		GPUProfile:          r.GPUProfile,
		MemoryMB:            r.MemoryMB,
		WorkloadDuration:    time.Duration(r.WorkloadSeconds * float64(time.Second)),
		BaselineDuration:    time.Duration(r.BaselineSeconds * float64(time.Second)),
		CooldownDuration:    time.Duration(r.CooldownSeconds * float64(time.Second)),
	}, nil
}

// Serve starts a local-only HTTP server and writes a private auth token file.
func Serve(ctx context.Context, addr, tokenFile string, engine *Engine, autoMonitor bool) (retErr error) {
	if ctx == nil {
		return errors.New("server context must not be nil")
	}
	if engine == nil {
		return errors.New("engine is required")
	}
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("parse API address: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return errors.New("refusing non-loopback API address")
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	if tokenFile == "" {
		tokenFile = filepath.Join(engine.cfg.DataDir, "api.token")
	}
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}
	if err := atomicPrivateFile(tokenFile, []byte(token+"\n")); err != nil {
		return fmt.Errorf("write API token: %w", err)
	}
	addressFile := tokenFile + ".address"
	defer func() {
		_ = os.Remove(addressFile)
		_ = os.Remove(tokenFile)
	}()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := requireLoopbackListener(listener.Addr()); err != nil {
		return err
	}
	actual := listener.Addr().String()
	if err := atomicPrivateFile(addressFile, []byte(actual+"\n")); err != nil {
		return fmt.Errorf("write API address: %w", err)
	}

	serveCtx, serveCancel := context.WithCancel(ctx)
	defer serveCancel()
	engine.bindContext(serveCtx)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		retErr = errors.Join(retErr, engine.Close(closeCtx))
	}()
	mux := http.NewServeMux()
	auth := bearerAuth(token)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version.Version})
	})
	mux.HandleFunc("GET /status", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, engine.Status())
	}))
	mux.HandleFunc("GET /settings", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, engine.RuntimeSettings())
	}))
	mux.HandleFunc("GET /settings/profiles", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, engine.RuntimeProfiles())
	}))
	mux.HandleFunc("PUT /settings", auth(func(w http.ResponseWriter, r *http.Request) {
		var settings model.RuntimeSettings
		if err := decodeJSON(w, r, &settings); err != nil {
			return
		}
		if err := config.ValidateRuntimeSettings(settings); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := engine.ApplyRuntimeSettings(r.Context(), settings); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrBenchmarkRunning) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, engine.RuntimeSettings())
	}))
	mux.HandleFunc("GET /benchmarks", auth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, benchmark.Catalog())
	}))
	mux.HandleFunc("GET /apps", auth(func(w http.ResponseWriter, _ *http.Request) {
		status := engine.Status()
		if status.LastSample == nil {
			writeJSON(w, http.StatusOK, []model.AppPower{})
			return
		}
		writeJSON(w, http.StatusOK, status.LastSample.Attribution.Apps)
	}))
	mux.HandleFunc("POST /monitor/start", auth(func(w http.ResponseWriter, r *http.Request) {
		if err := engine.StartMonitor(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, engine.Status())
	}))
	mux.HandleFunc("POST /monitor/stop", auth(func(w http.ResponseWriter, r *http.Request) {
		stopCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := engine.StopMonitor(stopCtx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, engine.Status())
	}))
	mux.HandleFunc("POST /benchmark/start", auth(func(w http.ResponseWriter, r *http.Request) {
		var request BenchmarkStartRequest
		if err := decodeJSON(w, r, &request); err != nil {
			return
		}
		if request.Name == "" {
			http.Error(w, "benchmark name required", http.StatusBadRequest)
			return
		}
		if request.DurationSeconds < 0 || request.DurationSeconds > 86400 {
			http.Error(w, "duration_seconds must be between 0 and 86400", http.StatusBadRequest)
			return
		}

		var startErr error
		if request.Name == "custom" {
			if request.Custom == nil {
				http.Error(w, "custom benchmark configuration required", http.StatusBadRequest)
				return
			}
			spec, err := request.Custom.spec()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			startErr = engine.StartCustomBenchmark(r.Context(), spec)
		} else {
			if request.Custom != nil {
				http.Error(w, "custom configuration is only valid for the custom benchmark", http.StatusBadRequest)
				return
			}
			startErr = engine.StartBenchmark(
				r.Context(),
				request.Name,
				time.Duration(request.DurationSeconds*float64(time.Second)),
			)
		}
		if startErr != nil {
			http.Error(w, startErr.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, engine.Status())
	}))
	mux.HandleFunc("POST /benchmark/stop", auth(func(w http.ResponseWriter, r *http.Request) {
		stopCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := engine.StopBenchmark(stopCtx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, engine.Status())
	}))
	mux.HandleFunc("POST /report", auth(func(w http.ResponseWriter, r *http.Request) {
		artifact, err := engine.GenerateReport(r.Context())
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrHistoricalLoggingDisabled):
				status = http.StatusBadRequest
			case errors.Is(err, os.ErrNotExist):
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, artifact)
	}))
	mux.HandleFunc("GET /report/latest", auth(func(w http.ResponseWriter, _ *http.Request) {
		artifact, err := engine.LatestReport()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, artifact)
	}))

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      130 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	if autoMonitor {
		go func() {
			if err := engine.StartMonitor(serveCtx); err != nil && serveCtx.Err() == nil {
				engine.appendError(fmt.Errorf("auto-start monitor: %w", err))
			}
		}()
	}

	shutdownResult := make(chan error, 1)
	go func() {
		<-serveCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		shutdownResult <- server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("MacPowerLab API listening on http://%s\nToken file: %s\n", actual, tokenFile)
	serveErr := server.Serve(listener)
	serveCancel()
	shutdownErr := <-shutdownResult
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}

func requireLoopbackListener(address net.Addr) error {
	tcpAddress, ok := address.(*net.TCPAddr)
	if !ok || tcpAddress.IP == nil || !tcpAddress.IP.IsLoopback() {
		return fmt.Errorf("refusing non-loopback listener address %q", address.String())
	}
	return nil
}

func bearerAuth(token string) func(http.HandlerFunc) http.HandlerFunc {
	expected := []byte("Bearer " + token)
	return func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actual := []byte(r.Header.Get("Authorization"))
			if len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			handler(w, r)
		}
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "request must contain one JSON value", http.StatusBadRequest)
		return errors.New("multiple JSON values")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// The response may already be partially written; there is no safe status
		// change here. Close-on-error is handled by net/http.
		return
	}
}

func atomicPrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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

func newToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
