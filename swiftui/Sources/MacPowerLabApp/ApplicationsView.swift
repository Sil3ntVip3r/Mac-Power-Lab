import SwiftUI

private enum ApplicationTableMode: String, CaseIterable, Identifiable {
    case power = "Power"
    case activity = "Activity"
    case io = "I/O"
    case identity = "Identity"

    var id: String { rawValue }
}

struct ApplicationsView: View {
    let status: EngineStatus?

    @State private var mode: ApplicationTableMode = .power
    @State private var search = ""
    @State private var hideZeroUse = false
    @State private var sortOrder = [
        KeyPathComparator(\AppPower.sortTotalW, order: .reverse)
    ]

    private var apps: [AppPower] {
        let source = status?.lastSample?.attribution?.apps ?? []
        let filtered = source.filter { app in
            let matchesSearch = search.isEmpty
                || app.name.localizedCaseInsensitiveContains(search)
                || (app.category?.localizedCaseInsensitiveContains(search) ?? false)
                || (app.attributionSource?.localizedCaseInsensitiveContains(search) ?? false)
            let matchesUse = !hideZeroUse
                || app.sortTotalW > 0.005
                || app.sortDynamicW > 0.005
                || app.sortEnergyImpact > 0
                || app.sortCPUTime > 0
                || app.sortGPUTime > 0
            return matchesSearch && matchesUse
        }
        return filtered.sorted(using: sortOrder)
    }

