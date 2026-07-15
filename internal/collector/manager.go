package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/attribution"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	plistx "github.com/Sil3ntVip3r/Mac-Power-Lab/internal/plist"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/store"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

// Manager coordinates independent battery, powermetrics, attribution, and
// storage streams. Its public methods are safe for concurrent use.
type Manager struct {
	cfg config.Config

	lifecycle         sync.Mutex
	stateMu           sync.Mutex
	mu                sync.RWMutex
	session           model.Session
	latest            *model.PowerSample
	latestPM          PowermetricsSnapshot
	latestProcesses   []model.ProcessActivity
	latestProcessesAt time.Time
	processStatus     string
	phase             string
	seq               uint64
	store             *store.SessionStore
	attr              *attribution.Attributor
	cancel            context.CancelFunc
	terminated        bool
	done              chan struct{}
	samples           chan model.PowerSample
	errs              chan error
	tempHistory       []tempPoint
	previousSource    string
	previousState     string
}

type tempPoint struct {
	t time.Time
	v float64
}

const minimumSensorFreshness = 5 * time.Second

func sensorFresh(now, collectedAt time.Time, interval time.Duration) bool {
	if collectedAt.IsZero() {
		return false
	}
	window := interval * 3
	if window < minimumSensorFreshness {
		window = minimumSensorFreshness
	}
	age := now.Sub(collectedAt)
	return age >= 0 && age <= window
}

// NewManager validates configuration without starting privileged collectors.
func NewManager(cfg config.Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	processStatus := "pending"
	if !cfg.AppAttribution {
		processStatus = "disabled"
	}
	return &Manager{
		cfg:           cfg,
		attr:          attribution.New(cfg.TopApps),
		processStatus: processStatus,
		done:          make(chan struct{}),
		samples:       make(chan model.PowerSample, 1),
		errs:          make(chan error, 32),
	}, nil
}

