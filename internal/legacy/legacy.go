// Package legacy imports v0.x CSV and invokes compatibility scripts.
package legacy

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/execx"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReadLatestCSVRow imports the final row of a legacy monitor CSV.
func ReadLatestCSVRow(path string) (model.PowerSample, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.PowerSample{}, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	headers, err := r.Read()
	if err != nil {
		return model.PowerSample{}, err
	}
	var row []string
	for {
		v, e := r.Read()
		if e != nil {
			break
		}
		row = v
	}
	if len(row) != len(headers) {
		return model.PowerSample{}, errors.New("legacy CSV has no complete data row")
	}
	m := map[string]string{}
	for i, k := range headers {
		m[k] = row[i]
	}
	num := func(keys ...string) float64 {
		for _, k := range keys {
			if s := m[k]; s != "" {
				v, _ := strconv.ParseFloat(s, 64)
				return v
			}
		}
		return 0
	}
	ts, _ := time.Parse(time.RFC3339, m["timestamp"])
	return model.PowerSample{Schema: version.PowerSampleSchema, Timestamp: ts, Battery: model.BatterySample{Percent: num("battery_percent"), PowerSource: m["power_source"], VoltageV: num("battery_voltage_v"), CurrentA: num("battery_current_a"), NetWatts: num("net_battery_watts"), TemperatureC: num("battery_temp_c")}, Adapter: model.AdapterSample{RatedWatts: num("adapter_reported_watts")}, Components: model.ComponentSample{CPUWatts: num("cpu_power_w"), GPUWatts: num("gpu_power_w")}, PrimaryLoadW: num("primary_total_load_w", "whole_mac_watts_estimate")}, nil
}

// RunScript executes one legacy script without a shell.
func RunScript(ctx context.Context, legacyDir, script string, args ...string) error {
	path := filepath.Join(legacyDir, filepath.Base(script))
	if _, err := os.Stat(path); err != nil {
		return err
	}
	all := append([]string{path}, args...)
	return execx.RunInteractive(ctx, "/bin/zsh", all...)
}
func Python() string {
	if p, e := exec.LookPath("python3"); e == nil {
		return p
	}
	return "python3"
}
func Explain() string {
	return fmt.Sprintf("legacy compatibility uses %s and v0.x scripts", strings.TrimSpace(Python()))
}
