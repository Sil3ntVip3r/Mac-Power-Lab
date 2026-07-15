import AppKit
import Foundation
import SwiftUI

private enum ReportPresentationError: LocalizedError {
    case artifactMissing(String)

    var errorDescription: String? {
        switch self {
        case .artifactMissing(let path):
            return "The backend completed report generation, but the timestamped HTML report was not found at \(path)."
        }
    }
}

@MainActor
final class AppModel: ObservableObject {
    @Published var status: EngineStatus?
    @Published var isConnecting = false
    @Published var errorMessage: String?
    @Published private(set) var isGeneratingReport = false
    @Published private(set) var reportMessage: String?
    @Published private(set) var latestReportURL: URL?

    @Published var benchmarkCatalog: [BenchmarkDefinition] = BenchmarkDefinition.fallbackCatalog
    @Published var selectedBenchmark = "quick"
    @Published var presetDurationMinutes = 15.0

    @Published var customName = "Custom workload"
    @Published var customPowerSource = "any"
    @Published var customCPU = true
    @Published var customGPU = true
    @Published var customMemory = true
    @Published var customGPUProfile = "high"
    @Published var customMemoryAutomatic = true
    @Published var customMemoryMB = 8192.0
    @Published var customWorkloadMinutes = 5.0
    @Published var customBaselineEnabled = true
    @Published var customBaselineMinutes = 1.0
    @Published var customCooldownEnabled = true
    @Published var customCooldownMinutes = 2.0

    private let api = APIClient()
    private let launcher = BackendLauncher()
    private var pollingTask: Task<Void, Never>?

    var selectedDefinition: BenchmarkDefinition? {
        benchmarkCatalog.first { $0.id == selectedBenchmark }
    }

    var customIsValid: Bool {
        customCPU || customGPU || customMemory
    }

    var customEstimatedMinutes: Double {
        customWorkloadMinutes
            + (customBaselineEnabled ? customBaselineMinutes : 0)
            + (customCooldownEnabled ? customCooldownMinutes : 0)
    }

    func connect() {
        guard !isConnecting else { return }
        isConnecting = true
        errorMessage = nil
        Task {
            do {
                let connection = try await launcher.launch()
                await api.configure(baseURL: connection.url, token: connection.token)
                async let statusRequest = api.status()
                async let catalogRequest = api.benchmarks()
                status = try await statusRequest
                let catalog = try await catalogRequest
                if !catalog.isEmpty {
                    benchmarkCatalog = catalog
                }
                applyBenchmarkDefaults(selectedBenchmark)
                if let artifact = try? await api.latestReport() {
                    latestReportURL = URL(fileURLWithPath: artifact.htmlPath)
                }
                startPolling()
            } catch {
                errorMessage = error.localizedDescription
            }
            isConnecting = false
        }
    }

    func toggleMonitor() {
        Task {
            do {
                if status?.monitorRunning == true {
                    status = try await api.stopMonitor()
                } else {
                    status = try await api.startMonitor()
                }
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func applyBenchmarkDefaults(_ identifier: String) {
        guard identifier != "custom",
              let definition = benchmarkCatalog.first(where: { $0.id == identifier })
        else {
            return
        }
        presetDurationMinutes = max(1, definition.typicalDurationSeconds / 60)
    }

    func startBenchmark() {
        guard status?.benchmark.running != true else { return }
        Task {
            do {
                if selectedBenchmark == "custom" {
                    guard customIsValid else {
                        errorMessage = "Select at least one custom workload."
                        return
                    }
                    let custom = CustomBenchmarkPayload(
                        displayName: customName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                            ? "Custom workload"
                            : customName.trimmingCharacters(in: .whitespacesAndNewlines),
                        requiredPowerSource: customPowerSource,
                        cpu: customCPU,
                        gpu: customGPU,
                        memory: customMemory,
                        gpuProfile: customGPUProfile,
                        memoryMB: customMemoryAutomatic ? 0 : Int(customMemoryMB),
                        workloadSeconds: customWorkloadMinutes * 60,
                        baselineSeconds: customBaselineEnabled ? customBaselineMinutes * 60 : 0,
                        cooldownSeconds: customCooldownEnabled ? customCooldownMinutes * 60 : 0
                    )
                    status = try await api.startCustomBenchmark(custom)
                } else {
                    let duration = selectedDefinition?.adjustableDuration == true
                        ? presetDurationMinutes * 60
                        : 0
                    status = try await api.startBenchmark(
                        name: selectedBenchmark,
                        duration: duration
                    )
                }
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func stopBenchmark() {
        Task {
            do {
                status = try await api.stopBenchmark()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func generateReport() {
        guard !isGeneratingReport else { return }
        isGeneratingReport = true
        errorMessage = nil
        reportMessage = nil

        Task {
            defer { isGeneratingReport = false }
            do {
                let artifact = try await api.generateReport()
                let reportURL = URL(fileURLWithPath: artifact.htmlPath)
                guard FileManager.default.fileExists(atPath: reportURL.path) else {
                    throw ReportPresentationError.artifactMissing(reportURL.path)
                }

                status = try await api.status()
                latestReportURL = reportURL
                let cutoff = artifact.dataThrough.formatted(
                    date: .abbreviated,
                    time: .standard
                )
                reportMessage = "Cumulative report through \(cutoff): \(reportURL.path)"

                if !NSWorkspace.shared.open(reportURL) {
                    NSWorkspace.shared.activateFileViewerSelecting([reportURL])
                    reportMessage = "Cumulative report generated and revealed in Finder: \(reportURL.path)"
                }
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func openLatestReport() {
        guard let latestReportURL else { return }
        errorMessage = nil
        if !NSWorkspace.shared.open(latestReportURL) {
            errorMessage = "The report exists but macOS could not open it: \(latestReportURL.path)"
        }
    }

    func revealLatestReport() {
        guard let latestReportURL else { return }
        NSWorkspace.shared.activateFileViewerSelecting([latestReportURL])
    }

    func dismissReportMessage() {
        reportMessage = nil
    }

    private func startPolling() {
        pollingTask?.cancel()
        pollingTask = Task {
            while !Task.isCancelled {
                do {
                    status = try await api.status()
                    errorMessage = nil
                } catch {
                    errorMessage = error.localizedDescription
                }
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }
}
