package attribution

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/model"
)

func TestAttributionInvariant(t *testing.T) {
	a := New(10)
	activities := []model.ProcessActivity{
		{Key: "coalition:1", Name: "Safari", Source: "powermetrics-coalition", CPUTimeMSPerS: 700, GPUTimeMSPerS: 100, EnergyImpactPerS: 20},
		{Key: "coalition:2", Name: "WindowServer", Source: "powermetrics-coalition", CPUTimeMSPerS: 300, GPUTimeMSPerS: 200, EnergyImpactPerS: 10},
	}
	now := time.Now()
	_ = a.Observe(now, 15, "Battery Power", "battery discharge watts", model.ComponentSample{}, activities)
	result := a.Observe(now.Add(time.Second), 60, "Battery Power", "battery discharge watts", model.ComponentSample{CPUWatts: 20, GPUWatts: 10}, activities)
	var total float64
	for _, app := range result.Apps {
		if math.IsNaN(app.EstimatedShareW) || app.EstimatedShareW < 0 {
			t.Fatalf("bad app=%+v", app)
		}
		total += app.EstimatedShareW
	}
	if math.Abs(total-60) > 0.01 {
		t.Fatalf("allocated %.4f W, want 60 W", total)
	}
	if result.Confidence != "high" {
		t.Fatalf("confidence=%s", result.Confidence)
	}
}

func TestZeroMetricDoesNotReceiveNonZeroPoolShare(t *testing.T) {
	a := New(10)
	now := time.Now()
	activities := []model.ProcessActivity{
		{Key: "busy", Name: "Busy", Source: "powermetrics-coalition", CPUTimeMSPerS: 1000, EnergyImpactPerS: 10},
		{Key: "idle", Name: "Idle", Source: "powermetrics-coalition", CPUTimeMSPerS: 0, EnergyImpactPerS: 1},
	}
	for i := 0; i < baselineSampleLimit; i++ {
		_ = a.Observe(now.Add(time.Duration(i)*time.Second), 10, "Battery Power", "battery discharge watts", model.ComponentSample{}, activities)
	}
	result := a.Observe(now.Add(time.Minute), 60, "Battery Power", "battery discharge watts", model.ComponentSample{CPUWatts: 30}, activities)
	var total float64
	for _, app := range result.Apps {
		total += app.EstimatedShareW
		if app.Key == "idle" && app.EstimatedCPUW != 0 {
			t.Fatalf("idle app received CPU watts: %+v", app)
		}
	}
	if math.Abs(total-60) > 0.001 {
		t.Fatalf("allocated %.4f W, want 60 W", total)
	}
}

func TestBaselineDoesNotDriftUpDuringLongStress(t *testing.T) {
	a := New(5)
	now := time.Now()
	activity := []model.ProcessActivity{{Key: "app", Name: "App", CPUTimeMSPerS: 1000, Source: "powermetrics-task"}}
	for i := 0; i < baselineSampleLimit; i++ {
		_ = a.Observe(now.Add(time.Duration(i)*time.Second), 12, "Battery Power", "battery discharge watts", model.ComponentSample{}, activity)
	}
	var result model.AttributionResult
	for i := 0; i < baselineSampleLimit*10; i++ {
		result = a.Observe(now.Add(time.Minute+time.Duration(i)*time.Second), 100, "Battery Power", "battery discharge watts", model.ComponentSample{CPUWatts: 40}, activity)
	}
	if math.Abs(result.BaselineWatts-12) > 0.001 {
		t.Fatalf("baseline drifted to %.2f W", result.BaselineWatts)
	}
	if result.DynamicWatts < 87.9 {
		t.Fatalf("dynamic load collapsed: %.2f W", result.DynamicWatts)
	}
}

func TestEnergyMapIsStrictlyBounded(t *testing.T) {
	a := New(10)
	now := time.Now()
	activities := make([]model.ProcessActivity, 0, energyEntryLimit+100)
	for i := 0; i < energyEntryLimit+100; i++ {
		activities = append(activities, model.ProcessActivity{
			Key:           fmt.Sprintf("pid:%d", i),
			Name:          "Process",
			CPUTimeMSPerS: 1,
			Source:        "powermetrics-task",
		})
	}
	_ = a.Observe(now, 100, "Battery Power", "battery discharge watts", model.ComponentSample{}, activities)
	if len(a.energy) > energyEntryLimit {
		t.Fatalf("energy entries=%d, limit=%d", len(a.energy), energyEntryLimit)
	}
}

func BenchmarkAttributorObserve200Processes(b *testing.B) {
	activities := make([]model.ProcessActivity, 200)
	for i := range activities {
		activities[i] = model.ProcessActivity{
			Key:                   fmt.Sprintf("coalition:%d", i),
			Name:                  "Process",
			Source:                "powermetrics-coalition",
			CPUTimeMSPerS:         float64(i%20 + 1),
			GPUTimeMSPerS:         float64(i % 5),
			EnergyImpactPerS:      float64(i%11 + 1),
			InterruptWakeupsPerS:  float64(i % 7),
			DiskReadBytesPerS:     float64(i * 100),
			NetworkWriteBytesPerS: float64(i * 50),
		}
	}
	attributor := New(20)
	components := model.ComponentSample{CPUWatts: 25, GPUWatts: 15}
	start := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		attributor.Observe(
			start.Add(time.Duration(i)*time.Second),
			80,
			"Battery Power",
			"battery discharge watts",
			components,
			activities,
		)
	}
}

func TestComponentPoolsScaleProportionallyToDynamicPower(t *testing.T) {
	a := New(10)
	now := time.Now()
	activities := []model.ProcessActivity{
		{Key: "app", Name: "App", Source: "powermetrics-coalition", CPUTimeMSPerS: 100, GPUTimeMSPerS: 100},
	}
	for i := 0; i < baselineSampleLimit; i++ {
		_ = a.Observe(now.Add(time.Duration(i)*time.Second), 10, "Battery Power", "battery discharge watts", model.ComponentSample{}, activities)
	}
	result := a.Observe(now.Add(time.Minute), 50, "Battery Power", "battery discharge watts", model.ComponentSample{CPUWatts: 30, GPUWatts: 30}, activities)
	if math.Abs(result.CPUComponentPoolW-20) > 0.001 || math.Abs(result.GPUComponentPoolW-20) > 0.001 {
		t.Fatalf("pools cpu=%.2f gpu=%.2f want 20/20", result.CPUComponentPoolW, result.GPUComponentPoolW)
	}
	if math.Abs(result.CPUComponentPoolW+result.GPUComponentPoolW+result.ResidualPoolW-result.DynamicWatts) > 0.001 {
		t.Fatalf("component pools do not conserve dynamic watts: %+v", result)
	}
}
