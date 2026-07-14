// Package collector reads macOS power sources and powermetrics data.
package collector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	plistx "github.com/Sil3ntVip3r/Mac-Power-Lab/internal/plist"
)

const batteryCommandOutputLimit = 16 << 20

// BatteryDiagnostics records optional source availability separately from the
// mandatory AppleSmartBattery source. A missing optional source must not make
// the complete monitor fail, but it must be visible to callers.
type BatteryDiagnostics struct {
	Status   map[string]string
	Warnings []string
}

// BatterySnapshot is one battery/adapter collection plus source diagnostics.
type BatterySnapshot struct {
	Battery     model.BatterySample
	Adapter     model.AdapterSample
	Diagnostics BatteryDiagnostics
}

// BatteryCollector reads AppleSmartBattery, AppleSmartBatteryBank, and pmset.
type BatteryCollector struct{}

type commandOutcome struct {
	result execx.Result
	err    error
}

// Collect returns one battery/adapter snapshot. It preserves the original
// compact API while CollectDetailed exposes partial-source diagnostics.
func (collector BatteryCollector) Collect(
	ctx context.Context,
) (model.BatterySample, model.AdapterSample, error) {
	snapshot, err := collector.CollectDetailed(ctx)
	return snapshot.Battery, snapshot.Adapter, err
}

// CollectDetailed runs independent macOS power-source commands concurrently.
// AppleSmartBattery is mandatory. Battery-bank and pmset failures degrade the
// snapshot and are reported through Diagnostics rather than being swallowed.
func (BatteryCollector) CollectDetailed(ctx context.Context) (BatterySnapshot, error) {
	now := time.Now()
	snapshot := BatterySnapshot{
		Battery: model.BatterySample{CollectedAt: now},
		Diagnostics: BatteryDiagnostics{Status: map[string]string{
			"apple_smart_battery":      "pending",
			"apple_smart_battery_bank": "pending",
			"pmset_batt":               "pending",
		}},
	}
	if ctx == nil {
		return snapshot, errors.New("battery collector context must not be nil")
	}
	if runtime.GOOS != "darwin" {
		return snapshot, errors.New("battery collector requires macOS")
	}

	rootCh := make(chan commandOutcome, 1)
	bankCh := make(chan commandOutcome, 1)
	pmsetCh := make(chan commandOutcome, 1)

	go func() {
		result, err := execx.Run(
			ctx,
			batteryCommandOutputLimit,
			"/usr/sbin/ioreg",
			"-r", "-c", "AppleSmartBattery", "-a",
		)
		rootCh <- commandOutcome{result: result, err: err}
	}()
	go func() {
		result, err := execx.Run(
			ctx,
			batteryCommandOutputLimit,
			"/usr/sbin/ioreg",
			"-r", "-c", "AppleSmartBatteryBank", "-a",
		)
		bankCh <- commandOutcome{result: result, err: err}
	}()
	go func() {
		result, err := execx.Run(ctx, 1<<20, "/usr/bin/pmset", "-g", "batt")
		pmsetCh <- commandOutcome{result: result, err: err}
	}()

	rootOutcome := <-rootCh
	bankOutcome := <-bankCh
	pmsetOutcome := <-pmsetCh

	if rootOutcome.err != nil {
		snapshot.Diagnostics.Status["apple_smart_battery"] = "failed"
		return snapshot, fmt.Errorf("read AppleSmartBattery: %w", rootOutcome.err)
	}
	root, err := parseBatteryRoot(rootOutcome.result.Stdout)
	if err != nil {
		snapshot.Diagnostics.Status["apple_smart_battery"] = "failed"
		return snapshot, err
	}
	snapshot.Diagnostics.Status["apple_smart_battery"] = "ok"

	banks := []any(nil)
	if bankOutcome.err != nil {
		snapshot.Diagnostics.Status["apple_smart_battery_bank"] = "unavailable"
		snapshot.Diagnostics.Warnings = append(
			snapshot.Diagnostics.Warnings,
			"AppleSmartBatteryBank unavailable: "+shortError(bankOutcome.err),
		)
	} else {
		value, parseErr := plistx.Parse(bankOutcome.result.Stdout)
		if parseErr != nil {
			snapshot.Diagnostics.Status["apple_smart_battery_bank"] = "invalid"
			snapshot.Diagnostics.Warnings = append(
				snapshot.Diagnostics.Warnings,
				"AppleSmartBatteryBank parse failed: "+shortError(parseErr),
			)
		} else {
			banks = asSlice(value)
			snapshot.Diagnostics.Status["apple_smart_battery_bank"] = "ok"
		}
	}

	pmset := ""
	if pmsetOutcome.err != nil {
		snapshot.Diagnostics.Status["pmset_batt"] = "unavailable"
		snapshot.Diagnostics.Warnings = append(
			snapshot.Diagnostics.Warnings,
			"pmset battery status unavailable: "+shortError(pmsetOutcome.err),
		)
	} else {
		pmset = string(pmsetOutcome.result.Stdout)
		snapshot.Diagnostics.Status["pmset_batt"] = "ok"
	}

	snapshot.Battery, snapshot.Adapter = parseBatteryData(root, banks, pmset, now)
	return snapshot, nil
}

