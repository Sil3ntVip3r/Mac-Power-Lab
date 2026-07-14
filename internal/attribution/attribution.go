// Package attribution estimates application power from task/coalition activity.
package attribution

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

const (
	baselineSampleLimit = 32
	energyEntryLimit    = 4096
	energyEntryTTL      = 30 * time.Minute
)

// Attributor is a bounded, thread-safe streaming app-power model.
type Attributor struct {
	mu        sync.Mutex
	topN      int
	baselines map[string]*baselineWindow
	last      time.Time
	energy    map[string]energyEntry
}

type energyEntry struct {
	wattHours float64
	lastSeen  time.Time
}

// baselineWindow keeps the lowest observed values rather than a rolling window.
// A rolling window eventually forgets idle samples during a long stress test and
// incorrectly reclassifies the workload as baseline power.
type baselineWindow struct {
	values []float64 // sorted ascending
	max    int
}

func (w *baselineWindow) add(value float64) {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	index := sort.SearchFloat64s(w.values, value)
	if len(w.values) >= w.max && index >= w.max {
		return
	}
	w.values = append(w.values, 0)
	copy(w.values[index+1:], w.values[index:])
	w.values[index] = value
	if len(w.values) > w.max {
		w.values = w.values[:w.max]
	}
}

func (w *baselineWindow) baseline() float64 {
	if len(w.values) == 0 {
		return 0
	}
	// Use the median of the lower envelope to reject a single anomalously low
	// sample while keeping baseline stable across long workload phases.
	return w.values[len(w.values)/2]
}

// New creates an attributor that retains at most topN app rows per sample.
func New(topN int) *Attributor {
	if topN < 1 {
		topN = 10
	}
	return &Attributor{
		topN:      topN,
		baselines: make(map[string]*baselineWindow, 2),
		energy:    make(map[string]energyEntry, 128),
	}
}

