package benchmark

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Definition describes a user-facing benchmark preset.
//
// It is the single source of truth used by the CLI, local API, and SwiftUI app.
// Keeping presentation metadata beside the plan factory prevents UI descriptions
// from drifting away from the workload that actually runs.
type Definition struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Summary                string            `json:"summary"`
	Description            string            `json:"description"`
	Category               string            `json:"category"`
	Icon                   string            `json:"icon"`
	Intensity              string            `json:"intensity"`
	RequiredPowerSource    string            `json:"required_power_source,omitempty"`
	TypicalDurationSeconds float64           `json:"typical_duration_seconds"`
	AdjustableDuration     bool              `json:"adjustable_duration"`
	MinimumDurationSeconds float64           `json:"minimum_duration_seconds,omitempty"`
	MaximumDurationSeconds float64           `json:"maximum_duration_seconds,omitempty"`
	BestFor                []string          `json:"best_for"`
	Metrics                []string          `json:"metrics"`
	SafetyNotes            []string          `json:"safety_notes,omitempty"`
	Phases                 []DefinitionPhase `json:"phases"`
}

// DefinitionPhase is the human-readable description of one preset phase.
type DefinitionPhase struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Description     string   `json:"description"`
	DurationSeconds float64  `json:"duration_seconds"`
	Components      []string `json:"components,omitempty"`
	Profile         string   `json:"profile,omitempty"`
}

// CustomSpec defines a validated, arbitrary workload plan.
type CustomSpec struct {
	DisplayName         string
	RequiredPowerSource string
	CPU                 bool
	GPU                 bool
	Memory              bool
	GPUProfile          string
	MemoryMB            int
	WorkloadDuration    time.Duration
	BaselineDuration    time.Duration
	CooldownDuration    time.Duration
}