func parseBatteryRoot(data []byte) (map[string]any, error) {
	rootAny, err := plistx.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse AppleSmartBattery: %w", err)
	}
	roots := asSlice(rootAny)
	if len(roots) > 0 {
		if root := asMap(roots[0]); root != nil {
			return root, nil
		}
	}
	if root := asMap(rootAny); root != nil {
		return root, nil
	}
	return nil, errors.New("AppleSmartBattery plist did not contain a dictionary")
}

func parseBatteryData(
	root map[string]any,
	banks []any,
	pmset string,
	collectedAt time.Time,
) (model.BatterySample, model.AdapterSample) {
	battery := model.BatterySample{CollectedAt: collectedAt}
	adapter := model.AdapterSample{}
	combined := make([]any, 0, 1+len(banks))
	combined = append(combined, root)
	combined = append(combined, banks...)

	battery.ExternalConnected = asBool(first(root, "ExternalConnected", "AppleRawExternalConnected")) ||
		strings.Contains(pmset, "AC Power")
	battery.Charging = asBool(first(root, "IsCharging", "AppleRawBatteryCharging")) ||
		pmsetReportsCharging(pmset)
	if battery.ExternalConnected {
		battery.PowerSource = "AC Power"
	} else {
		battery.PowerSource = "Battery Power"
	}
	battery.State = parseBatteryState(pmset, battery.ExternalConnected, battery.Charging)
	if percent, ok := percentFromPMSet(pmset); ok {
		battery.Percent = percent
	} else {
		battery.Percent = asFloat(first(root, "CurrentCapacity"))
	}

	// AppleSmartBattery electrical fields are documented/observed in millivolts
	// and milliamps. Unit conversion must not depend on magnitude: a legitimate
	// 50 mA idle current is still 0.050 A, not 50 A.
	battery.VoltageV = millivoltsToVolts(asFloat(first(root, "Voltage")))
	battery.CurrentA = milliampsToAmps(asFloat(first(root, "Amperage", "InstantAmperage")))
	battery.NetWatts = battery.VoltageV * battery.CurrentA
	battery.TemperatureRaw = asFloat(first(combined, "Temperature", "BatteryTemperature"))
	battery.TemperatureC = decodeTemperature(battery.TemperatureRaw)
	if battery.TemperatureC != 0 {
		battery.TemperatureF = battery.TemperatureC*9/5 + 32
	}
	battery.VirtualTemperatureC = decodeTemperature(asFloat(first(combined, "VirtualTemperature")))
	battery.CycleCount = asInt(first(root, "CycleCount"))
	battery.CurrentCapacityMAh = asFloat(first(root, "AppleRawCurrentCapacity", "CurrentCapacity"))
	battery.FullChargeCapacityMAh = asFloat(first(root, "AppleRawMaxCapacity", "MaxCapacity"))
	battery.DesignCapacityMAh = asFloat(first(root, "DesignCapacity"))
	if battery.DesignCapacityMAh > 0 && battery.FullChargeCapacityMAh > 0 {
		battery.HealthPercent = battery.FullChargeCapacityMAh / battery.DesignCapacityMAh * 100
	}
	if battery.VoltageV > 0 {
		battery.EstimatedRemainingWh = battery.CurrentCapacityMAh / 1000 * battery.VoltageV
		battery.EstimatedFullWh = battery.FullChargeCapacityMAh / 1000 * battery.VoltageV
	}
	battery.CellDisconnectCount = asInt(first(root, "BatteryCellDisconnectCount"))
	battery.TimeToEmptyMinutes = validBatteryMinutes(asFloat(first(root, "TimeRemaining", "AvgTimeToEmpty")))
	battery.TimeToFullMinutes = validBatteryMinutes(asFloat(first(root, "AvgTimeToFull")))

	// BatteryData SystemPower is observed in watts. Telemetry fields vary by OS
	// build, so they retain the conservative compatibility normalization.
	battery.BMSSystemPowerW = wattsValue(first(combined, "SystemPower"))
	battery.SystemEffectiveTotalLoadW = normalizeTelemetryPower(first(root, "SystemEffectiveTotalLoad"))
	battery.PowerDistributionInputW = normalizeTelemetryPower(first(root, "IPDInputPower"))

	cells := positiveFinite(collectNumbers(combined, "CellVoltage"))
	if len(cells) > 0 {
		sort.Float64s(cells)
		battery.CellVoltageMinMV = cells[0]
		battery.CellVoltageMaxMV = cells[len(cells)-1]
		battery.CellVoltageDeltaMV = battery.CellVoltageMaxMV - battery.CellVoltageMinMV
	}
	qmax := positiveFinite(collectNumbers(combined, "Qmax"))
	if len(qmax) > 1 {
		sort.Float64s(qmax)
		battery.QMaxDelta = qmax[len(qmax)-1] - qmax[0]
	}
	ra := positiveFinite(collectNumbers(combined, "WeightedRa"))
	if len(ra) > 1 {
		sort.Float64s(ra)
		battery.WeightedRADelta = ra[len(ra)-1] - ra[0]
	}

	details := asMap(first(root, "AdapterDetails"))
	rawDetails := first(root, "AppleRawAdapterDetails")
	if details == nil {
		if values := asSlice(rawDetails); len(values) > 0 {
			details = asMap(values[0])
		} else {
			details = asMap(rawDetails)
		}
	}
	if details != nil {
		adapter.Name = asString(first(details, "Name", "Description", "Manufacturer"))
		adapter.RatedWatts = wattsValue(first(details, "Watts"))
		adapter.ContractVoltageV = millivoltsToVolts(asFloat(first(details, "AdapterVoltage", "Voltage")))
		adapter.ContractCurrentA = milliampsToAmps(asFloat(first(details, "Current")))
		adapter.ContractWatts = adapter.ContractVoltageV * adapter.ContractCurrentA
	}
	adapter.Connected = battery.ExternalConnected
	adapter.PortControllerMaxPowerW = milliwattsToWatts(
		maxNumber(positiveFinite(collectNumbers(root, "PortControllerMaxPower"))),
	)
	return battery, adapter
}