    private var attribution: AttributionResult? {
        status?.lastSample?.attribution
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            MonitorPageHeader(
                title: "Applications",
                subtitle: "Sortable app and process-coalition power attribution, activity, wakeups, disk, and network use.",
                systemImage: "app.badge"
            )

            HStack(spacing: 10) {
                MonitorMetricCard(
                    title: "Method",
                    value: MPLFormat.text(attribution?.method),
                    subtitle: MPLFormat.text(attribution?.confidence),
                    systemImage: "function"
                )
                MonitorMetricCard(
                    title: "Baseline",
                    value: MPLFormat.watts(attribution?.baselineW),
                    subtitle: "Learned quiet-system load",
                    systemImage: "waveform.path"
                )
                MonitorMetricCard(
                    title: "Dynamic App Load",
                    value: attributedWatts(attribution?.dynamicW),
                    subtitle: "Load allocated above baseline",
                    systemImage: "bolt.badge.clock"
                )
                MonitorMetricCard(
                    title: "Tracked Rows",
                    value: String(apps.count),
                    subtitle: "\(status?.lastSample?.attribution?.apps?.count ?? 0) available before filtering",
                    systemImage: "list.number"
                )
            }
            .frame(maxHeight: 116)

            HStack {
                Picker("Columns", selection: $mode) {
                    ForEach(ApplicationTableMode.allCases) { mode in
                        Text(mode.rawValue).tag(mode)
                    }
                }
                .pickerStyle(.segmented)
                .frame(maxWidth: 420)
                .onChange(of: mode) { _, newMode in
                    switch newMode {
                    case .power:
                        sortOrder = [KeyPathComparator(\AppPower.sortTotalW, order: .reverse)]
                    case .activity:
                        sortOrder = [KeyPathComparator(\AppPower.sortEnergyImpact, order: .reverse)]
                    case .io:
                        sortOrder = [KeyPathComparator(\AppPower.sortDiskRead, order: .reverse)]
                    case .identity:
                        sortOrder = [KeyPathComparator(\AppPower.sortName)]
                    }
                }

                TextField("Filter applications", text: $search)
                    .textFieldStyle(.roundedBorder)
                    .frame(maxWidth: 280)

                Toggle("Hide zero-use rows", isOn: $hideZeroUse)
                    .toggleStyle(.checkbox)

                Spacer()

                Text("Click a column heading to sort")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Divider()

            if apps.isEmpty {
                MonitorEmptyState(
                    title: search.isEmpty ? "No application data yet" : "No matching applications",
                    message: "Application attribution updates independently from the one-second monitor sample.",
                    systemImage: "bolt.horizontal"
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                applicationTable
                    .id(mode)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .layoutPriority(1)
            }

            Text(
                "Estimated watts are confidence-labelled attribution values. "
                + "Dynamic W represents app-caused load above baseline; Total W also "
                + "includes a proportional share of platform baseline power. "
                + "A displayed 0.00 W is a valid zero allocation; n/a means attribution is unavailable."
            )
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .padding()
    }

    @ViewBuilder
    private var applicationTable: some View {
        switch mode {
        case .power:
            Table(apps, sortOrder: $sortOrder) {
                appNameColumn
                TableColumn("Dynamic W", value: \.sortDynamicW) { app in
                    Text(attributedWatts(app.estimatedDynamicW)).monospacedDigit()
                }
                .width(min: 90, ideal: 105)
                TableColumn("Total W", value: \.sortTotalW) { app in
                    Text(MPLFormat.watts(app.estimatedShareW)).monospacedDigit()
                }
                .width(min: 90, ideal: 105)
                TableColumn("CPU W", value: \.sortCPUW) { app in
                    Text(attributedWatts(app.estimatedCPUW)).monospacedDigit()
                }
                .width(min: 80, ideal: 95)
                TableColumn("GPU W", value: \.sortGPUW) { app in
                    Text(attributedWatts(app.estimatedGPUW)).monospacedDigit()
                }
                .width(min: 80, ideal: 95)
                TableColumn("Residual W", value: \.sortResidualW) { app in
                    Text(attributedWatts(app.estimatedResidualW)).monospacedDigit()
                }
                .width(min: 90, ideal: 105)
                TableColumn("Energy Wh", value: \.sortEnergyWh) { app in
                    Text(MPLFormat.number(app.estimatedEnergyWh, digits: 4)).monospacedDigit()
                }
                .width(min: 90, ideal: 105)
                TableColumn("Share", value: \.sortEnergyShare) { app in
                    Text(MPLFormat.percent(app.energySharePercent)).monospacedDigit()
                }
                .width(min: 75, ideal: 90)
                TableColumn("Confidence", value: \.confidenceRank) { app in
                    Text(app.confidence.capitalized)
                }
                .width(min: 85, ideal: 100)
            }
            .tableStyle(.inset(alternatesRowBackgrounds: true))

        case .activity:
            Table(apps, sortOrder: $sortOrder) {
                appNameColumn
                TableColumn("Energy Impact", value: \.sortEnergyImpact) { app in
                    Text(MPLFormat.number(app.energyImpact, digits: 2)).monospacedDigit()
                }
                TableColumn("CPU ms/s", value: \.sortCPUTime) { app in
                    Text(MPLFormat.number(app.cpuTimeMSPerS, digits: 1)).monospacedDigit()
                }
                TableColumn("GPU ms/s", value: \.sortGPUTime) { app in
                    Text(MPLFormat.number(app.gpuTimeMSPerS, digits: 1)).monospacedDigit()
                }
                TableColumn("Interrupt wakeups/s", value: \.sortInterruptWakeups) { app in
                    Text(MPLFormat.number(app.interruptWakeupsPerS, digits: 1)).monospacedDigit()
                }
                TableColumn("Idle wakeups/s", value: \.sortIdleWakeups) { app in
                    Text(MPLFormat.number(app.idleWakeupsPerS, digits: 1)).monospacedDigit()
                }
                TableColumn("Confidence", value: \.confidenceRank) { app in
                    Text(app.confidence.capitalized)
                }
            }
            .tableStyle(.inset(alternatesRowBackgrounds: true))

        case .io:
            Table(apps, sortOrder: $sortOrder) {
                appNameColumn
                TableColumn("Disk read", value: \.sortDiskRead) { app in
                    Text(MPLFormat.bytesPerSecond(app.diskReadBytesPerS)).monospacedDigit()
                }
                TableColumn("Disk write", value: \.sortDiskWrite) { app in
                    Text(MPLFormat.bytesPerSecond(app.diskWriteBytesPerS)).monospacedDigit()
                }
                TableColumn("Network receive", value: \.sortNetworkRead) { app in
                    Text(MPLFormat.bytesPerSecond(app.networkReadBytesPerS)).monospacedDigit()
                }
                TableColumn("Network send", value: \.sortNetworkWrite) { app in
                    Text(MPLFormat.bytesPerSecond(app.networkWriteBytesPerS)).monospacedDigit()
                }
                TableColumn("Energy Impact", value: \.sortEnergyImpact) { app in
                    Text(MPLFormat.number(app.energyImpact, digits: 2)).monospacedDigit()
                }
            }
            .tableStyle(.inset(alternatesRowBackgrounds: true))

        case .identity:
            Table(apps, sortOrder: $sortOrder) {
                appNameColumn
                TableColumn("Category", value: \.sortCategory) { app in
                    Text(app.category ?? "n/a")
                }
                TableColumn("PID", value: \.sortPID) { app in
                    Text(MPLFormat.integer(app.pid)).monospacedDigit()
                }
                TableColumn("Responsible PID", value: \.sortResponsiblePID) { app in
                    Text(MPLFormat.integer(app.responsiblePID)).monospacedDigit()
                }
                TableColumn("Coalition", value: \.sortCoalitionID) { app in
                    Text(MPLFormat.integer64(app.coalitionID)).monospacedDigit()
                }
                TableColumn("Attribution source", value: \.sortAttributionSource) { app in
                    Text(app.attributionSource ?? "n/a")
                }
                TableColumn("Confidence", value: \.confidenceRank) { app in
                    Text(app.confidence.capitalized)
                }
            }
            .tableStyle(.inset(alternatesRowBackgrounds: true))
        }
    }

    private func attributedWatts(_ value: Double?) -> String {
        guard attribution != nil else { return "n/a" }
        // Go v1 contracts omit numeric zero values. Once attribution exists, a
        // missing computed watt field therefore represents a real zero.
        return MPLFormat.watts(value ?? 0)
    }

    private var appNameColumn: TableColumn<AppPower, KeyPathComparator<AppPower>, some View, Text> {
        TableColumn("Application", value: \.sortName) { app in
            VStack(alignment: .leading, spacing: 1) {
                Text(app.name)
                    .lineLimit(1)
                if let category = app.category, !category.isEmpty {
                    Text(category)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .width(min: 185, ideal: 260)
    }
}
