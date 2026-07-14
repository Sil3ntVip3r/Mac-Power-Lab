import SwiftUI

struct DashboardView: View {
    let status: EngineStatus?

    private var sample: PowerSample? { status?.lastSample }

    private var topApps: [AppPower] {
        Array(
            (sample?.attribution?.apps ?? [])
                .sorted { $0.sortTotalW > $1.sortTotalW }
                .prefix(5)
        )
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                MonitorPageHeader(
                    title: "Overview",
                    subtitle: "The most important live power, battery, performance, and application signals.",
                    systemImage: "gauge.with.dots.needle.50percent"
                )

                if let sample {
                    LazyVGrid(
                        columns: [GridItem(.adaptive(minimum: 205))],
                        spacing: 12
                    ) {
                        MonitorMetricCard(
                            title: "Primary Load",
                            value: MPLFormat.watts(sample.primaryLoadW),
                            subtitle: MPLFormat.text(sample.primaryLoadSource),
                            systemImage: "bolt.fill",
                            emphasis: .orange
                        )
                        MonitorMetricCard(
                            title: "Battery",
                            value: MPLFormat.percent(sample.battery.percent),
                            subtitle: "\(MPLFormat.text(sample.battery.powerSource)) · \(MPLFormat.text(sample.battery.state))",
                            systemImage: "battery.75percent"
                        )
                        MonitorMetricCard(
                            title: "Battery Flow",
                            value: MPLFormat.watts(sample.battery.netW),
                            subtitle: "\(MPLFormat.volts(sample.battery.voltageV)) / \(MPLFormat.amps(sample.battery.currentA))",
                            systemImage: "arrow.left.arrow.right"
                        )
                        MonitorMetricCard(
                            title: "Temperature",
                            value: MPLFormat.celsius(sample.battery.temperatureC),
                            subtitle: MPLFormat.text(sample.thermal.summary),
                            systemImage: "thermometer.medium"
                        )
                        MonitorMetricCard(
                            title: "Charger Output",
                            value: MPLFormat.watts(sample.adapter.outputEstimateW),
                            subtitle: MPLFormat.text(sample.adapter.outputEstimateSource),
                            systemImage: "powerplug"
                        )
                        MonitorMetricCard(
                            title: "Charger Load",
                            value: MPLFormat.percent(sample.adapter.loadPercent),
                            subtitle: "Headroom \(MPLFormat.watts(sample.adapter.headroomW))",
                            systemImage: "bolt.horizontal.circle"
                        )
                        MonitorMetricCard(
                            title: "Baseline",
                            value: MPLFormat.watts(sample.baselineLoadW),
                            subtitle: "Learned quiet-system load",
                            systemImage: "waveform.path"
                        )
                        MonitorMetricCard(
                            title: "Current Phase",
                            value: MPLFormat.text(sample.phase),
                            subtitle: "Sequence \(sample.sequence.map(String.init) ?? "n/a")",
                            systemImage: "flag.fill"
                        )
                    }

                    MonitorSection("System Snapshot", systemImage: "cpu") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "CPU", value: MPLFormat.watts(sample.components.cpuW)),
                            MonitorStat(title: "GPU", value: MPLFormat.watts(sample.components.gpuW)),
                            MonitorStat(title: "Package", value: MPLFormat.watts(sample.components.packageW)),
                            MonitorStat(title: "CPU frequency", value: MPLFormat.megahertz(sample.components.cpuEstimateMHz)),
                            MonitorStat(title: "GPU frequency", value: MPLFormat.megahertz(sample.components.gpuMHz)),
                            MonitorStat(title: "Thermal pressure", value: MPLFormat.text(sample.thermal.macosPressure)),
                            MonitorStat(title: "Battery health", value: MPLFormat.percent(sample.battery.healthPercent)),
                            MonitorStat(title: "Cell delta", value: MPLFormat.millivolts(sample.battery.cellVoltageDeltaMV)),
                        ])
                    }

                    MonitorSection("Application Power Summary", systemImage: "app.badge") {
                        VStack(alignment: .leading, spacing: 10) {
                            MonitorStatGrid(items: [
                                MonitorStat(
                                    title: "Attribution method",
                                    value: MPLFormat.text(sample.attribution?.method)
                                ),
                                MonitorStat(
                                    title: "Confidence",
                                    value: MPLFormat.text(sample.attribution?.confidence)
                                ),
                                MonitorStat(
                                    title: "Dynamic app load",
                                    value: MPLFormat.watts(sample.attribution?.dynamicW)
                                ),
                                MonitorStat(
                                    title: "Tracked applications",
                                    value: String(sample.attribution?.apps?.count ?? 0)
                                ),
                            ])

                            if topApps.isEmpty {
                                Text("Application attribution is still collecting.")
                                    .foregroundStyle(.secondary)
                            } else {
                                Divider()
                                ForEach(Array(topApps.enumerated()), id: \.element.id) { index, app in
                                    HStack {
                                        Text("\(index + 1)")
                                            .font(.caption.bold())
                                            .frame(width: 24, height: 24)
                                            .background(.quaternary, in: Circle())
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(app.name)
                                                .fontWeight(.semibold)
                                            Text(app.category ?? app.attributionSource ?? "Application")
                                                .font(.caption)
                                                .foregroundStyle(.secondary)
                                        }
                                        Spacer()
                                        VStack(alignment: .trailing, spacing: 2) {
                                            Text(MPLFormat.watts(app.estimatedShareW))
                                                .fontWeight(.semibold)
                                                .monospacedDigit()
                                            Text("\(MPLFormat.wattHours(app.estimatedEnergyWh)) session")
                                                .font(.caption2)
                                                .foregroundStyle(.secondary)
                                        }
                                    }
                                    if index < topApps.count - 1 {
                                        Divider()
                                    }
                                }
                            }
                        }
                    }

                    if let warnings = sample.warnings, !warnings.isEmpty {
                        MonitorSection("Warnings", systemImage: "exclamationmark.triangle.fill") {
                            VStack(alignment: .leading, spacing: 8) {
                                ForEach(warnings, id: \.self) { warning in
                                    Label(warning, systemImage: "exclamationmark.triangle")
                                        .foregroundStyle(.orange)
                                }
                            }
                        }
                    }
                } else {
                    MonitorEmptyState(
                        title: "Monitor is not producing samples",
                        message: "Start the monitor or wait for the first battery and powermetrics sample.",
                        systemImage: "waveform.path.ecg"
                    )
                }
            }
            .padding()
        }
    }
}
