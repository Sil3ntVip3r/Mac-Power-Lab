import SwiftUI

struct BenchmarkView: View {
    @ObservedObject var model: AppModel

    private var progress: BenchmarkProgress? {
        model.status?.benchmark
    }

    private var selectedDefinition: BenchmarkDefinition? {
        model.selectedDefinition
    }

    private var canStart: Bool {
        progress?.running != true
            && (model.selectedBenchmark != "custom" || model.customIsValid)
    }

    var body: some View {
        Form {
            Section("Choose a benchmark") {
                Picker("Benchmark", selection: $model.selectedBenchmark) {
                    ForEach(model.benchmarkCatalog) { definition in
                        Label(definition.name, systemImage: definition.icon)
                            .tag(definition.id)
                    }
                    Divider()
                    Label("Custom benchmark", systemImage: "slider.horizontal.3")
                        .tag("custom")
                }
                .pickerStyle(.menu)
                .onChange(of: model.selectedBenchmark) { _, identifier in
                    model.applyBenchmarkDefaults(identifier)
                }

                if model.selectedBenchmark == "custom" {
                    CustomBenchmarkEditor(model: model)
                } else if let definition = selectedDefinition {
                    BenchmarkExplanationCard(
                        definition: definition,
                        durationMinutes: $model.presetDurationMinutes
                    )
                }
            }

            Section("Progress") {
                ProgressView(value: progress?.percent ?? 0, total: 100)

                HStack {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(progress?.phase ?? "Not running")
                            .font(.headline)
                        if let phaseIndex = progress?.phaseIndex,
                           let phaseCount = progress?.phaseCount,
                           phaseCount > 0 {
                            Text("Phase \(phaseIndex) of \(phaseCount)")
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer()
                    VStack(alignment: .trailing, spacing: 4) {
                        Text(String(format: "%.1f%%", progress?.percent ?? 0))
                            .monospacedDigit()
                        if progress?.running == true {
                            Text(
                                "\(formatDuration(progress?.elapsedSeconds)) elapsed · "
                                + "\(formatDuration(progress?.remainingSeconds)) remaining"
                            )
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                        }
                    }
                }

                HStack {
                    Button {
                        model.startBenchmark()
                    } label: {
                        Label(
                            model.selectedBenchmark == "custom"
                                ? "Start custom benchmark"
                                : "Start \(selectedDefinition?.name ?? "benchmark")",
                            systemImage: "play.fill"
                        )
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(!canStart)

                    Button("Stop", role: .destructive) {
                        model.stopBenchmark()
                    }
                    .disabled(progress?.running != true)
                }

                if model.selectedBenchmark == "custom" && !model.customIsValid {
                    Label(
                        "Select at least one workload: CPU, GPU, or memory.",
                        systemImage: "exclamationmark.triangle"
                    )
                    .foregroundStyle(.orange)
                }
            }

            Section("How results are interpreted") {
                Text(
                    "MacPowerLab records total system power, battery or charger flow, "
                    + "CPU/GPU components, thermal pressure, cluster frequency, and "
                    + "application activity during every phase. Battery tests use real "
                    + "battery discharge watts as total load. Per-app watts are "
                    + "confidence-labelled attribution estimates."
                )
                .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .padding()
    }

    private func formatDuration(_ seconds: Double?) -> String {
        guard let seconds, seconds.isFinite, seconds >= 0 else { return "n/a" }
        let total = Int(seconds.rounded())
        if total >= 3600 {
            return String(format: "%dh %02dm", total / 3600, (total % 3600) / 60)
        }
        if total >= 60 {
            return String(format: "%dm %02ds", total / 60, total % 60)
        }
        return "\(total)s"
    }
}

private struct BenchmarkExplanationCard: View {
    let definition: BenchmarkDefinition
    @Binding var durationMinutes: Double

    private var totalDurationMinutes: Double {
        definition.adjustableDuration
            ? durationMinutes + cooldownMinutes
            : definition.typicalDurationSeconds / 60
    }

    private var cooldownMinutes: Double {
        guard definition.id == "thermal" else { return 0 }
        return min(10, max(2, durationMinutes / 2))
    }

    var body: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 14) {
                    Image(systemName: definition.icon)
                        .font(.system(size: 30))
                        .foregroundStyle(.tint)
                        .frame(width: 42)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(definition.name)
                            .font(.title3.bold())
                        Text(definition.summary)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    VStack(alignment: .trailing, spacing: 4) {
                        Text(definition.intensity)
                            .font(.caption.bold())
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(.quaternary, in: Capsule())
                        Text(formatMinutes(totalDurationMinutes))
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }

                Text(definition.description)

                HStack(spacing: 16) {
                    Label(
                        definition.requiredPowerSource?.isEmpty == false
                            ? definition.requiredPowerSource!
                            : "AC or battery",
                        systemImage: powerIcon(definition.requiredPowerSource)
                    )
                    Label(definition.category, systemImage: "square.grid.2x2")
                    Label(
                        "\(definition.phases.count) phase\(definition.phases.count == 1 ? "" : "s")",
                        systemImage: "list.number"
                    )
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)

                if definition.adjustableDuration {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text(definition.id == "thermal" ? "Stress duration" : "Duration")
                            Spacer()
                            Text(formatMinutes(durationMinutes))
                                .monospacedDigit()
                        }
                        Slider(
                            value: $durationMinutes,
                            in: sliderRange,
                            step: 1
                        )
                    }
                }

                if !definition.phases.isEmpty {
                    Divider()
                    Text("Phases")
                        .font(.headline)
                    ForEach(Array(definition.phases.enumerated()), id: \.element.id) { index, phase in
                        HStack(alignment: .top, spacing: 10) {
                            Text("\(index + 1)")
                                .font(.caption.bold())
                                .frame(width: 22, height: 22)
                                .background(.quaternary, in: Circle())
                            VStack(alignment: .leading, spacing: 2) {
                                Text(phase.name)
                                    .fontWeight(.semibold)
                                Text(phase.description)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            Text(formatSeconds(phaseDuration(phase)))
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                DetailList(title: "Best for", values: definition.bestFor)
                DetailList(title: "Measures", values: definition.metrics)

                if let safety = definition.safetyNotes, !safety.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Label("Before starting", systemImage: "checklist")
                            .font(.headline)
                        ForEach(safety, id: \.self) { note in
                            Label(note, systemImage: "exclamationmark.triangle")
                                .font(.caption)
                                .foregroundStyle(.orange)
                        }
                    }
                }
            }
            .padding(4)
        }
    }

    private var sliderRange: ClosedRange<Double> {
        let minimum = max(1, (definition.minimumDurationSeconds ?? 60) / 60)
        let maximum = max(minimum, (definition.maximumDurationSeconds ?? 7200) / 60)
        return minimum...maximum
    }

    private func phaseDuration(_ phase: BenchmarkPhaseDefinition) -> Double {
        if definition.adjustableDuration && definition.phases.count == 1 {
            return durationMinutes * 60
        }
        if definition.id == "thermal" {
            if phase.kind == "extreme" {
                return durationMinutes * 60
            }
            if phase.kind == "idle" {
                return cooldownMinutes * 60
            }
        }
        return phase.durationSeconds
    }

    private func powerIcon(_ source: String?) -> String {
        switch source {
        case "Battery Power": return "battery.50percent"
        case "AC Power": return "powerplug"
        default: return "arrow.triangle.2.circlepath"
        }
    }

    private func formatMinutes(_ minutes: Double) -> String {
        if minutes >= 60 {
            return String(format: "%.1f hr", minutes / 60)
        }
        return "\(Int(minutes.rounded())) min"
    }

    private func formatSeconds(_ seconds: Double) -> String {
        let minutes = Int((seconds / 60).rounded())
        return minutes > 0 ? "\(minutes) min" : "\(Int(seconds.rounded())) sec"
    }
}

private struct DetailList: View {
    let title: String
    let values: [String]