// Observe returns a calibrated app-power estimate. Watts remain estimates because
// macOS exposes activity and Energy Impact, not a direct electrical meter per app.
func (a *Attributor) Observe(
	timestamp time.Time,
	totalWatts float64,
	powerSource string,
	totalSource string,
	components model.ComponentSample,
	activities []model.ProcessActivity,
) model.AttributionResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	if !finitePositive(totalWatts) {
		totalWatts = 0
	}

	sourceKey := strings.TrimSpace(powerSource)
	if sourceKey == "" {
		sourceKey = "unknown"
	}
	window := a.baselines[sourceKey]
	if window == nil {
		window = &baselineWindow{max: baselineSampleLimit}
		a.baselines[sourceKey] = window
	}
	window.add(totalWatts)
	baseline := window.baseline()
	if totalWatts > 0 && baseline > totalWatts {
		baseline = totalWatts
	}
	dynamic := math.Max(0, totalWatts-baseline)

	grouped := group(activities)
	if len(grouped) == 0 || totalWatts <= 0 {
		a.last = timestamp
		a.pruneEnergy(timestamp, nil)
		return model.AttributionResult{
			Method:        "unavailable",
			Confidence:    "low",
			BaselineWatts: baseline,
			DynamicWatts:  dynamic,
		}
	}

	reportedCPU := math.Max(0, components.CPUWatts)
	reportedGPU := math.Max(0, components.GPUWatts)
	reportedComponents := reportedCPU + reportedGPU
	componentScale := 1.0
	if reportedComponents > dynamic && reportedComponents > 0 {
		componentScale = dynamic / reportedComponents
	}
	// When reported CPU+GPU power exceeds the dynamic pool, scale both
	// proportionally. Giving CPU first claim and GPU only the remainder biases
	// attribution based on field order rather than measured component ratios.
	cpuPool := reportedCPU * componentScale
	gpuPool := reportedGPU * componentScale
	residualPool := math.Max(0, dynamic-cpuPool-gpuPool)

	cpuTotal := sumMetric(grouped, func(activity model.ProcessActivity) float64 {
		return math.Max(0, activity.CPUTimeMSPerS)
	})
	gpuTotal := sumMetric(grouped, func(activity model.ProcessActivity) float64 {
		return math.Max(0, activity.GPUTimeMSPerS)
	})
	residualTotal := sumMetric(grouped, residualWeight)
	overallTotal := sumMetric(grouped, overallWeight)

	deltaHours := a.deltaHours(timestamp)
	confidenceValue, method := confidence(grouped, totalSource)
	apps := make([]model.AppPower, 0, len(grouped))
	seen := make(map[string]struct{}, len(grouped))

	for _, activity := range grouped {
		cpuWatts := cpuPool * proportionalShare(
			math.Max(0, activity.CPUTimeMSPerS),
			cpuTotal,
			len(grouped),
		)
		gpuWatts := gpuPool * proportionalShare(
			math.Max(0, activity.GPUTimeMSPerS),
			gpuTotal,
			len(grouped),
		)
		residualWatts := residualPool * proportionalShare(
			residualWeight(activity),
			residualTotal,
			len(grouped),
		)
		dynamicWatts := cpuWatts + gpuWatts + residualWatts
		baselineWatts := baseline * proportionalShare(
			overallWeight(activity),
			overallTotal,
			len(grouped),
		)
		totalAppWatts := dynamicWatts + baselineWatts

		entry := a.energy[activity.Key]
		entry.wattHours += totalAppWatts * deltaHours
		entry.lastSeen = timestamp
		a.energy[activity.Key] = entry
		seen[activity.Key] = struct{}{}

		apps = append(apps, model.AppPower{
			Schema:                version.AppPowerSchema,
			Timestamp:             timestamp,
			Key:                   activity.Key,
			Name:                  display(activity),
			Category:              activity.Category,
			PID:                   activity.PID,
			ResponsiblePID:        activity.ResponsiblePID,
			CoalitionID:           activity.CoalitionID,
			EstimatedShareW:       totalAppWatts,
			EstimatedDynamicW:     dynamicWatts,
			EstimatedCPUW:         cpuWatts,
			EstimatedGPUW:         gpuWatts,
			EstimatedResidualW:    residualWatts,
			EstimatedEnergyWh:     entry.wattHours,
			EnergySharePercent:    percent(totalAppWatts, totalWatts),
			EnergyImpact:          activity.EnergyImpactPerS,
			CPUTimeMSPerS:         activity.CPUTimeMSPerS,
			GPUTimeMSPerS:         activity.GPUTimeMSPerS,
			InterruptWakeupsPerS:  activity.InterruptWakeupsPerS,
			IdleWakeupsPerS:       activity.IdleWakeupsPerS,
			DiskReadBytesPerS:     activity.DiskReadBytesPerS,
			DiskWriteBytesPerS:    activity.DiskWriteBytesPerS,
			NetworkReadBytesPerS:  activity.NetworkReadBytesPerS,
			NetworkWriteBytesPerS: activity.NetworkWriteBytesPerS,
			Confidence:            confidenceValue,
			AttributionSource:     method,
		})
	}

	a.pruneEnergy(timestamp, seen)
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].EstimatedDynamicW == apps[j].EstimatedDynamicW {
			return apps[i].Key < apps[j].Key
		}
		return apps[i].EstimatedDynamicW > apps[j].EstimatedDynamicW
	})
	if len(apps) > a.topN {
		apps = apps[:a.topN]
	}

	return model.AttributionResult{
		Method:            method,
		Confidence:        confidenceValue,
		BaselineWatts:     baseline,
		DynamicWatts:      dynamic,
		CPUComponentPoolW: cpuPool,
		GPUComponentPoolW: gpuPool,
		ResidualPoolW:     residualPool,
		Apps:              apps,
	}
}

func (a *Attributor) deltaHours(timestamp time.Time) float64 {
	delta := 0.0
	if !a.last.IsZero() {
		delta = timestamp.Sub(a.last).Hours()
		// Reject clock regressions and large gaps such as sleep/wake. The next
		// live sample resumes integration without charging the gap to apps.
		if delta < 0 || delta > 1.0/120 {
			delta = 0
		}
	}
	a.last = timestamp
	return delta
}

