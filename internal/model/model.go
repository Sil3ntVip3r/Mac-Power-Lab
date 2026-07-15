// Package model defines versioned, stable data contracts for MacPowerLab.
package model

import "time"

// ClusterSample represents one Apple Silicon CPU cluster's activity.
type ClusterSample struct {
	Name          string  `json:"name"`
	FrequencyMHz  float64 `json:"frequency_mhz,omitempty"`
	ActivePercent float64 `json:"active_percent,omitempty"`
	PowerWatts    float64 `json:"power_w,omitempty"`
}

// BatterySample contains electrical, capacity, temperature, and cell-health data.
type BatterySample struct {
	Percent                   float64   `json:"percent,omitempty"`
	PowerSource               string    `json:"power_source,omitempty"`
	State                     string    `json:"state,omitempty"`
	ExternalConnected         bool      `json:"external_connected"`
	Charging                  bool      `json:"charging"`
	VoltageV                  float64   `json:"voltage_v,omitempty"`
	CurrentA                  float64   `json:"current_a,omitempty"`
	NetWatts                  float64   `json:"net_w,omitempty"`
	TemperatureC              float64   `json:"temperature_c,omitempty"`
	TemperatureF              float64   `json:"temperature_f,omitempty"`
	TemperatureRaw            float64   `json:"temperature_raw,omitempty"`
	VirtualTemperatureC       float64   `json:"virtual_temperature_c,omitempty"`
	CycleCount                int       `json:"cycle_count,omitempty"`
	CurrentCapacityMAh        float64   `json:"current_capacity_mah,omitempty"`
	FullChargeCapacityMAh     float64   `json:"full_charge_capacity_mah,omitempty"`
	DesignCapacityMAh         float64   `json:"design_capacity_mah,omitempty"`
	HealthPercent             float64   `json:"health_percent,omitempty"`
	EstimatedRemainingWh      float64   `json:"estimated_remaining_wh,omitempty"`
	EstimatedFullWh           float64   `json:"estimated_full_wh,omitempty"`
	CellVoltageMinMV          float64   `json:"cell_voltage_min_mv,omitempty"`
	CellVoltageMaxMV          float64   `json:"cell_voltage_max_mv,omitempty"`
	CellVoltageDeltaMV        float64   `json:"cell_voltage_delta_mv,omitempty"`
	QMaxDelta                 float64   `json:"qmax_delta,omitempty"`
	WeightedRADelta           float64   `json:"weighted_ra_delta,omitempty"`
	CellDisconnectCount       int       `json:"cell_disconnect_count,omitempty"`
	TimeToEmptyMinutes        float64   `json:"time_to_empty_minutes,omitempty"`
	TimeToFullMinutes         float64   `json:"time_to_full_minutes,omitempty"`
	BMSSystemPowerW           float64   `json:"bms_system_power_w,omitempty"`
	SystemEffectiveTotalLoadW float64   `json:"system_effective_total_load_w,omitempty"`
	PowerDistributionInputW   float64   `json:"power_distribution_input_w,omitempty"`
	CollectedAt               time.Time `json:"collected_at"`
}

// AdapterSample describes the USB-C/charger contract and estimated live output.
type AdapterSample struct {
	Name                    string  `json:"name,omitempty"`
	Connected               bool    `json:"connected"`
	RatedWatts              float64 `json:"rated_w,omitempty"`
	ContractVoltageV        float64 `json:"contract_voltage_v,omitempty"`
	ContractCurrentA        float64 `json:"contract_current_a,omitempty"`
	ContractWatts           float64 `json:"contract_w,omitempty"`
	OutputEstimateW         float64 `json:"output_estimate_w,omitempty"`
	OutputEstimateSource    string  `json:"output_estimate_source,omitempty"`
	LoadPercent             float64 `json:"load_percent,omitempty"`
	HeadroomW               float64 `json:"headroom_w,omitempty"`
	BatteryAssistW          float64 `json:"battery_assist_w,omitempty"`
	PortControllerMaxPowerW float64 `json:"port_controller_max_power_w,omitempty"`
}

