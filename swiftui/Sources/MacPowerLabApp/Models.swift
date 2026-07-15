import Foundation

struct AppPower: Codable, Identifiable, Hashable {
    var id: String { key }

    let key: String
    let name: String
    let category: String?
    let pid: Int?
    let responsiblePID: Int?
    let coalitionID: Int64?
    let estimatedShareW: Double?
    let estimatedDynamicW: Double?
    let estimatedCPUW: Double?
    let estimatedGPUW: Double?
    let estimatedResidualW: Double?
    let estimatedEnergyWh: Double?
    let energySharePercent: Double?
    let energyImpact: Double?
    let cpuTimeMSPerS: Double?
    let gpuTimeMSPerS: Double?
    let interruptWakeupsPerS: Double?
    let idleWakeupsPerS: Double?
    let diskReadBytesPerS: Double?
    let diskWriteBytesPerS: Double?
    let networkReadBytesPerS: Double?
    let networkWriteBytesPerS: Double?
    let confidence: String
    let attributionSource: String?

    enum CodingKeys: String, CodingKey {
        case key, name, category, pid, confidence
        case responsiblePID = "responsible_pid"
        case coalitionID = "coalition_id"
        case estimatedShareW = "estimated_share_w"
        case estimatedDynamicW = "estimated_dynamic_w"
        case estimatedCPUW = "estimated_cpu_w"
        case estimatedGPUW = "estimated_gpu_w"
        case estimatedResidualW = "estimated_residual_w"
        case estimatedEnergyWh = "estimated_energy_wh"
        case energySharePercent = "energy_share_percent"
        case energyImpact = "energy_impact"
        case cpuTimeMSPerS = "cpu_time_ms_per_s"
        case gpuTimeMSPerS = "gpu_time_ms_per_s"
        case interruptWakeupsPerS = "interrupt_wakeups_per_s"
        case idleWakeupsPerS = "idle_wakeups_per_s"
        case diskReadBytesPerS = "disk_read_bytes_per_s"
        case diskWriteBytesPerS = "disk_write_bytes_per_s"
        case networkReadBytesPerS = "network_read_bytes_per_s"
        case networkWriteBytesPerS = "network_write_bytes_per_s"
        case attributionSource = "attribution_source"
    }

    var sortName: String {
        name
            .folding(
                options: [.caseInsensitive, .diacriticInsensitive],
                locale: .current
            )
            .lowercased()
    }
    var sortCategory: String { category?.lowercased() ?? "" }
    var sortDynamicW: Double { estimatedDynamicW ?? 0 }
    var sortTotalW: Double { estimatedShareW ?? 0 }
    var sortCPUW: Double { estimatedCPUW ?? 0 }
    var sortGPUW: Double { estimatedGPUW ?? 0 }
    var sortResidualW: Double { estimatedResidualW ?? 0 }
    var sortEnergyWh: Double { estimatedEnergyWh ?? 0 }
    var sortEnergyShare: Double { energySharePercent ?? 0 }
    var sortEnergyImpact: Double { energyImpact ?? 0 }
    var sortCPUTime: Double { cpuTimeMSPerS ?? 0 }
    var sortGPUTime: Double { gpuTimeMSPerS ?? 0 }
    var sortInterruptWakeups: Double { interruptWakeupsPerS ?? 0 }
    var sortIdleWakeups: Double { idleWakeupsPerS ?? 0 }
    var sortDiskRead: Double { diskReadBytesPerS ?? 0 }
    var sortDiskWrite: Double { diskWriteBytesPerS ?? 0 }
    var sortNetworkRead: Double { networkReadBytesPerS ?? 0 }
    var sortNetworkWrite: Double { networkWriteBytesPerS ?? 0 }
    var sortPID: Int { pid ?? 0 }
    var sortResponsiblePID: Int { responsiblePID ?? 0 }
    var sortCoalitionID: Int64 { coalitionID ?? 0 }
    var sortAttributionSource: String { attributionSource?.lowercased() ?? "" }
    var confidenceRank: Int {
        switch confidence.lowercased() {
        case "high": return 3
        case "medium": return 2
        case "low": return 1
        default: return 0
        }
    }
}

