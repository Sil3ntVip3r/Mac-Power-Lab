package collector

import (
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
)

func TestCadenceDiagnosticsTrackRequestedObservedAndReplacedFrames(t *testing.T) {
	settings := config.DefaultRuntimeSettings()
	settings.UIRefreshMS = 500
	settings.BatteryCollectionMS = 1_000
	settings.PowermetricsMS = 2_000
	settings.AppAttributionMS = 10_000
	settings.LogIntervalMS = 5_000
	state := newCadenceState(settings)

	base := time.Unix(1_700_000_000, 0)
	state.observeUI(base, false)
	state.observeUI(base.Add(500*time.Millisecond), true)
	state.observeUI(base.Add(time.Second), false)
	state.observeBattery(base)
	state.observeBattery(base.Add(time.Second))
	state.observePowermetrics(base)
	state.observePowermetrics(base.Add(2 * time.Second))
	state.observeApps(base)
	state.observeApps(base.Add(10 * time.Second))
	state.observeLog(base)
	state.observeLog(base.Add(5 * time.Second))

	got := state.snapshot()
	if got.UIRefresh.RequestedMS != 500 || got.UIRefresh.ObservedMS != 500 || got.UIRefresh.Observations != 2 {
		t.Fatalf("UI cadence=%+v", got.UIRefresh)
	}
	if got.BatteryCollection.ObservedMS != 1_000 || got.Powermetrics.ObservedMS != 2_000 ||
		got.AppAttribution.ObservedMS != 10_000 || got.DurableLogging.ObservedMS != 5_000 {
		t.Fatalf("cadence=%+v", got)
	}
	if got.LivePublications != 3 {
		t.Fatalf("live publications=%d", got.LivePublications)
	}
	if got.ReplacedStreamFrames != 1 {
		t.Fatalf("replaced=%d", got.ReplacedStreamFrames)
	}
}

func TestCadenceDiagnosticsDisableDurableRequestForLiveOnly(t *testing.T) {
	settings := config.DefaultRuntimeSettings()
	settings.LoggingEnabled = false
	settings.LogIntervalMS = 0
	got := newCadenceState(settings).snapshot()
	if got.DurableLogging.RequestedMS != 0 {
		t.Fatalf("durable logging request=%d", got.DurableLogging.RequestedMS)
	}
}
