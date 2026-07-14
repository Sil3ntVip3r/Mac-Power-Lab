import SwiftUI

struct SettingsView: View {
    let status: EngineStatus?

    var body: some View {
        Form {
            Section("Engine") {
                LabeledContent("Version", value: status?.version ?? "n/a")
                LabeledContent(
                    "Monitor",
                    value: status?.monitorRunning == true ? "Running" : "Stopped"
                )
                LabeledContent("Session", value: status?.session?.id ?? "n/a")
                LabeledContent(
                    "Started",
                    value: MPLFormat.date(status?.session?.startedAt)
                )
                LabeledContent(
                    "macOS",
                    value: [
                        status?.session?.osVersion,
                        status?.session?.osBuild,
                    ]
                    .compactMap { $0 }
                    .joined(separator: " / ")
                )
                LabeledContent(
                    "Hardware",
                    value: [
                        status?.session?.machine,
                        status?.session?.chip,
                    ]
                    .compactMap { $0 }
                    .joined(separator: " / ")
                )
                LabeledContent(
                    "Data directory",
                    value: status?.session?.dataDirectory ?? "n/a"
                )
            }

            Section("Capabilities") {
                ForEach(
                    (status?.capabilities ?? [:]).keys.sorted(),
                    id: \.self
                ) { key in
                    LabeledContent(
                        key,
                        value: status?.capabilities?[key] == true
                            ? "Available"
                            : "Unavailable"
                    )
                }
            }

            if let errors = status?.errors, !errors.isEmpty {
                Section("Engine errors") {
                    ForEach(errors, id: \.self) { error in
                        Text(error)
                            .foregroundStyle(.red)
                            .textSelection(.enabled)
                    }
                }
            }

            Section("Privacy and accuracy") {
                Text(
                    "All collection stays on this Mac. The local API listens "
                    + "only on loopback and uses a private bearer token. Per-app "
                    + "watts are confidence-labelled attribution estimates "
                    + "calibrated to total power, not direct electrical measurements."
                )
                .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}