struct BatterySample: Codable {
    let percent: Double?
    let powerSource: String?
    let state: String?
    let externalConnected: Bool?
    let charging: Bool?
    let voltageV: Double?
    let currentA: Double?
    let netW: Double?
    let temperatureC: Double?
    let temperatureF: Double?
    let temperatureRaw: Double?
    let virtualTemperatureC: Double?
    let cycleCount: Int?
    let currentCapacityMAh: Double?
    let fullChargeCapacityMAh: Double?
    let designCapacityMAh: Double?
    let healthPercent: Double?
    let estimatedRemainingWh: Double?
    let estimatedFullWh: Double?
    let cellVoltageMinMV: Double?
    let cellVoltageMaxMV: Double?
    let cellVoltageDeltaMV: Double?
    let qMaxDelta: Double?
    let weightedRADelta: Double?
    let cellDisconnectCount: Int?
    let timeToEmptyMinutes: Double?
    let timeToFullMinutes: Double?
    let bmsSystemPowerW: Double?
    let systemEffectiveTotalLoadW: Double?
    let powerDistributionInputW: Double?

    enum CodingKeys: String, CodingKey {
        case percent, state, charging
        case powerSource = "power_source"
        case externalConnected = "external_connected"
        case voltageV = "voltage_v"
        case currentA = "current_a"
        case netW = "net_w"
        case temperatureC = "temperature_c"
        case temperatureF = "temperature_f"
        case temperatureRaw = "temperature_raw"
        case virtualTemperatureC = "virtual_temperature_c"
        case cycleCount = "cycle_count"
        case currentCapacityMAh = "current_capacity_mah"
        case fullChargeCapacityMAh = "full_charge_capacity_mah"
        case designCapacityMAh = "design_capacity_mah"
        case healthPercent = "health_percent"
        case estimatedRemainingWh = "estimated_remaining_wh"
        case estimatedFullWh = "estimated_full_wh"
        case cellVoltageMinMV = "cell_voltage_min_mv"
        case cellVoltageMaxMV = "cell_voltage_max_mv"
        case cellVoltageDeltaMV = "cell_voltage_delta_mv"
        case qMaxDelta = "qmax_delta"
        case weightedRADelta = "weighted_ra_delta"
        case cellDisconnectCount = "cell_disconnect_count"
        case timeToEmptyMinutes = "time_to_empty_minutes"
        case timeToFullMinutes = "time_to_full_minutes"
        case bmsSystemPowerW = "bms_system_power_w"
        case systemEffectiveTotalLoadW = "system_effective_total_load_w"
        case powerDistributionInputW = "power_distribution_input_w"
    }
}

struct AdapterSample: Codable {
    let name: String?
    let connected: Bool
    let ratedW: Double?
    let contractVoltageV: Double?
    let contractCurrentA: Double?
    let contractW: Double?
    let outputEstimateW: Double?
    let outputEstimateSource: String?
    let loadPercent: Double?
    let headroomW: Double?
    let batteryAssistW: Double?
    let portControllerMaxPowerW: Double?

    enum CodingKeys: String, CodingKey {
        case name, connected
        case ratedW = "rated_w"
        case contractVoltageV = "contract_voltage_v"
        case contractCurrentA = "contract_current_a"
        case contractW = "contract_w"
        case outputEstimateW = "output_estimate_w"
        case outputEstimateSource = "output_estimate_source"
        case loadPercent = "load_percent"
        case headroomW = "headroom_w"
        case batteryAssistW = "battery_assist_w"
        case portControllerMaxPowerW = "port_controller_max_power_w"
    }
}

struct ClusterSample: Codable, Identifiable {
    var id: String { name }

    let name: String
    let frequencyMHz: Double?
    let activePercent: Double?
    let powerW: Double?

    enum CodingKeys: String, CodingKey {
        case name
        case frequencyMHz = "frequency_mhz"
        case activePercent = "active_percent"
        case powerW = "power_w"
    }
}

struct ComponentSample: Codable {
    let cpuW: Double?
    let gpuW: Double?
    let aneW: Double?
    let dramW: Double?
    let packageW: Double?
    let gpuMHz: Double?
    let cpuEstimateMHz: Double?
    let cpuEstimateSource: String?
    let clusters: [ClusterSample]?

