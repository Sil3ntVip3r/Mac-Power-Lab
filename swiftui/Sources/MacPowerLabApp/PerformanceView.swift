import SwiftUI

struct PerformanceView: View {
    let status: EngineStatus?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                MonitorPageHeader(
                    title: "Performance & Thermals",
                    subtitle: "SoC component power, CPU clusters, frequencies, thermal pressure, and collector health.",
                    systemImage: "cpu"
                )

                if let sample = status?.lastSample {
                    LazyVGrid(
                        columns: [GridItem(.adaptive(minimum: 205))],
                        spacing: 12
                    ) {
                        MonitorMetricCard(
                            title: "Primary Load",
                            value: MPLFormat.watts(sample.primaryLoadW),
                            subtitle: MPLFormat.text(sample.primaryLoadSource),
                            systemImage: "bolt.fill"
                        )
                        MonitorMetricCard(
                            title: "CPU",
                            value: MPLFormat.watts(sample.components.cpuW),
                            subtitle: MPLFormat.megahertz(sample.components.cpuEstimateMHz),
                            systemImage: "cpu"
                        )
                        MonitorMetricCard(
                            title: "GPU",
                            value: MPLFormat.watts(sample.components.gpuW),
                            subtitle: MPLFormat.megahertz(sample.components.gpuMHz),
                            systemImage: "gpu"
                        )
                        MonitorMetricCard(
                            title: "Package",
                            value: MPLFormat.watts(sample.components.packageW),
                            subtitle: "Reported SoC package estimate",
                            systemImage: "square.3.layers.3d"
                        )
                        MonitorMetricCard(
                            title: "Thermal Pressure",
                            value: MPLFormat.text(sample.thermal.macosPressure),
                            subtitle: MPLFormat.text(sample.thermal.summary),
                            systemImage: "thermometer.high"
                        )
                        MonitorMetricCard(
                            title: "Temperature Trend",
                            value: "\(MPLFormat.number(sample.thermal.batteryTrendCPerMin, digits: 2)) °C/min",
                            subtitle: MPLFormat.text(sample.thermal.batteryState),
                            systemImage: "chart.line.uptrend.xyaxis"
                        )
                    }

                    MonitorSection("SoC Components", systemImage: "square.3.layers.3d") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "CPU power", value: MPLFormat.watts(sample.components.cpuW)),
                            MonitorStat(title: "GPU power", value: MPLFormat.watts(sample.components.gpuW)),
                            MonitorStat(title: "ANE power", value: MPLFormat.watts(sample.components.aneW)),
                            MonitorStat(title: "DRAM power", value: MPLFormat.watts(sample.components.dramW)),
                            MonitorStat(title: "Package power", value: MPLFormat.watts(sample.components.packageW)),
                            MonitorStat(title: "CPU frequency estimate", value: MPLFormat.megahertz(sample.components.cpuEstimateMHz)),
                            MonitorStat(title: "CPU estimate source", value: MPLFormat.text(sample.components.cpuEstimateSource)),
                            MonitorStat(title: "GPU frequency", value: MPLFormat.megahertz(sample.components.gpuMHz)),
                        ])
                    }

                    if let clusters = sample.components.clusters, !clusters.isEmpty {
                        MonitorSection("CPU Clusters", systemImage: "cpu.fill") {
                            LazyVGrid(
                                columns: [GridItem(.adaptive(minimum: 210))],
                                spacing: 10
                            ) {
                                ForEach(clusters) { cluster in
                                    VStack(alignment: .leading, spacing: 7) {
                                        Text(cluster.name)
                                            .font(.title3.bold())
                                        LabeledContent(
                                            "Frequency",
                                            value: MPLFormat.megahertz(cluster.frequencyMHz)
                                        )
                                        LabeledContent(
                                            "Active",
                                            value: MPLFormat.percent(cluster.activePercent)
                                        )
                                        LabeledContent(
                                            "Power",
                                            value: MPLFormat.watts(cluster.powerW)
                                        )
                                    }
                                    .padding(12)
                                    .background(.quaternary.opacity(0.45), in: RoundedRectangle(cornerRadius: 8))
                                }
                            }
                        }
                    }

                    MonitorSection("Thermal Signals", systemImage: "thermometer.medium") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "macOS pressure", value: MPLFormat.text(sample.thermal.macosPressure)),
                            MonitorStat(title: "Battery thermal state", value: MPLFormat.text(sample.thermal.batteryState)),
                            MonitorStat(title: "Battery trend", value: "\(MPLFormat.number(sample.thermal.batteryTrendCPerMin, digits: 2)) °C/min"),
                            MonitorStat(title: "Combined summary", value: MPLFormat.text(sample.thermal.summary)),
                            MonitorStat(title: "Thermal source", value: MPLFormat.text(sample.thermal.source)),
                            MonitorStat(title: "Battery temperature", value: MPLFormat.celsius(sample.battery.temperatureC)),
                        ])
                    }

                    MonitorSection("Collector Health", systemImage: "checkmark.shield") {
                        if let collectorStatus = sample.collectorStatus, !collectorStatus.isEmpty {
                            MonitorStatGrid(
                                items: collectorStatus.keys.sorted().map { key in
                                    MonitorStat(title: key, value: collectorStatus[key] ?? "n/a")
                                }
                            )
                        } else {
                            Text("No collector status details are currently reported.")
                                .foregroundStyle(.secondary)
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
                        title: "No performance sample yet",
                        message: "Start the monitor to populate SoC and thermal statistics.",
                        systemImage: "cpu"
                    )
                }
            }
            .padding()
        }
    }
}
