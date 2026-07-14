// Package tui renders the production terminal dashboard without scrolling.
package tui

import (
	"context"
	"fmt"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/benchmark"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/collector"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/version"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

type Renderer struct {
	out   io.Writer
	color bool
}

func New(out io.Writer, color bool) *Renderer {
	if out == nil {
		out = os.Stdout
	}
	return &Renderer{out: out, color: color}
}
func (r *Renderer) c(code, s string) string {
	if !r.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func (r *Renderer) clear() { fmt.Fprint(r.out, "\x1b[2J\x1b[H\x1b[?25l") }
func (r *Renderer) show()  { fmt.Fprint(r.out, "\x1b[?25h") }

// Run renders manager samples until ctx cancellation.
func Run(ctx context.Context, m *collector.Manager, b *benchmark.Controller, color bool) error {
	r := New(os.Stdout, color)
	defer r.show()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last *model.PowerSample
	var progress model.BenchmarkProgress
	for {
		select {
		case <-ctx.Done():
			return nil
		case s, ok := <-m.Samples():
			if !ok {
				return nil
			}
			v := s
			last = &v
			progressValue := progress
			if b != nil {
				progressValue = b.Progress()
			}
			r.render(last, progressValue)
		case p := <-benchmarkUpdates(b):
			progress = p
			r.render(last, p)
		case <-ticker.C:
			if last != nil {
				if b != nil {
					progress = b.Progress()
				}
				r.render(last, progress)
			}
		}
	}
}
func benchmarkUpdates(b *benchmark.Controller) <-chan model.BenchmarkProgress {
	if b == nil {
		return nil
	}
	return b.Updates()
}
func (r *Renderer) render(s *model.PowerSample, p model.BenchmarkProgress) {
	r.clear()
	fmt.Fprintf(r.out, "%s\n", r.c("1;37;44", fmt.Sprintf(" MacPowerLab v%s — Go engine ", version.Version)))
	if s == nil {
		fmt.Fprintln(r.out, "\nWaiting for first sensor sample…")
		return
	}
	fmt.Fprintf(r.out, "Session %-18s  %s  phase: %s\n\n", s.SessionID, s.Timestamp.Format("15:04:05"), empty(s.Phase, "idle / unmarked"))
	fmt.Fprintf(r.out, "%s\n", r.c("1;96", "POWER / BATTERY / CHARGER"))
	fmt.Fprintf(r.out, "Power source       %-18s  state %-12s  battery %6.1f%%\n", s.Battery.PowerSource, s.Battery.State, s.Battery.Percent)
	fmt.Fprintf(r.out, "Primary load       %8.2f W  via %s\n", s.PrimaryLoadW, s.PrimaryLoadSource)
	fmt.Fprintf(r.out, "Battery flow       %8.2f W  %.3f V / %.3f A\n", s.Battery.NetWatts, s.Battery.VoltageV, s.Battery.CurrentA)
	fmt.Fprintf(r.out, "Battery temp       %8.2f °C  trend %+5.2f °C/min  cells Δ %.0f mV\n", s.Battery.TemperatureC, s.Thermal.BatteryTrendCPerMin, s.Battery.CellVoltageDeltaMV)
	fmt.Fprintf(r.out, "Adapter            %8.0f W  est output %7.2f W  load %5.1f%%  headroom %6.2f W\n", s.Adapter.RatedWatts, s.Adapter.OutputEstimateW, s.Adapter.LoadPercent, s.Adapter.HeadroomW)
	fmt.Fprintf(r.out, "Estimate source    %s\n\n", empty(s.Adapter.OutputEstimateSource, "n/a"))
	fmt.Fprintf(r.out, "%s\n", r.c("1;96", "SOC / THERMAL"))
	fmt.Fprintf(r.out, "CPU/GPU/ANE/DRAM   %6.2f / %6.2f / %6.2f / %6.2f W\n", s.Components.CPUWatts, s.Components.GPUWatts, s.Components.ANEWatts, s.Components.DRAMWatts)
	fmt.Fprintf(r.out, "CPU est / GPU      %7.0f MHz / %7.0f MHz  thermal %s\n", s.Components.CPUEstimateMHz, s.Components.GPUMHz, empty(s.Thermal.Summary, "n/a"))
	if len(s.Components.Clusters) > 0 {
		fmt.Fprint(r.out, "Clusters            ")
		for i, c := range s.Components.Clusters {
			if i > 0 {
				fmt.Fprint(r.out, " | ")
			}
			fmt.Fprintf(r.out, "%s %.0fMHz/%.0f%%", c.Name, c.FrequencyMHz, c.ActivePercent)
		}
		fmt.Fprintln(r.out)
	}
	fmt.Fprintln(r.out)
	fmt.Fprintf(r.out, "%s\n", r.c("1;96", "APPLICATION POWER ATTRIBUTION"))
	fmt.Fprintf(r.out, "Model/status       %s / confidence %s  baseline %.2f W / dynamic %.2f W\n", empty(s.Attribution.Method, "unavailable"), empty(s.Attribution.Confidence, "low"), s.Attribution.BaselineWatts, s.Attribution.DynamicWatts)
	fmt.Fprintf(r.out, "%-3s %-25s %9s %9s %9s %8s %s\n", "#", "Application", "Total W", "Dynamic W", "Energy Wh", "Impact", "Confidence")
	apps := append([]model.AppPower(nil), s.Attribution.Apps...)
	sort.Slice(apps, func(i, j int) bool { return apps[i].EstimatedDynamicW > apps[j].EstimatedDynamicW })
	for i, a := range apps {
		if i >= 10 {
			break
		}
		fmt.Fprintf(r.out, "%-3d %-25.25s %9.2f %9.2f %9.4f %8.2f %s\n", i+1, a.Name, a.EstimatedShareW, a.EstimatedDynamicW, a.EstimatedEnergyWh, a.EnergyImpact, a.Confidence)
	}
	if len(apps) == 0 {
		fmt.Fprintln(r.out, "No task/coalition activity available yet.")
	}
	if p.Running {
		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, "%s\n", r.c("1;95", "BENCHMARK"))
		fmt.Fprintf(r.out, "%s (%d/%d)  [%s] %5.1f%%  elapsed %s  remaining %s\n", p.Phase, p.PhaseIndex, p.PhaseCount, bar(p.Percent, 30), p.Percent, duration(p.Elapsed), duration(p.Remaining))
	}
	if len(s.Warnings) > 0 {
		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, "%s %s\n", r.c("1;93", "Warnings:"), strings.Join(s.Warnings, "; "))
	}
	fmt.Fprintln(r.out, "\nCtrl+C stops safely. Logs are written continuously.")
}
func empty(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
func duration(s float64) string {
	d := time.Duration(s * float64(time.Second))
	return d.Round(time.Second).String()
}
func bar(p float64, n int) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	f := int(p / 100 * float64(n))
	return strings.Repeat("█", f) + strings.Repeat("░", n-f)
}