    enum CodingKeys: String, CodingKey {
        case clusters
        case cpuW = "cpu_w"
        case gpuW = "gpu_w"
        case aneW = "ane_w"
        case dramW = "dram_w"
        case packageW = "package_w"
        case gpuMHz = "gpu_mhz"
        case cpuEstimateMHz = "cpu_estimate_mhz"
        case cpuEstimateSource = "cpu_estimate_source"
    }
}

struct ThermalSample: Codable {
    let macosPressure: String?
    let batteryState: String?
    let batteryTrendCPerMin: Double?
    let summary: String?
    let source: String?

    enum CodingKeys: String, CodingKey {
        case summary, source
        case macosPressure = "macos_pressure"
        case batteryState = "battery_state"
        case batteryTrendCPerMin = "battery_trend_c_per_min"
    }
}

struct AttributionResult: Codable {
    let method: String?
    let confidence: String?
    let baselineW: Double?
    let dynamicW: Double?
    let cpuComponentPoolW: Double?
    let gpuComponentPoolW: Double?
    let residualPoolW: Double?
    let apps: [AppPower]?

    enum CodingKeys: String, CodingKey {
        case method, confidence, apps
        case baselineW = "baseline_w"
        case dynamicW = "dynamic_w"
        case cpuComponentPoolW = "cpu_component_pool_w"
        case gpuComponentPoolW = "gpu_component_pool_w"
        case residualPoolW = "residual_pool_w"
    }
}

struct PowerSample: Codable {
    let timestamp: Date
    let sessionID: String
    let sequence: UInt64?
    let phase: String?
    let battery: BatterySample
    let adapter: AdapterSample
    let components: ComponentSample
    let thermal: ThermalSample
    let primaryLoadW: Double?
    let primaryLoadSource: String?
    let baselineLoadW: Double?
    let attribution: AttributionResult?
    let collectorStatus: [String: String]?
    let warnings: [String]?

    enum CodingKeys: String, CodingKey {
        case timestamp, sequence, phase, battery, adapter, components, thermal, attribution, warnings
        case sessionID = "session_id"
        case primaryLoadW = "primary_load_w"
        case primaryLoadSource = "primary_load_source"
        case baselineLoadW = "baseline_load_w"
        case collectorStatus = "collector_status"
    }
}

struct Session: Codable {
    let id: String
    let version: String
    let startedAt: Date?
    let endedAt: Date?
    let hostname: String?
    let osVersion: String?
    let osBuild: String?
    let machine: String?
    let chip: String?
    let dataDirectory: String?
    let metadata: [String: String]?

    enum CodingKeys: String, CodingKey {
        case id, version, hostname, machine, chip, metadata
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case osVersion = "os_version"
        case osBuild = "os_build"
        case dataDirectory = "data_directory"
    }
}

struct BenchmarkProgress: Codable {
    let running: Bool
    let plan: String?
    let phase: String?
    let phaseIndex: Int?
    let phaseCount: Int?
    let percent: Double?
    let elapsedSeconds: Double?
    let remainingSeconds: Double?
    let status: String?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case running, plan, phase, percent, status, error
        case phaseIndex = "phase_index"
        case phaseCount = "phase_count"
        case elapsedSeconds = "elapsed_seconds"
        case remainingSeconds = "remaining_seconds"
    }
}

struct ReportArtifact: Codable {
    let schema: String?
    let generatedAt: Date
    let dataThrough: Date
    let sessionID: String
    let directory: String
    let summaryPath: String
    let markdownPath: String
    let htmlPath: String

    enum CodingKeys: String, CodingKey {
        case schema, directory
        case generatedAt = "generated_at"
        case dataThrough = "data_through"
        case sessionID = "session_id"
        case summaryPath = "summary_path"
        case markdownPath = "markdown_path"
        case htmlPath = "html_path"
    }
}

struct EngineStatus: Codable {
    let version: String
    let monitorRunning: Bool
    let session: Session?
    let lastSample: PowerSample?
    let benchmark: BenchmarkProgress
    let capabilities: [String: Bool]?
    let errors: [String]?

    enum CodingKeys: String, CodingKey {
        case version, session, benchmark, capabilities, errors
        case monitorRunning = "monitor_running"
        case lastSample = "last_sample"
    }
}


