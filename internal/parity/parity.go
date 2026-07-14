// Package parity compares the Go collector with the frozen legacy collector.
package parity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/collector"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/legacy"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
)

var tolerances = map[string]float64{
	"battery_percent":   2,
	"net_battery_watts": 8,
	"battery_voltage_v": 1,
	"battery_current_a": 1,
	"battery_temp_c":    3,
	"adapter_watts":     15,
	"cpu_power_w":       12,
	"gpu_power_w":       12,
}

// Run executes live parity checks and writes a stable JSON report.
func Run(ctx context.Context, legacyDir, out string, iterations int) (model.ParityReport, error) {
	if ctx == nil {
		return model.ParityReport{}, errors.New("parity context must not be nil")
	}
	if iterations < 1 {
		iterations = 1
	}
	report := model.ParityReport{
		Schema:     version.ParitySchema,
		CreatedAt:  time.Now(),
		Iterations: iterations,
		Passed:     true,
	}

	for i := 0; i < iterations; i++ {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		goSample, goErr, oldSample, legacyErr := collectPair(
			ctx,
			collector.CollectOnce,
			func(pairCtx context.Context) (model.PowerSample, error) {
				return collectLegacySample(pairCtx, legacyDir)
			},
		)
		if goErr != nil {
			report.Errors = append(report.Errors, "go collector: "+goErr.Error())
			report.Passed = false
		}
		if legacyErr != nil {
			report.Errors = append(report.Errors, "legacy collector: "+legacyErr.Error())
			report.Passed = false
		}
		if goErr == nil && legacyErr == nil {
			differences, passed := compareSamples(goSample, oldSample)
			report.Differences = append(report.Differences, differences...)
			report.Passed = report.Passed && passed
		}

		if i+1 < iterations {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return report, ctx.Err()
			case <-timer.C:
			}
		}
	}

	if out != "" {
		if err := writeAtomicJSON(out, report); err != nil {
			return report, fmt.Errorf("write parity report: %w", err)
		}
	}
	return report, nil
}

type sampleCollector func(context.Context) (model.PowerSample, error)

func collectPair(
	ctx context.Context,
	goCollector sampleCollector,
	legacyCollector sampleCollector,
) (model.PowerSample, error, model.PowerSample, error) {
	type result struct {
		sample model.PowerSample
		err    error
	}
	goResult := make(chan result, 1)
	legacyResult := make(chan result, 1)
	go func() {
		sample, err := goCollector(ctx)
		goResult <- result{sample: sample, err: err}
	}()
	go func() {
		sample, err := legacyCollector(ctx)
		legacyResult <- result{sample: sample, err: err}
	}()
	left := <-goResult
	right := <-legacyResult
	return left.sample, left.err, right.sample, right.err
}

func collectLegacySample(ctx context.Context, legacyDir string) (model.PowerSample, error) {
	if legacyDir == "" {
		return model.PowerSample{}, errors.New("legacy directory must not be empty")
	}
	tmp, err := os.CreateTemp("", "macpower_legacy_*.csv")
	if err != nil {
		return model.PowerSample{}, fmt.Errorf("create legacy output: %w", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return model.PowerSample{}, fmt.Errorf("close legacy output: %w", err)
	}
	defer os.Remove(path)

	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err = execx.Run(
		commandCtx,
		8<<20,
		legacy.Python(),
		filepath.Join(legacyDir, "mac_power_watch.py"),
		"--once",
		"--plain",
		"--powermetrics",
		"--powermetrics-every", "1",
		"--powermetrics-sample-ms", "1000",
		"--no-app-power",
		"--no-debug-json",
		"--log", path,
	)
	if err != nil {
		return model.PowerSample{}, err
	}
	old, err := legacy.ReadLatestCSVRow(path)
	if err != nil {
		return model.PowerSample{}, fmt.Errorf("read legacy CSV: %w", err)
	}
	return old, nil
}

func compareSamples(current, legacySample model.PowerSample) ([]model.ParityDifference, bool) {
	pairs := []struct {
		name string
		a    float64
		b    float64
	}{
		{"battery_percent", current.Battery.Percent, legacySample.Battery.Percent},
		{"net_battery_watts", current.Battery.NetWatts, legacySample.Battery.NetWatts},
		{"battery_voltage_v", current.Battery.VoltageV, legacySample.Battery.VoltageV},
		{"battery_current_a", current.Battery.CurrentA, legacySample.Battery.CurrentA},
		{"battery_temp_c", current.Battery.TemperatureC, legacySample.Battery.TemperatureC},
		{"adapter_watts", current.Adapter.RatedWatts, legacySample.Adapter.RatedWatts},
		{"cpu_power_w", current.Components.CPUWatts, legacySample.Components.CPUWatts},
		{"gpu_power_w", current.Components.GPUWatts, legacySample.Components.GPUWatts},
	}
	differences := make([]model.ParityDifference, 0, len(pairs))
	passed := true
	for _, pair := range pairs {
		tolerance := tolerances[pair.name]
		difference := math.Abs(pair.a - pair.b)
		pass := difference <= tolerance
		differences = append(differences, model.ParityDifference{
			Field:        pair.name,
			GoValue:      pair.a,
			LegacyValue:  pair.b,
			AbsoluteDiff: difference,
			Tolerance:    tolerance,
			Pass:         pass,
		})
		passed = passed && pass
	}
	return differences, passed
}

func writeAtomicJSON(path string, value any) error {
	if path == "" {
		return errors.New("output path must not be empty")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".parity-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
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