// Catalog returns the complete built-in benchmark catalog in display order.
func Catalog() []Definition {
	return []Definition{
		definition(
			"quick",
			"Quick diagnostic",
			"Fast health check across idle, CPU, GPU, memory, and mixed load.",
			"Use this first after installing MacPowerLab or updating macOS. It confirms every workload launches, sensors remain responsive, and the battery or charger handles short transitions.",
			"Diagnostic",
			"stethoscope",
			"Moderate",
			"",
			3*time.Minute+45*time.Second,
			false,
			[]string{"Post-update verification", "Fast hardware check", "Sensor compatibility"},
			[]string{"Idle power", "CPU/GPU response", "Memory bandwidth load", "Thermal transitions"},
			nil,
			QuickPlan(),
		),
		definition(
			"idle",
			"Idle baseline",
			"Measures quiet-system battery draw, background apps, and display overhead.",
			"Runs no synthetic workload. Close or leave apps open depending on whether you want a clean baseline or a real-world background-drain audit.",
			"Efficiency",
			"moon.zzz",
			"Light",
			"",
			5*time.Minute,
			true,
			[]string{"Background drain", "Display and WindowServer impact", "Before/after comparisons"},
			[]string{"Average idle watts", "Application Energy Impact", "Wakeups", "Projected runtime"},
			nil,
			IdlePlan(5*time.Minute),
		),
		definition(
			"cpu",
			"CPU sustained load",
			"Loads all CPU cores to measure sustained processor power and thermals.",
			"Best for comparing CPU efficiency across macOS builds, power modes, cooling conditions, or battery age.",
			"Component",
			"cpu",
			"High",
			"",
			5*time.Minute,
			true,
			[]string{"CPU efficiency", "Frequency stability", "Thermal throttling checks"},
			[]string{"CPU watts", "Cluster frequency/residency", "Battery draw", "Thermal pressure"},
			nil,
			CPUPlan(5*time.Minute),
		),
		definition(
			"gpu",
			"GPU sustained load",
			"Runs a high-intensity Metal compute workload.",
			"Measures Apple GPU power, frequency stability, battery draw, and the effect of WindowServer or other GPU-using applications.",
			"Component",
			"gpu",
			"High",
			"",
			5*time.Minute,
			true,
			[]string{"GPU efficiency", "Metal validation", "Graphics thermal behavior"},
			[]string{"GPU watts", "GPU MHz", "Battery draw", "Thermal pressure"},
			[]string{"Requires the Metal Toolchain and a Metal-capable Mac."},
			GPUPlan(5*time.Minute, "high"),
		),
		definition(
			"memory",
			"Memory bandwidth",
			"Continuously reads and writes a large in-memory working set.",
			"Useful for measuring unified-memory power, thermal behavior, and how memory-heavy apps affect runtime without writing benchmark data to the SSD.",
			"Component",
			"memorychip",
			"High",
			"",
			5*time.Minute,
			true,
			[]string{"Unified-memory load", "Memory efficiency", "No-SSD stress testing"},
			[]string{"System watts", "Memory allocation", "Battery draw", "Thermal trend"},
			nil,
			MemoryPlan(5*time.Minute),
		),
		definition(
			"mixed",
			"Mixed system load",
			"Runs CPU, high GPU, and memory workloads together.",
			"A realistic heavy-system test below the extreme GPU profile. It is useful for workstation-style sustained load and charger-headroom testing.",
			"System",
			"square.3.layers.3d",
			"Very high",
			"",
			5*time.Minute,
			true,
			[]string{"Workstation load", "Charger headroom", "Sustained mixed performance"},
			[]string{"Total system watts", "CPU/GPU split", "Battery assist", "Thermal stability"},
			nil,
			MixedPlan(5*time.Minute, "high"),
		),
		definition(
			"app-audit",
			"Application power audit",
			"Profiles the apps you normally use without adding synthetic load.",
			"Leave your normal apps running. MacPowerLab samples process coalitions, Energy Impact, CPU/GPU time, wakeups, disk, and network activity to rank likely power users.",
			"Applications",
			"list.bullet.rectangle",
			"Real-world",
			"",
			10*time.Minute,
			true,
			[]string{"Finding battery-draining apps", "Comparing workflows", "Background-service analysis"},
			[]string{"Estimated app watts", "Energy Impact", "CPU/GPU shares", "Wakeups and I/O"},
			[]string{"Per-app watts are attribution estimates, not direct electrical measurements."},
			AppAuditPlan(10*time.Minute),
		),
		definition(
			"battery",
			"Battery discharge suite",
			"Full unplugged suite: idle, CPU, GPU, memory, and extreme load.",
			"Produces the most complete battery benchmark and runtime projections. It requires Battery Power so the measured discharge watts represent real total system load.",
			"Battery",
			"battery.25percent",
			"Variable",
			"Battery Power",
			11*time.Minute,
			false,
			[]string{"Battery benchmark score", "Runtime projection", "Battery aging comparisons"},
			[]string{"Wh used", "Runtime by workload", "Peak draw", "Battery temperature", "Cell stability"},
			[]string{"Unplug the charger before starting.", "Starting around 60–90% gives clearer percentage changes than starting at 100%."},
			BatteryPlan(),
		),
		definition(
			"ac",
			"AC adapter / charging suite",
			"Tests charger output, battery charge acceptance, headroom, and battery assist.",
			"Runs the full workload suite while connected to AC. It shows whether the adapter keeps charging, reaches its estimated limit, or requires temporary battery assistance.",
			"Charging",
			"powerplug",
			"Variable",
			"AC Power",
			11*time.Minute,
			false,
			[]string{"Charger comparison", "Cable testing", "Battery-assist detection"},
			[]string{"Estimated adapter output", "Headroom", "Charge acceptance", "Battery assist"},
			[]string{"Exact wall power still requires a USB-C or wall power meter."},
			ACPlan(),
		),
		definition(
			"thermal",
			"Thermal stability",
			"Extreme mixed load followed by a monitored cooldown.",
			"Measures temperature rise, macOS thermal pressure, frequency stability, and recovery after load is removed.",
			"Thermal",
			"thermometer.medium",
			"Extreme",
			"",
			15*time.Minute,
			true,
			[]string{"Throttling investigation", "Cooling comparisons", "Recovery behavior"},
			[]string{"Thermal pressure", "Battery temperature slope", "Cluster MHz", "Cooldown rate"},
			[]string{"Keep vents clear and use the Mac on a hard surface."},
			ThermalPlan(10*time.Minute),
		),
		definition(
			"extreme",
			"Extreme soak",
			"Maximum sustained CPU, extreme GPU, and memory load.",
			"The heaviest single MacPowerLab workload. Use it for charger-limit testing, peak battery draw, thermal-soak behavior, and sustained system stability.",
			"System",
			"flame.fill",
			"Maximum",
			"",
			15*time.Minute,
			true,
			[]string{"Maximum load", "Peak battery draw", "Long-duration stability"},
			[]string{"Peak and average watts", "CPU/GPU saturation", "Thermal pressure", "Battery assist"},
			[]string{"Keep vents clear.", "Stop the test if the Mac is physically obstructed or operating abnormally."},
			ExtremePlan(15*time.Minute),
		),
	}
}