func parseBatteryState(_ string, external, charging bool) string {
	switch {
	case charging:
		return "Charging"
	case external:
		return "Plugged In"
	default:
		return "Discharging"
	}
}

func pmsetReportsCharging(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "discharging") ||
		strings.Contains(lower, "not charging") ||
		strings.Contains(lower, "charged") {
		return false
	}
	return strings.Contains(lower, "charging") || strings.Contains(lower, "finishing charge")
}

func percentFromPMSet(value string) (float64, bool) {
	for _, field := range strings.Fields(value) {
		if !strings.HasSuffix(field, "%;") && !strings.HasSuffix(field, "%") {
			continue
		}
		field = strings.TrimSuffix(strings.TrimSuffix(field, ";"), "%")
		var percent float64
		if _, err := fmt.Sscanf(field, "%f", &percent); err == nil && percent >= 0 && percent <= 100 {
			return percent, true
		}
	}
	return 0, false
}

func validBatteryMinutes(value float64) float64 {
	if value <= 0 || value >= 65535 {
		return 0
	}
	return value
}

func millivoltsToVolts(value float64) float64 { return value / 1000 }
func milliampsToAmps(value float64) float64   { return value / 1000 }
func milliwattsToWatts(value float64) float64 { return value / 1000 }

func wattsValue(value any) float64 {
	watts, ok := number(value)
	if !ok {
		return 0
	}
	return watts
}

func normalizeTelemetryPower(value any) float64 {
	watts, ok := number(value)
	if !ok {
		return 0
	}
	// Compatibility fallback for telemetry fields seen as milliwatts on some
	// releases and watts on others. Values above plausible Mac system power are
	// interpreted as milliwatts.
	if math.Abs(watts) > 500 {
		return watts / 1000
	}
	return watts
}

func maxNumber(values []float64) float64 {
	var maximum float64
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func decodeTemperature(value float64) float64 {
	if value == 0 {
		return 0
	}
	candidates := make([]float64, 0, 2)
	if value > 2000 {
		candidates = append(candidates, value/10-273.15, value/100)
	} else if value > 200 {
		candidates = append(candidates, value-273.15, value/10)
	} else {
		candidates = append(candidates, value)
	}
	best := 0.0
	distance := math.MaxFloat64
	for _, candidate := range candidates {
		if candidate < -20 || candidate > 85 {
			continue
		}
		currentDistance := math.Abs(candidate - 35)
		if currentDistance < distance {
			best = candidate
			distance = currentDistance
		}
	}
	return best
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	const maximum = 240
	if len(message) > maximum {
		return message[:maximum] + "…"
	}
	return message
}

// normalizePower converts powermetrics-style power fields that may be reported
// in milliwatts on some macOS versions. Source-specific battery and adapter
// fields use explicit unit conversion above and must not call this helper.
func normalizePower(value any) float64 {
	watts, ok := number(value)
	if !ok {
		return 0
	}
	if math.Abs(watts) > 1000 {
		return watts / 1000
	}
	return watts
}