// Start creates a session and begins collection. Callers should validate sudo
// credentials before calling Start when powermetrics is enabled.
func (m *Manager) Start(parent context.Context, metadata map[string]string) error {
	if parent == nil {
		return errors.New("monitor parent context must not be nil")
	}
	if err := parent.Err(); err != nil {
		return fmt.Errorf("monitor parent context is already done: %w", err)
	}

	// Start and Stop are infrequent lifecycle operations. Serializing the full
	// transition is simpler and safer than publishing a half-initialized manager.
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()

	m.mu.RLock()
	running := m.cancel != nil
	terminated := m.terminated
	m.mu.RUnlock()
	if running {
		return errors.New("monitor already running")
	}
	if terminated {
		return errors.New("monitor manager cannot be restarted; create a new manager")
	}

	if err := m.cfg.Prepare(); err != nil {
		return fmt.Errorf("prepare monitor configuration: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	session := buildSession(ctx, m.cfg, metadata)
	st, err := store.NewSessionWithOptions(
		m.cfg.DataDir,
		session,
		store.SessionOptions{
			SQLite:        m.cfg.SQLite,
			SampleLogging: m.cfg.Runtime.LoggingEnabled,
			LogInterval:   time.Duration(m.cfg.Runtime.LogIntervalMS) * time.Millisecond,
		},
	)
	if err != nil {
		cancel()
		return fmt.Errorf("create monitor session: %w", err)
	}

	m.mu.Lock()
	m.session = st.Session
	m.store = st
	m.cancel = cancel
	m.mu.Unlock()

	go m.run(ctx)
	return nil
}

func (m *Manager) run(ctx context.Context) {
	var processWG sync.WaitGroup
	var auxiliaryWG sync.WaitGroup

	m.mu.RLock()
	flushStore := m.store
	m.mu.RUnlock()
	if flushStore != nil {
		auxiliaryWG.Add(1)
		go func() {
			defer auxiliaryWG.Done()
			flushStore.ContextFlush(ctx, 2*time.Second, m.emitError)
		}()
	}

	defer func() {
		// Process collectors can still report errors or update latestProcesses.
		// Wait before closing public channels and the session store.
		processWG.Wait()
		auxiliaryWG.Wait()

		m.mu.Lock()
		st := m.store
		m.cancel = nil
		m.terminated = true
		m.mu.Unlock()

		if st != nil {
			if err := st.Close(); err != nil {
				select {
				case m.errs <- fmt.Errorf("close session store: %w", err):
				default:
				}
			}
		}
		close(m.samples)
		close(m.errs)
		close(m.done)
	}()

	pmInterval := time.Duration(m.cfg.Runtime.PowermetricsMS) * time.Millisecond
	processInterval := time.Duration(m.cfg.Runtime.AppAttributionMS) * time.Millisecond
	uiRefreshInterval := time.Duration(m.cfg.Runtime.UIRefreshMS) * time.Millisecond
	batteryInterval := time.Duration(m.cfg.Runtime.BatteryCollectionMS) * time.Millisecond
	loggingEnabled := m.cfg.Runtime.LoggingEnabled
	logInterval := time.Duration(m.cfg.Runtime.LogIntervalMS) * time.Millisecond

	pm := NewPowermetricsCollector(pmInterval)
	pmCh, pmErr := pm.Start(ctx)
	batteryCh, batteryErr := startBatterySampler(ctx, batteryInterval)

	processTicker := time.NewTicker(processInterval)
	defer processTicker.Stop()
	processInitial := time.NewTimer(750 * time.Millisecond)
	defer processInitial.Stop()
	psTicker := time.NewTicker(30 * time.Second)
	defer psTicker.Stop()

	var processMu sync.Mutex
	collectProcesses := func() {
		if !m.cfg.AppAttribution {
			return
		}
		if !processMu.TryLock() {
			return
		}
		defer processMu.Unlock()

		// A one-shot tasks sample should complete quickly. Keep a hard timeout
		// independent of the sampling cadence so monitor shutdown remains bounded.
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		activities, err := CollectTasksOnce(c)
		cancel()
		if err == nil && len(activities) > 0 {
			m.mu.Lock()
			m.latestProcesses = activities
			m.latestProcessesAt = time.Now()
			m.processStatus = "ok"
			m.mu.Unlock()
			return
		}

		tasksErr := err
		c, cancel = context.WithTimeout(ctx, 3*time.Second)
		fallback, fallbackErr := CollectPS(c)
		cancel()
		if fallbackErr == nil && len(fallback) > 0 {
			m.mu.Lock()
			m.latestProcesses = fallback
			m.latestProcessesAt = time.Now()
			m.processStatus = "fallback-ps"
			m.mu.Unlock()
			return
		}
		if ctx.Err() == nil {
			combined := fmt.Errorf("app attribution tasks sample: %w", tasksErr)
			if fallbackErr != nil {
				combined = errors.Join(combined, fmt.Errorf("app attribution ps fallback: %w", fallbackErr))
			} else {
				combined = errors.Join(combined, errors.New("app attribution ps fallback returned no rows"))
			}
			m.emitError(combined)
		}
		m.mu.Lock()
		m.latestProcesses = nil
		m.latestProcessesAt = time.Time{}
		m.processStatus = "unavailable"
		m.mu.Unlock()
	}

	launchProcessCollection := func() {
		processWG.Add(1)
		go func() {
			defer processWG.Done()
			collectProcesses()
		}()
	}

	var latestBattery BatterySnapshot
	haveBattery := false
	emitSample := func(now time.Time, publishLive bool) {
		if !haveBattery {
			return
		}
		m.mu.RLock()
		pmSnap := m.latestPM
		phase := m.phase
		session := m.session
		processesAt := m.latestProcessesAt
		processStatus := m.processStatus
		processes := append([]model.ProcessActivity(nil), m.latestProcesses...)
		m.mu.RUnlock()

		if !sensorFresh(now, pmSnap.Timestamp, pmInterval) {
			pmSnap = PowermetricsSnapshot{Status: "stale"}
		}
		if !sensorFresh(now, processesAt, processInterval) {
			processes = nil
			if processStatus != "unavailable" && processStatus != "disabled" {
				processStatus = "stale"
			}
		}
		statuses := make(map[string]string, len(latestBattery.Diagnostics.Status)+2)
		for key, value := range latestBattery.Diagnostics.Status {
			statuses[key] = value
		}
		statuses["powermetrics"] = pmSnap.Status
		statuses["process_attribution"] = processStatus

		sample := m.compose(
			now, session.ID, phase,
			latestBattery.Battery, latestBattery.Adapter,
			pmSnap, processes, statuses, latestBattery.Diagnostics.Warnings,
		)
		if err := m.persist(sample); err != nil {
			m.emitError(err)
		}
		if publishLive {
			publishLatestSample(ctx, m.samples, sample)
		}
	}

	// Live publication and durable sampling have independent deadlines. The
	// single timer wakes for their union and composes once when they coincide.
	// Every composed sample is offered to the store, whose cadence gate retains
	// only the latest pending value between durable writes.
	var (
		sampleTimer  *time.Timer
		sampleTimerC <-chan time.Time
		nextLive     time.Time
		nextLog      time.Time
	)
	defer func() {
		if sampleTimer != nil {
			sampleTimer.Stop()
		}
	}()
	resetSampleTimer := func() {
		deadline := nextSampleDeadline(nextLive, nextLog, loggingEnabled)
		delay := time.Until(deadline)
		if delay < 0 {
			delay = 0
		}
		if sampleTimer == nil {
			sampleTimer = time.NewTimer(delay)
			sampleTimerC = sampleTimer.C
			return
		}
		sampleTimer.Reset(delay)
	}
	startSampleSchedule := func(now time.Time) {
		nextLive = now.Add(uiRefreshInterval)
		if loggingEnabled {
			nextLog = now.Add(logInterval)
		}
		resetSampleTimer()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-pmCh:
			if !ok {
				pmCh = nil
				continue
			}
			m.mu.Lock()
			m.latestPM = snap
			m.mu.Unlock()
		case err, ok := <-pmErr:
			if !ok {
				pmErr = nil
				continue
			}
			if err != nil {
				m.emitError(err)
			}
		case snapshot, ok := <-batteryCh:
			if !ok {
				batteryCh = nil
				continue
			}
			first := !haveBattery
			latestBattery = snapshot
			haveBattery = true
			if first {
				now := time.Now()
				emitSample(now, true)
				startSampleSchedule(now)
			}
		case err, ok := <-batteryErr:
			if !ok {
				batteryErr = nil
				continue
			}
			if err != nil {
				m.emitError(err)
			}
		case <-processInitial.C:
			launchProcessCollection()
		case <-processTicker.C:
			launchProcessCollection()
		case <-psTicker.C:
			if !m.cfg.AppAttribution {
				continue
			}
			m.mu.RLock()
			hasProcesses := len(m.latestProcesses) > 0
			m.mu.RUnlock()
			if !hasProcesses {
				launchProcessCollection()
			}
		case <-sampleTimerC:
			now := time.Now()
			publishLive := !now.Before(nextLive)
			emitSample(now, publishLive)
			if publishLive {
				nextLive = advanceDeadline(nextLive, uiRefreshInterval, now)
			}
			if loggingEnabled && !now.Before(nextLog) {
				nextLog = advanceDeadline(nextLog, logInterval, now)
			}
			resetSampleTimer()
		}
	}
}

func nextSampleDeadline(nextLive, nextLog time.Time, loggingEnabled bool) time.Time {
	if loggingEnabled && nextLog.Before(nextLive) {
		return nextLog
	}
	return nextLive
}

func advanceDeadline(deadline time.Time, interval time.Duration, now time.Time) time.Time {
	if deadline.After(now) {
		return deadline
	}
	missed := now.Sub(deadline)/interval + 1
	return deadline.Add(missed * interval)
}

func startBatterySampler(
	ctx context.Context,
	interval time.Duration,
) (<-chan BatterySnapshot, <-chan error) {
	out := make(chan BatterySnapshot, 1)
	errCh := make(chan error, 8)
	go func() {
		defer close(out)
		defer close(errCh)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		collect := func() {
			collectionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			snapshot, err := (BatteryCollector{}).CollectDetailed(collectionCtx)
			cancel()
			if err != nil {
				nonblockErr(errCh, err)
				return
			}
			publishLatestBattery(ctx, out, snapshot)
		}
		collect()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				collect()
			}
		}
	}()
	return out, errCh
}