    var body: some View {
        if !values.isEmpty {
            VStack(alignment: .leading, spacing: 5) {
                Text(title)
                    .font(.headline)
                ForEach(values, id: \.self) { value in
                    Label(value, systemImage: "checkmark.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

private struct CustomBenchmarkEditor: View {
    @ObservedObject var model: AppModel

    var body: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 16) {
                Text("Build a custom benchmark")
                    .font(.title3.bold())
                Text(
                    "Choose any combination of CPU, GPU, and memory load. "
                    + "Optional idle phases establish a baseline and measure recovery."
                )
                .foregroundStyle(.secondary)

                TextField("Benchmark name", text: $model.customName)

                Picker("Required power source", selection: $model.customPowerSource) {
                    Text("AC or battery").tag("any")
                    Text("Battery Power only").tag("battery")
                    Text("AC Power only").tag("ac")
                }

                GroupBox("Workloads") {
                    VStack(alignment: .leading, spacing: 10) {
                        Toggle(isOn: $model.customCPU) {
                            Label("CPU — load all CPU cores", systemImage: "cpu")
                        }
                        Toggle(isOn: $model.customGPU) {
                            Label("GPU — Metal compute workload", systemImage: "gpu")
                        }
                        Toggle(isOn: $model.customMemory) {
                            Label("Memory — unified-memory bandwidth", systemImage: "memorychip")
                        }
                    }
                    .padding(4)
                }

                if model.customGPU {
                    Picker("GPU intensity", selection: $model.customGPUProfile) {
                        Text("Normal").tag("normal")
                        Text("High").tag("high")
                        Text("Extreme").tag("extreme")
                    }
                    .pickerStyle(.segmented)
                }

                if model.customMemory {
                    Toggle("Automatic memory allocation", isOn: $model.customMemoryAutomatic)
                    if !model.customMemoryAutomatic {
                        HStack {
                            Text("Memory allocation")
                            Slider(
                                value: $model.customMemoryMB,
                                in: 512...65536,
                                step: 512
                            )
                            Text("\(Int(model.customMemoryMB)) MB")
                                .monospacedDigit()
                                .frame(width: 90, alignment: .trailing)
                        }
                    }
                }

                DurationSlider(
                    title: "Workload duration",
                    value: $model.customWorkloadMinutes,
                    range: 1...120
                )

                Toggle("Include idle baseline", isOn: $model.customBaselineEnabled)
                if model.customBaselineEnabled {
                    DurationSlider(
                        title: "Baseline duration",
                        value: $model.customBaselineMinutes,
                        range: 1...30
                    )
                }

                Toggle("Include monitored cooldown", isOn: $model.customCooldownEnabled)
                if model.customCooldownEnabled {
                    DurationSlider(
                        title: "Cooldown duration",
                        value: $model.customCooldownMinutes,
                        range: 1...30
                    )
                }

                HStack {
                    Label(
                        "Estimated total duration",
                        systemImage: "clock"
                    )
                    Spacer()
                    Text("\(Int(model.customEstimatedMinutes.rounded())) min")
                        .fontWeight(.semibold)
                        .monospacedDigit()
                }
            }
            .padding(4)
        }
    }
}

private struct DurationSlider: View {
    let title: String
    @Binding var value: Double
    let range: ClosedRange<Double>

    var body: some View {
        HStack {
            Text(title)
            Slider(value: $value, in: range, step: 1)
            Text("\(Int(value)) min")
                .monospacedDigit()
                .frame(width: 68, alignment: .trailing)
        }
    }
}