// ComponentSample contains SoC subsystem estimates and cluster activity.
type ComponentSample struct {
	CPUWatts          float64         `json:"cpu_w,omitempty"`
	GPUWatts          float64         `json:"gpu_w,omitempty"`
	ANEWatts          float64         `json:"ane_w,omitempty"`
	DRAMWatts         float64         `json:"dram_w,omitempty"`
	PackageWatts      float64         `json:"package_w,omitempty"`
	GPUMHz            float64         `json:"gpu_mhz,omitempty"`
	CPUEstimateMHz    float64         `json:"cpu_estimate_mhz,omitempty"`
	CPUEstimateSource string          `json:"cpu_estimate_source,omitempty"`
	Clusters          []ClusterSample `json:"clusters,omitempty"`
}

// ThermalSample keeps OS pressure and battery-trend signals distinct.
type ThermalSample struct {
	MacOSPressure       string  `json:"macos_pressure,omitempty"`
	BatteryState        string  `json:"battery_state,omitempty"`
	BatteryTrendCPerMin float64 `json:"battery_trend_c_per_min,omitempty"`
	Summary             string  `json:"summary,omitempty"`
	Source              string  `json:"source,omitempty"`
}

// ProcessActivity is one task or resource-coalition activity sample.
type ProcessActivity struct {
	Key                   string  `json:"key"`
	Name                  string  `json:"name"`
	DisplayName           string  `json:"display_name,omitempty"`
	Category              string  `json:"category,omitempty"`
	PID                   int     `json:"pid,omitempty"`
	ResponsiblePID        int     `json:"responsible_pid,omitempty"`
	CoalitionID           int64   `json:"coalition_id,omitempty"`
	EnergyImpact          float64 `json:"energy_impact,omitempty"`
	EnergyImpactPerS      float64 `json:"energy_impact_per_s,omitempty"`
	CPUTimeMSPerS         float64 `json:"cpu_time_ms_per_s,omitempty"`
	GPUTimeMSPerS         float64 `json:"gpu_time_ms_per_s,omitempty"`
	InterruptWakeupsPerS  float64 `json:"interrupt_wakeups_per_s,omitempty"`
	IdleWakeupsPerS       float64 `json:"idle_wakeups_per_s,omitempty"`
	DiskReadBytesPerS     float64 `json:"disk_read_bytes_per_s,omitempty"`
	DiskWriteBytesPerS    float64 `json:"disk_write_bytes_per_s,omitempty"`
	NetworkReadBytesPerS  float64 `json:"network_read_bytes_per_s,omitempty"`
	NetworkWriteBytesPerS float64 `json:"network_write_bytes_per_s,omitempty"`
	RSSBytes              int64   `json:"rss_bytes,omitempty"`
	Source                string  `json:"source,omitempty"`
}

// AppPower is a confidence-labelled attribution estimate, not direct metering.
type AppPower struct {
	Schema                string    `json:"schema"`
	Timestamp             time.Time `json:"timestamp"`
	Key                   string    `json:"key"`
	Name                  string    `json:"name"`
	Category              string    `json:"category,omitempty"`
	PID                   int       `json:"pid,omitempty"`
	ResponsiblePID        int       `json:"responsible_pid,omitempty"`
	CoalitionID           int64     `json:"coalition_id,omitempty"`
	EstimatedShareW       float64   `json:"estimated_share_w,omitempty"`
	EstimatedDynamicW     float64   `json:"estimated_dynamic_w,omitempty"`
	EstimatedCPUW         float64   `json:"estimated_cpu_w,omitempty"`
	EstimatedGPUW         float64   `json:"estimated_gpu_w,omitempty"`
	EstimatedResidualW    float64   `json:"estimated_residual_w,omitempty"`
	EstimatedEnergyWh     float64   `json:"estimated_energy_wh,omitempty"`
	EnergySharePercent    float64   `json:"energy_share_percent,omitempty"`
	EnergyImpact          float64   `json:"energy_impact,omitempty"`
	CPUTimeMSPerS         float64   `json:"cpu_time_ms_per_s,omitempty"`
	GPUTimeMSPerS         float64   `json:"gpu_time_ms_per_s,omitempty"`
	InterruptWakeupsPerS  float64   `json:"interrupt_wakeups_per_s,omitempty"`
	IdleWakeupsPerS       float64   `json:"idle_wakeups_per_s,omitempty"`
	DiskReadBytesPerS     float64   `json:"disk_read_bytes_per_s,omitempty"`
	DiskWriteBytesPerS    float64   `json:"disk_write_bytes_per_s,omitempty"`
	NetworkReadBytesPerS  float64   `json:"network_read_bytes_per_s,omitempty"`
	NetworkWriteBytesPerS float64   `json:"network_write_bytes_per_s,omitempty"`
	Confidence            string    `json:"confidence"`
	AttributionSource     string    `json:"attribution_source"`
}