func publishLatestBattery(ctx context.Context, out chan BatterySnapshot, snapshot BatterySnapshot) {
	select {
	case out <- snapshot:
		return
	default:
	}
	select {
	case <-out:
	default:
	}
	select {
	case out <- snapshot:
	case <-ctx.Done():
	}
}

func publishLatestSample(ctx context.Context, out chan model.PowerSample, sample model.PowerSample) {
	select {
	case out <- sample:
		return
	default:
	}
	select {
	case <-out:
	default:
	}
	select {
	case out <- sample:
	case <-ctx.Done():
	}
}

func (m *Manager) compose(
	now time.Time,
	sessionID string,
	phase string,
	battery model.BatterySample,
	adapter model.AdapterSample,
	pm PowermetricsSnapshot,
	processes []model.ProcessActivity,
	collectorStatus map[string]string,
	sourceWarnings []string,
) model.PowerSample {
	// Stateful estimators are serialized independently from the public snapshot
	// mutex. This keeps Status/TUI reads fast while attribution work runs.
	m.stateMu.Lock()
	m.seq++
	sequence := m.seq

	thermal := pm.Thermal
	trend, state := m.updateThermal(now, battery.TemperatureC)
	thermal.BatteryTrendCPerMin = trend
	thermal.BatteryState = state
	switch {
	case thermal.MacOSPressure != "" && state != "":
		thermal.Summary = "macOS " + thermal.MacOSPressure + " / battery " + state
		thermal.Source = "powermetrics thermal + battery trend"
	case thermal.MacOSPressure != "":
		thermal.Summary = "macOS " + thermal.MacOSPressure
		thermal.Source = "powermetrics thermal"
	case state != "":
		thermal.Summary = "battery " + state
		thermal.Source = "battery trend"
	}

	total, source := primaryLoad(battery, pm.Components)
	adapter = estimateAdapter(adapter, battery, total)
	attr := model.AttributionResult{}
	if m.cfg.AppAttribution {
		attr = m.attr.Observe(now, total, battery.PowerSource, source, pm.Components, processes)
	}
	warnings := append([]string(nil), sourceWarnings...)
	warnings = append(warnings, warningsFor(battery, adapter, thermal, pm, processes, m.cfg.AppAttribution)...)
	events := m.transitionEvents(now, sessionID, battery)
	m.stateMu.Unlock()

	sample := model.PowerSample{
		Schema:            version.PowerSampleSchema,
		Timestamp:         now,
		SessionID:         sessionID,
		Sequence:          sequence,
		Phase:             phase,
		Battery:           battery,
		Adapter:           adapter,
		Components:        cloneComponents(pm.Components),
		Thermal:           thermal,
		PrimaryLoadW:      total,
		PrimaryLoadSource: source,
		BaselineLoadW:     attr.BaselineWatts,
		Attribution:       cloneAttribution(attr),
		CollectorStatus:   cloneStringMap(collectorStatus),
		Warnings:          append([]string(nil), warnings...),
	}

	m.mu.Lock()
	m.latest = clonePowerSample(&sample)
	st := m.store
	m.mu.Unlock()

	// Disk I/O is deliberately outside every manager state lock.
	for _, event := range events {
		if st != nil {
			if err := st.WriteEvent(event); err != nil {
				m.emitError(fmt.Errorf("write transition event: %w", err))
			}
		}
	}
	return sample
}