func definition(
	id, name, summary, description, category, icon, intensity, requiredPower string,
	typical time.Duration,
	adjustable bool,
	bestFor, metrics, safety []string,
	plan Plan,
) Definition {
	minimum := 0.0
	maximum := 0.0
	if adjustable {
		minimum = time.Minute.Seconds()
		maximum = (2 * time.Hour).Seconds()
	}
	phases := make([]DefinitionPhase, 0, len(plan.Phases))
	for _, phase := range plan.Phases {
		phases = append(phases, DefinitionPhase{
			Name:            phase.Name,
			Kind:            phase.Kind,
			Description:     phaseDescription(phase),
			DurationSeconds: phase.Duration.Seconds(),
			Components:      append([]string(nil), phase.Components...),
			Profile:         phase.Profile,
		})
	}
	return Definition{
		ID:                     id,
		Name:                   name,
		Summary:                summary,
		Description:            description,
		Category:               category,
		Icon:                   icon,
		Intensity:              intensity,
		RequiredPowerSource:    requiredPower,
		TypicalDurationSeconds: typical.Seconds(),
		AdjustableDuration:     adjustable,
		MinimumDurationSeconds: minimum,
		MaximumDurationSeconds: maximum,
		BestFor:                bestFor,
		Metrics:                metrics,
		SafetyNotes:            safety,
		Phases:                 phases,
	}
}

func phaseDescription(phase Phase) string {
	switch phase.Kind {
	case "idle":
		return "No synthetic workload; monitor background and platform power."
	case "cpu":
		return "Loads all CPU cores continuously."
	case "gpu":
		return "Runs a Metal compute workload."
	case "memory":
		return "Exercises unified-memory bandwidth without SSD writes."
	case "combined", "extreme":
		return "Runs selected CPU, GPU, and memory workloads concurrently."
	default:
		return "Collects power and performance data."
	}
}

