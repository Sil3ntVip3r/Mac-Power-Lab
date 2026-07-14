import Foundation
import SwiftUI

@MainActor
final class AppModel: ObservableObject {
    @Published var status: EngineStatus?
    @Published var isConnecting = false
    @Published var errorMessage: String?

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
        Task {
            do {
                status = try await api.generateReport()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
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