struct BenchmarkPhaseDefinition: Codable, Identifiable, Hashable {
    var id: String { "\(name)-\(kind)-\(durationSeconds)" }
    let name: String
    let kind: String
    let description: String
    let durationSeconds: Double
    let components: [String]?
    let profile: String?

    enum CodingKeys: String, CodingKey {
        case name, kind, description, components, profile
        case durationSeconds = "duration_seconds"
    }
}

struct BenchmarkDefinition: Codable, Identifiable, Hashable {
    let id: String
    let name: String
    let summary: String
    let description: String
    let category: String
    let icon: String
    let intensity: String
    let requiredPowerSource: String?
    let typicalDurationSeconds: Double
    let adjustableDuration: Bool
    let minimumDurationSeconds: Double?
    let maximumDurationSeconds: Double?
    let bestFor: [String]
    let metrics: [String]
    let safetyNotes: [String]?
    let phases: [BenchmarkPhaseDefinition]

    enum CodingKeys: String, CodingKey {
        case id, name, summary, description, category, icon, intensity, metrics, phases
        case requiredPowerSource = "required_power_source"
        case typicalDurationSeconds = "typical_duration_seconds"
        case adjustableDuration = "adjustable_duration"
        case minimumDurationSeconds = "minimum_duration_seconds"
        case maximumDurationSeconds = "maximum_duration_seconds"
        case bestFor = "best_for"
        case safetyNotes = "safety_notes"
    }

    static let fallbackCatalog: [BenchmarkDefinition] = [
        BenchmarkDefinition(
            id: "quick",
            name: "Quick diagnostic",
            summary: "Fast idle, CPU, GPU, memory, and mixed-load health check.",
            description: "Verifies workloads and sensors after installation or an operating-system update.",
            category: "Diagnostic",
            icon: "stethoscope",
            intensity: "Moderate",
            requiredPowerSource: nil,
            typicalDurationSeconds: 225,
            adjustableDuration: false,
            minimumDurationSeconds: nil,
            maximumDurationSeconds: nil,
            bestFor: ["Fast health check"],
            metrics: ["Power", "Thermals", "Sensor compatibility"],
            safetyNotes: nil,
            phases: []
        ),
        BenchmarkDefinition(
            id: "battery",
            name: "Battery discharge suite",
            summary: "Complete unplugged battery and runtime benchmark.",
            description: "Runs idle, CPU, GPU, memory, and extreme phases while unplugged.",
            category: "Battery",
            icon: "battery.25percent",
            intensity: "Variable",
            requiredPowerSource: "Battery Power",
            typicalDurationSeconds: 660,
            adjustableDuration: false,
            minimumDurationSeconds: nil,
            maximumDurationSeconds: nil,
            bestFor: ["Runtime projection"],
            metrics: ["Wh used", "Peak draw", "Temperature"],
            safetyNotes: ["Unplug the charger before starting."],
            phases: []
        ),
        BenchmarkDefinition(
            id: "extreme",
            name: "Extreme soak",
            summary: "Maximum CPU, GPU, and memory load.",
            description: "The heaviest sustained MacPowerLab workload.",
            category: "System",
            icon: "flame.fill",
            intensity: "Maximum",
            requiredPowerSource: nil,
            typicalDurationSeconds: 900,
            adjustableDuration: true,
            minimumDurationSeconds: 60,
            maximumDurationSeconds: 7200,
            bestFor: ["Maximum load"],
            metrics: ["Peak watts", "Thermal pressure"],
            safetyNotes: ["Keep vents clear."],
            phases: []
        ),
    ]
}

struct CustomBenchmarkPayload: Encodable {
    let displayName: String
    let requiredPowerSource: String
    let cpu: Bool
    let gpu: Bool
    let memory: Bool
    let gpuProfile: String
    let memoryMB: Int
    let workloadSeconds: Double
    let baselineSeconds: Double
    let cooldownSeconds: Double

    enum CodingKeys: String, CodingKey {
        case cpu, gpu, memory
        case displayName = "display_name"
        case requiredPowerSource = "required_power_source"
        case gpuProfile = "gpu_profile"
        case memoryMB = "memory_mb"
        case workloadSeconds = "workload_seconds"
        case baselineSeconds = "baseline_seconds"
        case cooldownSeconds = "cooldown_seconds"
    }
}