func (m *Manager) updateThermal(now time.Time, temp float64) (float64, string) {
	if temp <= 0 {
		return 0, ""
	}
	m.tempHistory = append(m.tempHistory, tempPoint{t: now, v: temp})
	cutoff := now.Add(-5 * time.Minute)
	first := 0
	for first < len(m.tempHistory) && m.tempHistory[first].t.Before(cutoff) {
		first++
	}
	if first > 0 {
		copy(m.tempHistory, m.tempHistory[first:])
		m.tempHistory = m.tempHistory[:len(m.tempHistory)-first]
	}
	trend := 0.0
	if len(m.tempHistory) >= 2 {
		a := m.tempHistory[0]
		b := m.tempHistory[len(m.tempHistory)-1]
		minutes := b.t.Sub(a.t).Minutes()
		if minutes > 0 {
			trend = (b.v - a.v) / minutes
		}
	}
	state := "stable"
	if trend > 0.15 {
		state = "rising"
	} else if trend < -0.15 {
		state = "falling"
	}
	return trend, state
}

func (m *Manager) transitionEvents(
	now time.Time,
	sessionID string,
	battery model.BatterySample,
) []model.Event {
	events := make([]model.Event, 0, 2)
	if m.previousSource != "" && m.previousSource != battery.PowerSource {
		events = append(events, model.Event{
			Schema:    version.EventSchema,
			Timestamp: now,
			SessionID: sessionID,
			Type:      "power_source_changed",
			Message:   m.previousSource + " -> " + battery.PowerSource,
		})
	}
	if m.previousState != "" && m.previousState != battery.State {
		events = append(events, model.Event{
			Schema:    version.EventSchema,
			Timestamp: now,
			SessionID: sessionID,
			Type:      "battery_state_changed",
			Message:   m.previousState + " -> " + battery.State,
		})
	}
	m.previousSource = battery.PowerSource
	m.previousState = battery.State
	return events
}