// AttributionResult is the app attribution section attached to a power sample.
type AttributionResult struct {
	Method            string     `json:"method,omitempty"`
	Confidence        string     `json:"confidence,omitempty"`
	BaselineWatts     float64    `json:"baseline_w,omitempty"`
	DynamicWatts      float64    `json:"dynamic_w,omitempty"`
	CPUComponentPoolW float64    `json:"cpu_component_pool_w,omitempty"`
	GPUComponentPoolW float64    `json:"gpu_component_pool_w,omitempty"`
	ResidualPoolW     float64    `json:"residual_pool_w,omitempty"`
	Apps              []AppPower `json:"apps,omitempty"`
}

// PowerSample is the canonical timestamp-aligned monitoring record.
type PowerSample struct {
	Schema            string            `json:"schema"`
	Timestamp         time.Time         `json:"timestamp"`
	SessionID         string            `json:"session_id"`
	Sequence          uint64            `json:"sequence"`
	Phase             string            `json:"phase,omitempty"`
	Battery           BatterySample     `json:"battery"`
	Adapter           AdapterSample     `json:"adapter"`
	Components        ComponentSample   `json:"components"`
	Thermal           ThermalSample     `json:"thermal"`
	PrimaryLoadW      float64           `json:"primary_load_w,omitempty"`
	PrimaryLoadSource string            `json:"primary_load_source,omitempty"`
	BaselineLoadW     float64           `json:"baseline_load_w,omitempty"`
	Attribution       AttributionResult `json:"attribution,omitempty"`
	CollectorStatus   map[string]string `json:"collector_status,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
}

// Event records an observable state transition or warning.
type Event struct {
	Schema    string         `json:"schema"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	Type      string         `json:"type"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// TestRun records a benchmark phase or standalone workload.
type TestRun struct {
	Schema           string            `json:"schema"`
	ID               string            `json:"id"`
	SessionID        string            `json:"session_id"`
	Name             string            `json:"name"`
	Plan             string            `json:"plan"`
	Phase            string            `json:"phase"`
	Status           string            `json:"status"`
	StartedAt        time.Time         `json:"started_at"`
	EndedAt          time.Time         `json:"ended_at,omitempty"`
	RequestedSeconds float64           `json:"requested_seconds"`
	ActualSeconds    float64           `json:"actual_seconds,omitempty"`
	Commands         [][]string        `json:"commands,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Error            string            `json:"error,omitempty"`
}

// RuntimeSettings is the versioned, effective collection and persistence
// configuration. Durations use integer milliseconds so the same contract can
// be consumed without custom duration parsing by Go, Swift, and saved JSON.
type RuntimeSettings struct {
	Schema              string `json:"schema"`
	Profile             string `json:"profile"`
	UIRefreshMS         int64  `json:"ui_refresh_ms"`
	BatteryCollectionMS int64  `json:"battery_collection_ms"`
	PowermetricsMS      int64  `json:"powermetrics_ms"`
	AppAttributionMS    int64  `json:"app_attribution_ms"`
	LoggingEnabled      bool   `json:"logging_enabled"`
	LogIntervalMS       int64  `json:"log_interval_ms"`
	ProcessNice         int    `json:"process_nice"`
}

// EffectiveCollectionOptions records non-profile CLI and diagnostic settings
// that materially affect the meaning or availability of collected data.
type EffectiveCollectionOptions struct {
	AppAttribution bool `json:"app_attribution"`
	TopApps        int  `json:"top_apps"`
	SQLiteMirror   bool `json:"sqlite_mirror"`
	SafeMode       bool `json:"safe_mode,omitempty"`
}