func (a *Attributor) pruneEnergy(timestamp time.Time, seen map[string]struct{}) {
	cutoff := timestamp.Add(-energyEntryTTL)
	for key, entry := range a.energy {
		if _, active := seen[key]; active {
			continue
		}
		if entry.lastSeen.Before(cutoff) {
			delete(a.energy, key)
		}
	}
	if len(a.energy) <= energyEntryLimit {
		return
	}

	type candidate struct {
		key    string
		seen   time.Time
		active bool
	}
	candidates := make([]candidate, 0, len(a.energy))
	for key, entry := range a.energy {
		_, active := seen[key]
		candidates = append(candidates, candidate{key: key, seen: entry.lastSeen, active: active})
	}
	sort.Slice(candidates, func(i, j int) bool {
		// Prefer evicting inactive entries. If a pathological sample contains
		// more keys than the hard limit, evict the oldest active entries too so
		// memory remains strictly bounded.
		if candidates[i].active != candidates[j].active {
			return !candidates[i].active
		}
		if candidates[i].seen.Equal(candidates[j].seen) {
			return candidates[i].key < candidates[j].key
		}
		return candidates[i].seen.Before(candidates[j].seen)
	})
	for _, candidate := range candidates {
		if len(a.energy) <= energyEntryLimit {
			break
		}
		delete(a.energy, candidate.key)
	}
}

func group(input []model.ProcessActivity) []model.ProcessActivity {
	grouped := make(map[string]model.ProcessActivity, len(input))
	for _, process := range input {
		key := strings.TrimSpace(process.Key)
		if key == "" {
			key = "name:" + process.Name
			process.Key = key
		}
		current, exists := grouped[key]
		if !exists {
			grouped[key] = process
			continue
		}
		current.EnergyImpact += process.EnergyImpact
		current.EnergyImpactPerS += process.EnergyImpactPerS
		current.CPUTimeMSPerS += process.CPUTimeMSPerS
		current.GPUTimeMSPerS += process.GPUTimeMSPerS
		current.InterruptWakeupsPerS += process.InterruptWakeupsPerS
		current.IdleWakeupsPerS += process.IdleWakeupsPerS
		current.DiskReadBytesPerS += process.DiskReadBytesPerS
		current.DiskWriteBytesPerS += process.DiskWriteBytesPerS
		current.NetworkReadBytesPerS += process.NetworkReadBytesPerS
		current.NetworkWriteBytesPerS += process.NetworkWriteBytesPerS
		current.RSSBytes += process.RSSBytes
		grouped[key] = current
	}

	out := make([]model.ProcessActivity, 0, len(grouped))
	for _, value := range grouped {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func residualWeight(process model.ProcessActivity) float64 {
	return math.Max(0, process.EnergyImpactPerS)*5 +
		math.Max(0, process.InterruptWakeupsPerS)*.2 +
		math.Max(0, process.IdleWakeupsPerS)*.2 +
		math.Max(0, process.DiskReadBytesPerS+process.DiskWriteBytesPerS)/1e6 +
		math.Max(0, process.NetworkReadBytesPerS+process.NetworkWriteBytesPerS)/1e6
}

func overallWeight(process model.ProcessActivity) float64 {
	return math.Max(0, process.CPUTimeMSPerS) +
		math.Max(0, process.GPUTimeMSPerS)*2 +
		residualWeight(process)
}

func sumMetric(
	activities []model.ProcessActivity,
	metric func(model.ProcessActivity) float64,
) float64 {
	total := 0.0
	for _, activity := range activities {
		total += math.Max(0, metric(activity))
	}
	return total
}

func proportionalShare(value, total float64, count int) float64 {
	if total > 0 {
		if value <= 0 {
			return 0
		}
		return value / total
	}
	if count > 0 {
		return 1 / float64(count)
	}
	return 0
}

func percent(value, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return value / total * 100
}

func display(process model.ProcessActivity) string {
	if strings.TrimSpace(process.DisplayName) != "" {
		return process.DisplayName
	}
	return process.Name
}

func confidence(processes []model.ProcessActivity, totalSource string) (string, string) {
	if len(processes) == 0 {
		return "low", "unavailable"
	}

	allCoalitions := true
	allPowermetrics := true
	for _, process := range processes {
		source := strings.ToLower(process.Source)
		if !strings.Contains(source, "coalition") {
			allCoalitions = false
		}
		if !strings.Contains(source, "powermetrics") {
			allPowermetrics = false
		}
	}

	method := "ps-activity-allocation"
	confidenceValue := "low"
	switch {
	case allCoalitions:
		method = "powermetrics-coalition-component-allocation"
		confidenceValue = "high"
	case allPowermetrics:
		method = "powermetrics-task-component-allocation"
		confidenceValue = "medium"
	}
	if strings.Contains(strings.ToLower(totalSource), "estimate") && confidenceValue == "high" {
		confidenceValue = "medium"
	}
	return confidenceValue, method
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
