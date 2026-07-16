package collector

import (
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

const cadenceEWMAAlpha = 0.25

type cadenceTracker struct {
	requested    time.Duration
	last         time.Time
	observed     time.Duration
	observations uint64
}

func (tracker *cadenceTracker) observe(now time.Time) {
	if now.IsZero() {
		return
	}
	if !tracker.last.IsZero() {
		delta := now.Sub(tracker.last)
		if delta > 0 {
			if tracker.observations == 0 {
				tracker.observed = delta
			} else {
				tracker.observed = time.Duration(
					(1-cadenceEWMAAlpha)*float64(tracker.observed) +
						cadenceEWMAAlpha*float64(delta),
				)
			}
			tracker.observations++
		}
	}
	tracker.last = now
}

func (tracker cadenceTracker) metric() model.CadenceMetric {
	value := model.CadenceMetric{
		RequestedMS:  tracker.requested.Milliseconds(),
		Observations: tracker.observations,
		LastAt:       tracker.last,
	}
	if tracker.observed > 0 {
		value.ObservedMS = float64(tracker.observed) / float64(time.Millisecond)
	}
	return value
}

type cadenceState struct {
	mu sync.Mutex

	uiRefresh         cadenceTracker
	batteryCollection cadenceTracker
	powermetrics      cadenceTracker
	appAttribution    cadenceTracker
	durableLogging    cadenceTracker
	livePublications  uint64
	replacedStream    uint64
}

func newCadenceState(settings model.RuntimeSettings) *cadenceState {
	logInterval := time.Duration(settings.LogIntervalMS) * time.Millisecond
	if !settings.LoggingEnabled {
		logInterval = 0
	}
	return &cadenceState{
		uiRefresh: cadenceTracker{
			requested: time.Duration(settings.UIRefreshMS) * time.Millisecond,
		},
		batteryCollection: cadenceTracker{
			requested: time.Duration(settings.BatteryCollectionMS) * time.Millisecond,
		},
		powermetrics: cadenceTracker{
			requested: time.Duration(settings.PowermetricsMS) * time.Millisecond,
		},
		appAttribution: cadenceTracker{
			requested: time.Duration(settings.AppAttributionMS) * time.Millisecond,
		},
		durableLogging: cadenceTracker{requested: logInterval},
	}
}

func (state *cadenceState) observeUI(now time.Time, replaced bool) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.uiRefresh.observe(now)
	state.livePublications++
	if replaced {
		state.replacedStream++
	}
	state.mu.Unlock()
}

func (state *cadenceState) observeBattery(now time.Time) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.batteryCollection.observe(now)
	state.mu.Unlock()
}

func (state *cadenceState) observePowermetrics(now time.Time) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.powermetrics.observe(now)
	state.mu.Unlock()
}

func (state *cadenceState) observeApps(now time.Time) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.appAttribution.observe(now)
	state.mu.Unlock()
}

func (state *cadenceState) observeLog(now time.Time) {
	if state == nil {
		return
	}
	state.mu.Lock()
	state.durableLogging.observe(now)
	state.mu.Unlock()
}

func (state *cadenceState) snapshot() model.CadenceDiagnostics {
	if state == nil {
		return model.CadenceDiagnostics{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return model.CadenceDiagnostics{
		UIRefresh:            state.uiRefresh.metric(),
		BatteryCollection:    state.batteryCollection.metric(),
		Powermetrics:         state.powermetrics.metric(),
		AppAttribution:       state.appAttribution.metric(),
		DurableLogging:       state.durableLogging.metric(),
		LivePublications:     state.livePublications,
		ReplacedStreamFrames: state.replacedStream,
	}
}