// Session describes one monitor or benchmark collection session.
type Session struct {
	Schema           string                      `json:"schema"`
	ID               string                      `json:"id"`
	Version          string                      `json:"version"`
	StartedAt        time.Time                   `json:"started_at"`
	EndedAt          time.Time                   `json:"ended_at,omitempty"`
	Hostname         string                      `json:"hostname,omitempty"`
	OSVersion        string                      `json:"os_version,omitempty"`
	OSBuild          string                      `json:"os_build,omitempty"`
	Machine          string                      `json:"machine,omitempty"`
	Chip             string                      `json:"chip,omitempty"`
	DataDirectory    string                      `json:"data_directory"`
	RuntimeSettings  RuntimeSettings             `json:"runtime_settings"`
	EffectiveOptions *EffectiveCollectionOptions `json:"effective_options,omitempty"`
	Metadata         map[string]string           `json:"metadata,omitempty"`
}

// BenchmarkProgress drives the terminal and SwiftUI progress surfaces.
type BenchmarkProgress struct {
	Running       bool      `json:"running"`
	Plan          string    `json:"plan,omitempty"`
	Phase         string    `json:"phase,omitempty"`
	PhaseIndex    int       `json:"phase_index,omitempty"`
	PhaseCount    int       `json:"phase_count,omitempty"`
	PhaseStarted  time.Time `json:"phase_started,omitempty"`
	PhaseDuration float64   `json:"phase_duration_seconds,omitempty"`
	Elapsed       float64   `json:"elapsed_seconds,omitempty"`
	Remaining     float64   `json:"remaining_seconds,omitempty"`
	Percent       float64   `json:"percent,omitempty"`
	Status        string    `json:"status,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// Status is the current engine status exposed to the TUI and local API.
type Status struct {
	Schema         string            `json:"schema"`
	Version        string            `json:"version"`
	MonitorRunning bool              `json:"monitor_running"`
	Session        *Session          `json:"session,omitempty"`
	LastSample     *PowerSample      `json:"last_sample,omitempty"`
	Benchmark      BenchmarkProgress `json:"benchmark"`
	Capabilities   map[string]bool   `json:"capabilities,omitempty"`
	Errors         []string          `json:"errors,omitempty"`
}

// SessionSummary is the stable report contract generated from a session.
type SessionSummary struct {
	Schema              string     `json:"schema"`
	Session             Session    `json:"session"`
	SampleCount         int64      `json:"sample_count"`
	DurationSeconds     float64    `json:"duration_seconds"`
	PeakPrimaryLoadW    float64    `json:"peak_primary_load_w,omitempty"`
	AveragePrimaryLoadW float64    `json:"average_primary_load_w,omitempty"`
	PeakBatteryDrawW    float64    `json:"peak_battery_draw_w,omitempty"`
	AverageBatteryDrawW float64    `json:"average_battery_draw_w,omitempty"`
	PeakChargeW         float64    `json:"peak_charge_w,omitempty"`
	MaxBatteryTempC     float64    `json:"max_battery_temp_c,omitempty"`
	EnergyDischargedWh  float64    `json:"energy_discharged_wh,omitempty"`
	EnergyChargedWh     float64    `json:"energy_charged_wh,omitempty"`
	TopApps             []AppPower `json:"top_apps,omitempty"`
	TestRuns            []TestRun  `json:"test_runs,omitempty"`
	Warnings            []string   `json:"warnings,omitempty"`
}

// ParityDifference is one field comparison between Go and the legacy collector.
type ParityDifference struct {
	Field        string  `json:"field"`
	GoValue      float64 `json:"go_value"`
	LegacyValue  float64 `json:"legacy_value"`
	AbsoluteDiff float64 `json:"absolute_diff"`
	Tolerance    float64 `json:"tolerance"`
	Pass         bool    `json:"pass"`
}

// ParityReport records live collector parity against the legacy implementation.
type ParityReport struct {
	Schema      string             `json:"schema"`
	CreatedAt   time.Time          `json:"created_at"`
	Iterations  int                `json:"iterations"`
	Passed      bool               `json:"passed"`
	Differences []ParityDifference `json:"differences"`
	Errors      []string           `json:"errors,omitempty"`
}