func (m *Manager) persist(s model.PowerSample) error {
	m.mu.RLock()
	st := m.store
	m.mu.RUnlock()
	if st == nil {
		return errors.New("session store unavailable")
	}
	return st.OfferSample(s)
}

func (m *Manager) emitError(err error) {
	if err == nil {
		return
	}
	select {
	case m.errs <- err:
	default:
	}
	m.mu.RLock()
	st := m.store
	session := m.session
	m.mu.RUnlock()
	if st != nil {
		_ = st.WriteEvent(model.Event{
			Schema:    version.EventSchema,
			Timestamp: time.Now(),
			SessionID: session.ID,
			Type:      "collector_error",
			Message:   err.Error(),
		})
	}
}

// Stop requests shutdown and waits for all writers to flush.
func (m *Manager) Stop(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stop context must not be nil")
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()

	m.mu.RLock()
	cancel := m.cancel
	done := m.done
	m.mu.RUnlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop monitor: %w", ctx.Err())
	}
}

func (m *Manager) Samples() <-chan model.PowerSample { return m.samples }
func (m *Manager) Errors() <-chan error              { return m.errs }
func (m *Manager) SetPhase(v string) {
	m.mu.Lock()
	m.phase = strings.TrimSpace(v)
	m.mu.Unlock()
}
func (m *Manager) Phase() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.phase
}
func (m *Manager) LastSample() *model.PowerSample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.latest == nil {
		return nil
	}
	return clonePowerSample(m.latest)
}
func (m *Manager) Session() model.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneSession(m.session)
}
func (m *Manager) SessionDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.store == nil {
		return ""
	}
	return m.store.Dir
}

// Snapshot returns a consistent cumulative view of the session through a
// precise byte boundary while monitoring continues.
func (m *Manager) Snapshot() (store.SessionSnapshot, error) {
	m.mu.RLock()
	st := m.store
	m.mu.RUnlock()
	if st == nil {
		return store.SessionSnapshot{}, errors.New("session store unavailable")
	}
	return st.Snapshot()
}

