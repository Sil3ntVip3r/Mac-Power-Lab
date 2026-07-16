import SwiftUI

struct FullMonitorView: View {
    let status: EngineStatus?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                MonitorPageHeader(
                    title: "Full Monitor",
                    subtitle: "Every available live sensor and engine statistic, grouped by subsystem. Application rows are intentionally excluded.",
                    systemImage: "rectangle.3.group"
                )

                if let sample = status?.lastSample {
                    MonitorSection("Session & Engine", systemImage: "gearshape.2") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Engine version", value: status?.version ?? "n/a"),
                            MonitorStat(title: "Monitor running", value: status?.monitorRunning == true ? "Yes" : "No"),
                            MonitorStat(title: "Session ID", value: status?.session?.id ?? sample.sessionID),
                            MonitorStat(title: "Session started", value: MPLFormat.date(status?.session?.startedAt)),
                            MonitorStat(title: "Hostname", value: MPLFormat.text(status?.session?.hostname)),
                            MonitorStat(title: "macOS", value: [status?.session?.osVersion, status?.session?.osBuild].compactMap { $0 }.joined(separator: " / ")),
                            MonitorStat(title: "Machine", value: MPLFormat.text(status?.session?.machine)),
                            MonitorStat(title: "Chip", value: MPLFormat.text(status?.session?.chip)),
                            MonitorStat(title: "Sample timestamp", value: MPLFormat.date(sample.timestamp)),
                            MonitorStat(title: "Sequence", value: sample.sequence.map(String.init) ?? "n/a"),
                            MonitorStat(title: "Phase", value: MPLFormat.text(sample.phase)),
                            MonitorStat(title: "Data directory", value: MPLFormat.text(status?.session?.dataDirectory)),
                        ])
                    }

                    MonitorSection("Primary Power", systemImage: "bolt.fill") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Primary load", value: MPLFormat.watts(sample.primaryLoadW)),
                            MonitorStat(title: "Primary source", value: MPLFormat.text(sample.primaryLoadSource)),
                            MonitorStat(title: "Baseline load", value: MPLFormat.watts(sample.baselineLoadW)),
                            MonitorStat(title: "Battery net flow", value: MPLFormat.watts(sample.battery.netW)),
                            MonitorStat(title: "BMS SystemPower", value: MPLFormat.watts(sample.battery.bmsSystemPowerW)),
                            MonitorStat(title: "System effective total load", value: MPLFormat.watts(sample.battery.systemEffectiveTotalLoadW)),
                            MonitorStat(title: "PowerDistribution input", value: MPLFormat.watts(sample.battery.powerDistributionInputW)),
                        ])
                    }

                    MonitorSection("Battery Electrical & Temperature", systemImage: "battery.75percent") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Battery percent", value: MPLFormat.percent(sample.battery.percent)),
                            MonitorStat(title: "Power source", value: MPLFormat.text(sample.battery.powerSource)),
                            MonitorStat(title: "State", value: MPLFormat.text(sample.battery.state)),
                            MonitorStat(title: "External connected", value: MPLFormat.boolean(sample.battery.externalConnected)),
                            MonitorStat(title: "Charging", value: MPLFormat.boolean(sample.battery.charging)),
                            MonitorStat(title: "Voltage", value: MPLFormat.volts(sample.battery.voltageV)),
                            MonitorStat(title: "Current", value: MPLFormat.amps(sample.battery.currentA)),
                            MonitorStat(title: "Net watts", value: MPLFormat.watts(sample.battery.netW)),
                            MonitorStat(title: "Temperature", value: MPLFormat.celsius(sample.battery.temperatureC)),
                            MonitorStat(title: "Temperature °F", value: MPLFormat.fahrenheit(sample.battery.temperatureF)),
                            MonitorStat(title: "Raw temperature", value: MPLFormat.number(sample.battery.temperatureRaw, digits: 1)),
                            MonitorStat(title: "Virtual temperature", value: MPLFormat.celsius(sample.battery.virtualTemperatureC)),
                        ])
                    }

                    MonitorSection("Battery Capacity & Health", systemImage: "heart.text.square") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Cycle count", value: MPLFormat.integer(sample.battery.cycleCount)),
                            MonitorStat(title: "Current capacity", value: MPLFormat.milliampHours(sample.battery.currentCapacityMAh)),
                            MonitorStat(title: "Full-charge capacity", value: MPLFormat.milliampHours(sample.battery.fullChargeCapacityMAh)),
                            MonitorStat(title: "Design capacity", value: MPLFormat.milliampHours(sample.battery.designCapacityMAh)),
                            MonitorStat(title: "Health estimate", value: MPLFormat.percent(sample.battery.healthPercent)),
                            MonitorStat(title: "Remaining energy", value: MPLFormat.wattHours(sample.battery.estimatedRemainingWh)),
                            MonitorStat(title: "Full energy", value: MPLFormat.wattHours(sample.battery.estimatedFullWh)),
                            MonitorStat(title: "Time to empty", value: MPLFormat.minutes(sample.battery.timeToEmptyMinutes)),
                            MonitorStat(title: "Time to full", value: MPLFormat.minutes(sample.battery.timeToFullMinutes)),
                            MonitorStat(title: "Cell minimum", value: MPLFormat.millivolts(sample.battery.cellVoltageMinMV)),
                            MonitorStat(title: "Cell maximum", value: MPLFormat.millivolts(sample.battery.cellVoltageMaxMV)),
                            MonitorStat(title: "Cell delta", value: MPLFormat.millivolts(sample.battery.cellVoltageDeltaMV)),
                            MonitorStat(title: "QMax delta", value: MPLFormat.number(sample.battery.qMaxDelta, digits: 2)),
                            MonitorStat(title: "Weighted Ra delta", value: MPLFormat.number(sample.battery.weightedRADelta, digits: 2)),
                            MonitorStat(title: "Cell disconnects", value: MPLFormat.integer(sample.battery.cellDisconnectCount)),
                        ])
                    }

                    MonitorSection("Adapter & Charging", systemImage: "powerplug.fill") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Name", value: MPLFormat.text(sample.adapter.name)),
                            MonitorStat(title: "Connected", value: sample.adapter.connected ? "Yes" : "No"),
                            MonitorStat(title: "Rated watts", value: MPLFormat.watts(sample.adapter.ratedW)),
                            MonitorStat(title: "Contract voltage", value: MPLFormat.volts(sample.adapter.contractVoltageV)),
                            MonitorStat(title: "Contract current", value: MPLFormat.amps(sample.adapter.contractCurrentA)),
                            MonitorStat(title: "Contract watts", value: MPLFormat.watts(sample.adapter.contractW)),
                            MonitorStat(title: "Estimated output", value: MPLFormat.watts(sample.adapter.outputEstimateW)),
                            MonitorStat(title: "Estimate source", value: MPLFormat.text(sample.adapter.outputEstimateSource)),
                            MonitorStat(title: "Load", value: MPLFormat.percent(sample.adapter.loadPercent)),
                            MonitorStat(title: "Headroom", value: MPLFormat.watts(sample.adapter.headroomW)),
                            MonitorStat(title: "Battery assist", value: MPLFormat.watts(sample.adapter.batteryAssistW)),
                            MonitorStat(title: "Port controller max", value: MPLFormat.watts(sample.adapter.portControllerMaxPowerW)),
                        ])
                    }

                    MonitorSection("SoC Components", systemImage: "cpu") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "CPU power", value: MPLFormat.watts(sample.components.cpuW)),
                            MonitorStat(title: "GPU power", value: MPLFormat.watts(sample.components.gpuW)),
                            MonitorStat(title: "ANE power", value: MPLFormat.watts(sample.components.aneW)),
                            MonitorStat(title: "DRAM power", value: MPLFormat.watts(sample.components.dramW)),
                            MonitorStat(title: "Package power", value: MPLFormat.watts(sample.components.packageW)),
                            MonitorStat(title: "GPU frequency", value: MPLFormat.megahertz(sample.components.gpuMHz)),
                            MonitorStat(title: "CPU frequency estimate", value: MPLFormat.megahertz(sample.components.cpuEstimateMHz)),
                            MonitorStat(title: "CPU estimate source", value: MPLFormat.text(sample.components.cpuEstimateSource)),
                        ])

                        if let clusters = sample.components.clusters, !clusters.isEmpty {
                            Divider()
                                .padding(.vertical, 8)
                            ForEach(clusters) { cluster in
                                HStack {
                                    Text(cluster.name)
                                        .fontWeight(.semibold)
                                        .frame(width: 100, alignment: .leading)
                                    LabeledContent("Frequency", value: MPLFormat.megahertz(cluster.frequencyMHz))
                                    LabeledContent("Active", value: MPLFormat.percent(cluster.activePercent))
                                    LabeledContent("Power", value: MPLFormat.watts(cluster.powerW))
                                }
                                if cluster.id != clusters.last?.id {
                                    Divider()
                                }
                            }
                        }
                    }

                    MonitorSection("Thermal", systemImage: "thermometer.high") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "macOS pressure", value: MPLFormat.text(sample.thermal.macosPressure)),
                            MonitorStat(title: "Battery thermal state", value: MPLFormat.text(sample.thermal.batteryState)),
                            MonitorStat(title: "Battery trend", value: "\(MPLFormat.number(sample.thermal.batteryTrendCPerMin, digits: 2)) °C/min"),
                            MonitorStat(title: "Summary", value: MPLFormat.text(sample.thermal.summary)),
                            MonitorStat(title: "Source", value: MPLFormat.text(sample.thermal.source)),
                        ])
                    }

                    MonitorSection("Application Attribution Summary", systemImage: "app.badge") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Method", value: MPLFormat.text(sample.attribution?.method)),
                            MonitorStat(title: "Confidence", value: MPLFormat.text(sample.attribution?.confidence)),
                            MonitorStat(title: "Baseline pool", value: MPLFormat.watts(sample.attribution?.baselineW)),
                            MonitorStat(title: "Dynamic pool", value: MPLFormat.watts(sample.attribution?.dynamicW)),
                            MonitorStat(title: "CPU pool", value: MPLFormat.watts(sample.attribution?.cpuComponentPoolW)),
                            MonitorStat(title: "GPU pool", value: MPLFormat.watts(sample.attribution?.gpuComponentPoolW)),
                            MonitorStat(title: "Residual pool", value: MPLFormat.watts(sample.attribution?.residualPoolW)),
                            MonitorStat(title: "Tracked apps", value: String(sample.attribution?.apps?.count ?? 0)),
                        ])
                        Text("Application rows are available on the dedicated Applications page.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .padding(.top, 8)
                    }

                    MonitorSection("Benchmark", systemImage: "speedometer") {
                        let benchmark = status?.benchmark
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Running", value: benchmark?.running == true ? "Yes" : "No"),
                            MonitorStat(title: "Plan", value: MPLFormat.text(benchmark?.plan)),
                            MonitorStat(title: "Phase", value: MPLFormat.text(benchmark?.phase)),
                            MonitorStat(title: "Phase index", value: benchmark?.phaseIndex.map(String.init) ?? "n/a"),
                            MonitorStat(title: "Phase count", value: benchmark?.phaseCount.map(String.init) ?? "n/a"),
                            MonitorStat(title: "Progress", value: MPLFormat.percent(benchmark?.percent)),
                            MonitorStat(title: "Elapsed", value: MPLFormat.duration(benchmark?.elapsedSeconds)),
                            MonitorStat(title: "Remaining", value: MPLFormat.duration(benchmark?.remainingSeconds)),
                            MonitorStat(title: "Status", value: MPLFormat.text(benchmark?.status)),
                            MonitorStat(title: "Error", value: MPLFormat.text(benchmark?.error)),
                            MonitorStat(
                                title: "Backend nice",
                                value: niceValue(benchmark?.priority?.observedBackendNice),
                                detail: benchmark?.priority.map { "Requested \(niceValue($0.requestedBackendNice))" }
                            ),
                            MonitorStat(
                                title: "Workload nice",
                                value: workloadNiceSummary(benchmark?.priority),
                                detail: benchmark?.priority?.capturedAt.formatted(date: .omitted, time: .standard)
                            ),
                        ])
                    }

                    if let cadence = status?.cadence {
                        MonitorSection("Cadence Diagnostics", systemImage: "metronome") {
                            MonitorStatGrid(items: [
                                cadenceStat("Live publication", cadence.uiRefresh),
                                cadenceStat("Battery collection", cadence.batteryCollection),
                                cadenceStat("powermetrics", cadence.powermetrics),
                                cadenceStat("Application attribution", cadence.appAttribution),
                                cadenceStat("Durable logging", cadence.durableLogging),
                                MonitorStat(
                                    title: "Live publications",
                                    value: String(cadence.livePublications ?? 0),
                                    detail: "Backend live states published during this session."
                                ),
                                MonitorStat(
                                    title: "Replaced stream frames",
                                    value: String(cadence.replacedStreamFrames ?? 0),
                                    detail: "Newest-value replacement prevents stream backlog."
                                ),
                            ])
                        }
                    }

                    MonitorSection("Collector Status", systemImage: "waveform.badge.magnifyingglass") {
                        if let collectorStatus = sample.collectorStatus, !collectorStatus.isEmpty {
                            MonitorStatGrid(
                                items: collectorStatus.keys.sorted().map { key in
                                    MonitorStat(title: key, value: collectorStatus[key] ?? "n/a")
                                }
                            )
                        } else {
                            Text("No collector status map is available.")
                                .foregroundStyle(.secondary)
                        }
                    }

                    MonitorSection("Capabilities", systemImage: "checkmark.circle") {
                        if let capabilities = status?.capabilities, !capabilities.isEmpty {
                            MonitorStatGrid(
                                items: capabilities.keys.sorted().map { key in
                                    MonitorStat(
                                        title: key,
                                        value: capabilities[key] == true ? "Available" : "Unavailable"
                                    )
                                }
                            )
                        } else {
                            Text("No capability data is available.")
                                .foregroundStyle(.secondary)
                        }
                    }

                    if let warnings = sample.warnings, !warnings.isEmpty {
                        MonitorSection("Warnings", systemImage: "exclamationmark.triangle.fill") {
                            VStack(alignment: .leading, spacing: 8) {
                                ForEach(warnings, id: \.self) { warning in
                                    Text(warning)
                                        .foregroundStyle(.orange)
                                        .textSelection(.enabled)
                                }
                            }
                        }
                    }

                    if let errors = status?.errors, !errors.isEmpty {
                        MonitorSection("Engine Errors", systemImage: "xmark.octagon.fill") {
                            VStack(alignment: .leading, spacing: 8) {
                                ForEach(errors, id: \.self) { error in
                                    Text(error)
                                        .foregroundStyle(.red)
                                        .textSelection(.enabled)
                                }
                            }
                        }
                    }
                } else {
                    MonitorEmptyState(
                        title: "No monitor sample yet",
                        message: "Start the monitor to display the complete sensor set.",
                        systemImage: "rectangle.3.group"
                    )
                }
            }
            .padding()
        }
    }

    private func cadenceStat(_ title: String, _ metric: CadenceMetric) -> MonitorStat {
        let requested = cadenceDuration(Double(metric.requestedMS))
        let observed = metric.observedMS.map(cadenceDuration) ?? "Observing…"
        return MonitorStat(
            title: title,
            value: observed,
            detail: "Requested \(requested) · \(metric.observations ?? 0) intervals"
        )
    }

    private func cadenceDuration(_ milliseconds: Double) -> String {
        if milliseconds < 1_000 {
            return String(format: "%.0f ms", milliseconds)
        }
        return String(format: "%.2f s", milliseconds / 1_000)
    }

    private func niceValue(_ value: Int?) -> String {
        guard let value else { return "n/a" }
        return value > 0 ? "+\(value)" : "\(value)"
    }

    private func workloadNiceSummary(_ priority: BenchmarkPriorityObservation?) -> String {
        guard let priority else { return "n/a" }
        guard priority.supported else { return "Unsupported" }
        guard let workloads = priority.workloads, !workloads.isEmpty else {
            if let errors = priority.errors, !errors.isEmpty {
                return "Capture failed"
            }
            return "None"
        }
        return workloads.map { "\($0.label): \(niceValue($0.nice))" }.joined(separator: ", ")
    }

}
