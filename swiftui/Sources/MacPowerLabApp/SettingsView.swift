import Foundation
import SwiftUI

struct SettingsView: View {
    @ObservedObject var model: AppModel
    @State private var draft = RuntimeSettings.compatibilityDefault

    var body: some View {
        Form {
            runtimeProfileSection
            cadenceSection
            cadenceDiagnosticsSection
            loggingSection
            prioritySection
            applySection
            engineSection
            capabilitiesSection
            privacySection
        }
        .formStyle(.grouped)
        .padding()
        .task {
            if model.status == nil {
                model.connect()
            }
            synchronizeDraft(model.runtimeSettings)
        }
        .onChange(of: model.runtimeSettings) { _, settings in
            synchronizeDraft(settings)
        }
    }

    private var runtimeProfileSection: some View {
        Section("Runtime profile") {
            Picker("Profile", selection: profileBinding) {
                ForEach(model.runtimeProfiles) { profile in
                    Text(profile.name).tag(profile.id)
                }
            }
            .disabled(settingsUnavailable)

            if let profile = selectedProfileDefinition {
                Text(profile.description)
                    .foregroundStyle(.secondary)
            }

            Text(
                "Changing an individual control selects Custom. Applying settings while monitoring "
                + "is active flushes pending data and starts a fresh session."
            )
            .font(.callout)
            .foregroundStyle(.secondary)
        }
    }

    private var cadenceSection: some View {
        Section("Live view and collection") {
            durationControl(
                "UI refresh",
                detail: "How often the dashboard receives the latest live state.",
                keyPath: \.uiRefreshMS,
                range: 500...60_000,
                step: 500
            )
            durationControl(
                "Battery collection",
                detail: "How often battery and adapter sensors are collected.",
                keyPath: \.batteryCollectionMS,
                range: 500...60_000,
                step: 500
            )
            durationControl(
                "powermetrics",
                detail: "CPU, GPU, thermal, and package-power collection cadence.",
                keyPath: \.powermetricsMS,
                range: 1_000...60_000,
                step: 1_000
            )
            durationControl(
                "App attribution",
                detail: "How often per-application activity is collected.",
                keyPath: \.appAttributionMS,
                range: 2_000...60_000,
                step: 1_000
            )
        }
        .disabled(settingsUnavailable)
    }