// FlushPending durably records the latest pending live sample at a semantic
// boundary without changing the configured steady-state log cadence.
func (m *Manager) FlushPending() error {
	m.mu.RLock()
	st := m.store
	m.mu.RUnlock()
	if st == nil {
		return errors.New("session store unavailable")
	}
	return st.FlushPendingSample()
}
func (m *Manager) WriteTestRun(v model.TestRun) error {
	m.mu.RLock()
	st := m.store
	m.mu.RUnlock()
	if st == nil {
		return errors.New("session store unavailable")
	}
	return st.WriteTestRun(v)
}

// CollectOnce performs an isolated battery and powermetrics snapshot, useful
// for sensor scans and parity checks.
func CollectOnce(ctx context.Context) (model.PowerSample, error) {
	batterySnapshot, err := (BatteryCollector{}).CollectDetailed(ctx)
	if err != nil {
		return model.PowerSample{}, err
	}
	battery := batterySnapshot.Battery
	adapter := batterySnapshot.Adapter
	pm := PowermetricsSnapshot{Timestamp: time.Now(), Status: "unavailable"}
	if runtime.GOOS == "darwin" {
		collector := NewPowermetricsCollector(time.Second)
		args := collector.commandArgs()
		// Insert one-shot sample count before the remaining powermetrics flags.
		args = append(args, "-n", "1")
		if result, runErr := execx.Run(ctx, 32<<20, "/usr/bin/sudo", args...); runErr == nil {
			if values, parseErr := plistx.ParseNUL(result.Stdout); parseErr == nil && len(values) > 0 {
				if root, ok := values[len(values)-1].(map[string]any); ok {
					pm = ParsePowermetrics(root)
				}
			}
		}
	}
	total, source := primaryLoad(battery, pm.Components)
	return model.PowerSample{
		Schema:            version.PowerSampleSchema,
		Timestamp:         time.Now(),
		Battery:           battery,
		Adapter:           estimateAdapter(adapter, battery, total),
		Components:        pm.Components,
		Thermal:           pm.Thermal,
		PrimaryLoadW:      total,
		PrimaryLoadSource: source,
	}, nil
}

func primaryLoad(b model.BatterySample, c model.ComponentSample) (float64, string) {
	if b.PowerSource == "Battery Power" && b.NetWatts < -0.5 {
		return math.Abs(b.NetWatts), "battery discharge watts"
	}
	if b.BMSSystemPowerW > 0 {
		return b.BMSSystemPowerW, "BMS SystemPower"
	}
	if b.SystemEffectiveTotalLoadW > 0 {
		return b.SystemEffectiveTotalLoadW, "PowerTelemetry SystemEffectiveTotalLoad"
	}
	if c.PackageWatts > 0 {
		return c.PackageWatts, "powermetrics package estimate"
	}
	sum := c.CPUWatts + c.GPUWatts + c.ANEWatts + c.DRAMWatts
	if sum > 0 {
		return sum, "powermetrics component sum"
	}
	return 0, "unavailable"
}

func estimateAdapter(a model.AdapterSample, b model.BatterySample, systemW float64) model.AdapterSample {
	if !a.Connected {
		return a
	}
	charge := math.Max(0, b.NetWatts)
	assist := math.Max(0, -b.NetWatts)
	switch {
	case charge > 0.5:
		a.OutputEstimateW = systemW + charge
		a.OutputEstimateSource = "system load + battery charge acceptance"
	case assist > 0.5:
		a.BatteryAssistW = assist
		a.OutputEstimateW = math.Max(0, systemW-assist)
		a.OutputEstimateSource = "system load minus battery assist"
	default:
		a.OutputEstimateW = systemW
		a.OutputEstimateSource = "system load estimate"
	}
	if a.RatedWatts <= 0 {
		a.RatedWatts = a.ContractWatts
	}
	if a.RatedWatts > 0 {
		a.LoadPercent = a.OutputEstimateW / a.RatedWatts * 100
		a.HeadroomW = a.RatedWatts - a.OutputEstimateW
	}
	return a
}