// PlanByID builds a validated preset and applies a supported duration override.
func PlanByID(id string, duration time.Duration) (Plan, error) {
	switch id {
	case "quick":
		return QuickPlan(), nil
	case "idle":
		return IdlePlan(defaultDuration(duration, 5*time.Minute)), nil
	case "cpu":
		return CPUPlan(defaultDuration(duration, 5*time.Minute)), nil
	case "gpu":
		return GPUPlan(defaultDuration(duration, 5*time.Minute), "high"), nil
	case "memory":
		return MemoryPlan(defaultDuration(duration, 5*time.Minute)), nil
	case "mixed":
		return MixedPlan(defaultDuration(duration, 5*time.Minute), "high"), nil
	case "app-audit":
		return AppAuditPlan(defaultDuration(duration, 10*time.Minute)), nil
	case "battery":
		return BatteryPlan(), nil
	case "ac":
		return ACPlan(), nil
	case "thermal":
		return ThermalPlan(defaultDuration(duration, 10*time.Minute)), nil
	case "extreme":
		return ExtremePlan(defaultDuration(duration, 15*time.Minute)), nil
	default:
		return Plan{}, fmt.Errorf("unknown benchmark %q", id)
	}
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func QuickPlan() Plan {
	return Plan{
		Name: "quick",
		Phases: []Phase{
			{Name: "Idle baseline", Kind: "idle", Duration: 30 * time.Second},
			{Name: "CPU check", Kind: "cpu", Duration: 45 * time.Second},
			{Name: "GPU check", Kind: "gpu", Profile: "high", Duration: 45 * time.Second},
			{Name: "Memory check", Kind: "memory", Duration: 45 * time.Second},
			{Name: "Mixed load check", Kind: "combined", Profile: "high", Duration: 60 * time.Second, Components: []string{"cpu", "gpu", "memory"}},
		},
	}
}

func IdlePlan(duration time.Duration) Plan {
	return Plan{Name: "idle", Phases: []Phase{{Name: "Idle baseline", Kind: "idle", Duration: duration}}}
}

func CPUPlan(duration time.Duration) Plan {
	return Plan{Name: "cpu", Phases: []Phase{{Name: "CPU sustained load", Kind: "cpu", Duration: duration}}}
}

func GPUPlan(duration time.Duration, profile string) Plan {
	return Plan{Name: "gpu", Phases: []Phase{{Name: "GPU sustained load", Kind: "gpu", Profile: profile, Duration: duration}}}
}

func MemoryPlan(duration time.Duration) Plan {
	return Plan{Name: "memory", Phases: []Phase{{Name: "Memory bandwidth", Kind: "memory", Duration: duration}}}
}

func MixedPlan(duration time.Duration, profile string) Plan {
	return Plan{
		Name: "mixed",
		Phases: []Phase{{
			Name:       "Mixed CPU + GPU + memory",
			Kind:       "combined",
			Profile:    profile,
			Duration:   duration,
			Components: []string{"cpu", "gpu", "memory"},
		}},
	}
}

func AppAuditPlan(duration time.Duration) Plan {
	return Plan{Name: "app-audit", Phases: []Phase{{Name: "Application power audit", Kind: "idle", Duration: duration}}}
}

func ThermalPlan(stressDuration time.Duration) Plan {
	if stressDuration <= 0 {
		stressDuration = 10 * time.Minute
	}
	cooldown := stressDuration / 2
	if cooldown < 2*time.Minute {
		cooldown = 2 * time.Minute
	}
	if cooldown > 10*time.Minute {
		cooldown = 10 * time.Minute
	}
	return Plan{
		Name: "thermal",
		Phases: []Phase{
			{Name: "Thermal ramp", Kind: "extreme", Profile: "extreme", Duration: stressDuration, Components: []string{"cpu", "gpu", "memory"}},
			{Name: "Monitored cooldown", Kind: "idle", Duration: cooldown},
		},
	}
}

// CustomPlan validates and constructs a user-selected benchmark.
func CustomPlan(spec CustomSpec) (Plan, error) {
	name := strings.TrimSpace(spec.DisplayName)
	if name == "" {
		name = "Custom workload"
	}
	if len(name) > 80 {
		return Plan{}, errors.New("custom benchmark name must be 80 characters or fewer")
	}
	if spec.WorkloadDuration < time.Second || spec.WorkloadDuration > 24*time.Hour {
		return Plan{}, errors.New("custom workload duration must be between 1 second and 24 hours")
	}
	if spec.BaselineDuration < 0 || spec.BaselineDuration > time.Hour {
		return Plan{}, errors.New("custom baseline duration must be between 0 and 1 hour")
	}
	if spec.CooldownDuration < 0 || spec.CooldownDuration > time.Hour {
		return Plan{}, errors.New("custom cooldown duration must be between 0 and 1 hour")
	}
	if spec.MemoryMB < 0 || spec.MemoryMB > 262144 {
		return Plan{}, errors.New("custom memory allocation must be between 0 and 262144 MB")
	}
	profile := spec.GPUProfile
	if profile == "" {
		profile = "high"
	}
	if profile != "normal" && profile != "high" && profile != "extreme" {
		return Plan{}, errors.New("GPU profile must be normal, high, or extreme")
	}

	required := ""
	switch strings.ToLower(strings.TrimSpace(spec.RequiredPowerSource)) {
	case "", "any":
	case "battery", "battery power":
		required = "Battery Power"
	case "ac", "ac power":
		required = "AC Power"
	default:
		return Plan{}, errors.New("required power source must be any, battery, or ac")
	}

	components := make([]string, 0, 3)
	if spec.CPU {
		components = append(components, "cpu")
	}
	if spec.GPU {
		components = append(components, "gpu")
	}
	if spec.Memory {
		components = append(components, "memory")
	}
	if len(components) == 0 {
		return Plan{}, errors.New("custom benchmark must select at least one workload")
	}

	kind := components[0]
	if len(components) > 1 {
		kind = "combined"
	}

	phases := make([]Phase, 0, 3)
	if spec.BaselineDuration > 0 {
		phases = append(phases, Phase{Name: "Custom idle baseline", Kind: "idle", Duration: spec.BaselineDuration})
	}
	phases = append(phases, Phase{
		Name:       name,
		Kind:       kind,
		Profile:    profile,
		Duration:   spec.WorkloadDuration,
		MemoryMB:   spec.MemoryMB,
		Components: components,
	})
	if spec.CooldownDuration > 0 {
		phases = append(phases, Phase{Name: "Custom cooldown", Kind: "idle", Duration: spec.CooldownDuration})
	}
	return Plan{Name: "custom", RequiredPowerSource: required, Phases: phases}, nil
}
