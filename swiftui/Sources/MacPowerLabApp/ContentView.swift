import SwiftUI

private enum SidebarDestination: String, CaseIterable, Identifiable, Hashable {
    case overview
    case battery
    case performance
    case applications
    case fullMonitor
    case benchmarks
    case settings

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: return "Overview"
        case .battery: return "Battery & Charging"
        case .performance: return "Performance"
        case .applications: return "Applications"
        case .fullMonitor: return "Full Monitor"
        case .benchmarks: return "Benchmarks"
        case .settings: return "Settings"
        }
    }

    var icon: String {
        switch self {
        case .overview: return "gauge.with.dots.needle.50percent"
        case .battery: return "battery.100percent.bolt"
        case .performance: return "cpu"
        case .applications: return "app.badge"
        case .fullMonitor: return "rectangle.3.group"
        case .benchmarks: return "speedometer"
        case .settings: return "gearshape"
        }
    }
}

struct ContentView: View {
    @StateObject private var model = AppModel()
    @State private var selection: SidebarDestination? = .overview

    var body: some View {
        NavigationSplitView {
            List(selection: $selection) {
                Section("Monitor") {
                    sidebarLink(.overview)
                    sidebarLink(.battery)
                    sidebarLink(.performance)
                    sidebarLink(.applications)
                    sidebarLink(.fullMonitor)
                }

                Section("Tools") {
                    sidebarLink(.benchmarks)
                    sidebarLink(.settings)
                }
            }
            .navigationTitle("MacPowerLab")
            .navigationSplitViewColumnWidth(min: 190, ideal: 220, max: 270)
        } detail: {
            detailView
        }
        .toolbar {
            ToolbarItemGroup {
                Button(
                    model.status?.monitorRunning == true
                        ? "Stop Monitor"
                        : "Start Monitor"
                ) {
                    model.toggleMonitor()
                }
                .disabled(model.status == nil)

                Button("Generate Report") {
                    model.generateReport()
                }
                .disabled(model.status?.session == nil)

                if model.status == nil {
                    Button("Connect") {
                        model.connect()
                    }
                    .disabled(model.isConnecting)
                }
            }
        }
        .overlay(alignment: .bottom) {
            if let message = model.errorMessage {
                Text(message)
                    .padding(10)
                    .background(
                        .red.opacity(0.92),
                        in: RoundedRectangle(cornerRadius: 8)
                    )
                    .foregroundStyle(.white)
                    .padding()
                    .textSelection(.enabled)
            }
        }
        .task {
            if model.status == nil {
                model.connect()
            }
        }
        .frame(minWidth: 1180, minHeight: 760)
    }

    private func sidebarLink(_ destination: SidebarDestination) -> some View {
        Label(destination.title, systemImage: destination.icon)
            .tag(destination)
    }

    @ViewBuilder
    private var detailView: some View {
        switch selection ?? .overview {
        case .overview:
            DashboardView(status: model.status)
        case .battery:
            BatteryChargingView(status: model.status)
        case .performance:
            PerformanceView(status: model.status)
        case .applications:
            ApplicationsView(status: model.status)
        case .fullMonitor:
            FullMonitorView(status: model.status)
        case .benchmarks:
            BenchmarkView(model: model)
        case .settings:
            SettingsView(status: model.status)
        }
    }
}