func warningsFor(
	b model.BatterySample,
	a model.AdapterSample,
	t model.ThermalSample,
	pm PowermetricsSnapshot,
	processes []model.ProcessActivity,
	appAttributionEnabled bool,
) []string {
	var warnings []string
	if b.TemperatureC >= 42 {
		warnings = append(warnings, fmt.Sprintf("battery temperature high: %.1f C", b.TemperatureC))
	}
	if b.CellVoltageDeltaMV >= 35 {
		warnings = append(warnings, fmt.Sprintf("cell voltage delta elevated: %.0f mV", b.CellVoltageDeltaMV))
	}
	if a.LoadPercent >= 98 {
		warnings = append(warnings, "charger at or near estimated capacity")
	}
	if t.MacOSPressure != "" && !strings.EqualFold(t.MacOSPressure, "Nominal") {
		warnings = append(warnings, "macOS thermal pressure: "+t.MacOSPressure)
	}
	if pm.Timestamp.IsZero() {
		warnings = append(warnings, "powermetrics unavailable; component/app estimates limited")
	}
	if appAttributionEnabled && len(processes) == 0 {
		warnings = append(warnings, "process attribution unavailable")
	}
	return warnings
}

func cloneSession(value model.Session) model.Session {
	if value.Metadata != nil {
		value.Metadata = make(map[string]string, len(value.Metadata))
		for key, item := range value.Metadata {
			value.Metadata[key] = item
		}
	}
	return value
}

func cloneComponents(value model.ComponentSample) model.ComponentSample {
	value.Clusters = append([]model.ClusterSample(nil), value.Clusters...)
	return value
}

func cloneAttribution(value model.AttributionResult) model.AttributionResult {
	value.Apps = append([]model.AppPower(nil), value.Apps...)
	return value
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	copyValue := make(map[string]string, len(value))
	for key, item := range value {
		copyValue[key] = item
	}
	return copyValue
}

func clonePowerSample(value *model.PowerSample) *model.PowerSample {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.Components = cloneComponents(value.Components)
	copyValue.Attribution = cloneAttribution(value.Attribution)
	copyValue.CollectorStatus = cloneStringMap(value.CollectorStatus)
	copyValue.Warnings = append([]string(nil), value.Warnings...)
	return &copyValue
}

func uniqueSessionID(now time.Time) string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err == nil {
		return now.Format("20060102_150405.000000") + "_" + hex.EncodeToString(suffix[:])
	}
	return fmt.Sprintf("%s_%d", now.Format("20060102_150405.000000"), os.Getpid())
}

func buildSession(ctx context.Context, cfg config.Config, metadata map[string]string) model.Session {
	now := time.Now()
	id := uniqueSessionID(now)
	host, _ := os.Hostname()
	session := model.Session{
		Schema:          version.SessionSchema,
		ID:              id,
		Version:         version.Version,
		StartedAt:       now,
		Hostname:        host,
		DataDirectory:   cfg.DataDir,
		RuntimeSettings: cfg.Runtime,
		EffectiveOptions: &model.EffectiveCollectionOptions{
			AppAttribution: cfg.AppAttribution,
			TopApps:        cfg.TopApps,
			SQLiteMirror:   cfg.SQLite,
			SafeMode:       cfg.SafeMode,
		},
		Metadata: cloneStringMap(metadata),
	}
	if runtime.GOOS != "darwin" {
		return session
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if result, err := execx.Run(c, 1<<20, "/usr/bin/sw_vers"); err == nil {
		for _, line := range strings.Split(string(result.Stdout), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			if key == "ProductVersion" {
				session.OSVersion = value
			}
			if key == "BuildVersion" {
				session.OSBuild = value
			}
		}
	}
	if result, err := execx.Run(c, 4<<20, "/usr/sbin/system_profiler", "SPHardwareDataType"); err == nil {
		for _, line := range strings.Split(string(result.Stdout), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Model Identifier:") {
				session.Machine = strings.TrimSpace(strings.TrimPrefix(line, "Model Identifier:"))
			}
			if strings.HasPrefix(line, "Chip:") {
				session.Chip = strings.TrimSpace(strings.TrimPrefix(line, "Chip:"))
			}
		}
	}
	return session
}