    private var cadenceDiagnosticsSection: some View {
        Section("Cadence diagnostics") {
            cadenceRow(
                "SwiftUI status polling",
                requestedMS: model.runtimeSettings?.uiRefreshMS,
                observedMS: model.observedUIRefreshMS,
                observations: nil
            )
            cadenceRow("Backend live publication", metric: model.status?.cadence?.uiRefresh)
            cadenceRow("Battery collection", metric: model.status?.cadence?.batteryCollection)
            cadenceRow("powermetrics", metric: model.status?.cadence?.powermetrics)
            cadenceRow("Application attribution", metric: model.status?.cadence?.appAttribution)
            cadenceRow("Durable logging", metric: model.status?.cadence?.durableLogging)
            LabeledContent(
                "Backend live publications",
                value: String(model.status?.cadence?.livePublications ?? 0)
            )
            LabeledContent(
                "Client-skipped publications",
                value: String(model.missedLivePublications)
            )
            LabeledContent(
                "Replaced stream frames",
                value: String(model.status?.cadence?.replacedStreamFrames ?? 0)
            )

            Text(
                "Observed values are smoothed delivery intervals. Client-skipped publications show "
                + "how many backend live updates arrived between successful SwiftUI polls. Replaced "
                + "stream frames apply to channel consumers such as the terminal monitor; MacPowerLab "
                + "keeps the newest value rather than building a backlog."
            )
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }

    private var loggingSection: some View {
        Section("Durable history") {
            Toggle("Log power and app samples", isOn: loggingBinding)
                .disabled(settingsUnavailable)

            if draft.loggingEnabled {
                durationControl(
                    "Logging cadence",
                    detail: "Only the latest pending live sample is retained between durable writes.",
                    keyPath: \.logIntervalMS,
                    range: 500...60_000,
                    step: 500
                )
                .disabled(settingsUnavailable)
            } else {
                Text(
                    "Live-only mode keeps the dashboard active but does not append power or "
                    + "application sample rows. Historical reports are unavailable until durable "
                    + "logging is enabled and a new reportable session begins. Session metadata, "
                    + "events, and benchmark results remain available."
                )
                .foregroundStyle(.secondary)
            }
        }
    }

    private var prioritySection: some View {
        Section("Process priority") {
            Stepper(value: processNiceBinding, in: -5...10, step: 1) {
                LabeledContent("Ordinary nice value", value: signedNice(draft.processNice))
            }
            .disabled(settingsUnavailable)

            Text(
                "-5 favors responsiveness; +10 lowers overhead. MacPowerLab uses ordinary macOS "
                + "niceness only—never kernel real-time scheduling. Benchmark children run at nice 0 "
                + "so profile choice does not distort benchmark workloads."
            )
            .font(.callout)
            .foregroundStyle(.secondary)
        }
    }

    private var applySection: some View {
        Section {
            if model.status?.benchmark.running == true {
                Label(
                    "Finish or stop the benchmark before applying runtime settings.",
                    systemImage: "exclamationmark.triangle"
                )
                .foregroundStyle(.orange)
            }
            if let validationMessage = draft.validationMessage {
                Label(validationMessage, systemImage: "xmark.circle")
                    .foregroundStyle(.red)
            }
            if let message = model.settingsMessage {
                HStack {
                    Label(message, systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    Spacer()
                    Button("Dismiss") {
                        model.dismissSettingsMessage()
                    }
                }
            }

            HStack {
                Button("Revert") {
                    synchronizeDraft(model.runtimeSettings)
                }
                .disabled(model.runtimeSettings == nil || draft == model.runtimeSettings)

                Spacer()

                Button(model.isApplyingSettings ? "Applying…" : "Apply Settings") {
                    model.applyRuntimeSettings(draft)
                }
                .buttonStyle(.borderedProminent)
                .disabled(!canApply)
            }
        }
    }

    private var engineSection: some View {
        Section("Engine") {
            LabeledContent("Version", value: model.status?.version ?? "n/a")
            LabeledContent(
                "Monitor",
                value: model.status?.monitorRunning == true ? "Running" : "Stopped"
            )
            LabeledContent("Session", value: model.status?.session?.id ?? "n/a")
            LabeledContent(
                "Started",
                value: MPLFormat.date(model.status?.session?.startedAt)
            )
            LabeledContent(
                "macOS",
                value: [
                    model.status?.session?.osVersion,
                    model.status?.session?.osBuild,
                ]
                .compactMap { $0 }
                .joined(separator: " / ")
            )
            LabeledContent(
                "Hardware",
                value: [
                    model.status?.session?.machine,
                    model.status?.session?.chip,
                ]
                .compactMap { $0 }
                .joined(separator: " / ")
            )
            LabeledContent(
                "Data directory",
                value: model.status?.session?.dataDirectory ?? "n/a"
            )
        }
    }

    @ViewBuilder
    private var capabilitiesSection: some View {
        if let capabilities = model.status?.capabilities, !capabilities.isEmpty {
            Section("Capabilities") {
                ForEach(capabilities.keys.sorted(), id: \.self) { key in
                    LabeledContent(
                        key,
                        value: capabilities[key] == true ? "Available" : "Unavailable"
                    )
                }
            }
        }

        if let errors = model.status?.errors, !errors.isEmpty {
            Section("Engine errors") {
                ForEach(errors, id: \.self) { error in
                    Text(error)
                        .foregroundStyle(.red)
                        .textSelection(.enabled)
                }
            }
        }
    }

    private var privacySection: some View {
        Section("Privacy and accuracy") {
            Text(
                "All collection stays on this Mac. The local API listens only on loopback and uses "
                + "a private bearer token. Per-app watts are confidence-labelled attribution estimates "
                + "calibrated to total power, not direct electrical measurements."
            )
            .foregroundStyle(.secondary)
        }
    }

    private var selectedProfileDefinition: RuntimeProfileDefinition? {
        model.runtimeProfiles.first { $0.id == draft.profile }
    }

    private var settingsUnavailable: Bool {
        model.runtimeSettings == nil || model.isApplyingSettings
    }

    private var canApply: Bool {
        guard let current = model.runtimeSettings else { return false }
        return !model.isApplyingSettings
            && model.status?.benchmark.running != true
            && draft.validationMessage == nil
            && draft != current
    }

    private var profileBinding: Binding<String> {
        Binding(
            get: { draft.profile },
            set: { profileID in
                if profileID == RuntimeSettings.customProfile {
                    draft.profile = RuntimeSettings.customProfile
                } else if let profile = model.runtimeProfiles.first(where: { $0.id == profileID }) {
                    draft = profile.settings
                }
            }
        )
    }

    private var loggingBinding: Binding<Bool> {
        Binding(
            get: { draft.loggingEnabled },
            set: { enabled in
                draft.loggingEnabled = enabled
                draft.logIntervalMS = enabled ? max(draft.logIntervalMS, 1_000) : 0
                draft.profile = RuntimeSettings.customProfile
            }
        )
    }

    private var processNiceBinding: Binding<Int> {
        Binding(
            get: { draft.processNice },
            set: { value in
                draft.processNice = value
                draft.profile = RuntimeSettings.customProfile
            }
        )
    }

    private func durationBinding(
        _ keyPath: WritableKeyPath<RuntimeSettings, Int64>
    ) -> Binding<Int64> {
        Binding(
            get: { draft[keyPath: keyPath] },
            set: { value in
                draft[keyPath: keyPath] = value
                draft.profile = RuntimeSettings.customProfile
            }
        )
    }

    private func durationControl(
        _ title: String,
        detail: String,
        keyPath: WritableKeyPath<RuntimeSettings, Int64>,
        range: ClosedRange<Int64>,
        step: Int
    ) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Stepper(value: durationBinding(keyPath), in: range, step: step) {
                LabeledContent(title, value: durationLabel(draft[keyPath: keyPath]))
            }
            Text(detail)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private func cadenceRow(_ title: String, metric: CadenceMetric?) -> some View {
        cadenceRow(
            title,
            requestedMS: metric?.requestedMS,
            observedMS: metric?.observedMS,
            observations: metric?.observations
        )
    }

    private func cadenceRow(
        _ title: String,
        requestedMS: Int64?,
        observedMS: Double?,
        observations: UInt64?
    ) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            LabeledContent(
                title,
                value: cadenceLabel(requestedMS: requestedMS, observedMS: observedMS)
            )
            if let observations {
                Text("\(observations) measured intervals")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private func cadenceLabel(requestedMS: Int64?, observedMS: Double?) -> String {
        let requested = requestedMS.map(durationLabel) ?? "off"
        guard let observedMS, observedMS > 0 else {
            return "Requested \(requested) · Observing…"
        }
        return "Requested \(requested) · Observed \(durationLabel(Int64(observedMS.rounded())))"
    }

    private func synchronizeDraft(_ settings: RuntimeSettings?) {
        guard let settings else { return }
        draft = settings
    }

    private func durationLabel(_ milliseconds: Int64) -> String {
        if milliseconds < 1_000 {
            return "\(milliseconds) ms"
        }
        if milliseconds.isMultiple(of: 1_000) {
            return "\(milliseconds / 1_000) s"
        }
        return String(format: "%.1f s", Double(milliseconds) / 1_000)
    }

    private func signedNice(_ value: Int) -> String {
        value > 0 ? "+\(value)" : "\(value)"
    }
}
