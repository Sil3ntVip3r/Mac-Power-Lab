import SwiftUI

struct BatteryChargingView: View {
    let status: EngineStatus?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                MonitorPageHeader(
                    title: "Battery & Charging",
                    subtitle: "Electrical flow, capacity, health, cell balance, charging, and adapter details.",
                    systemImage: "battery.100percent.bolt"
                )

                if let sample = status?.lastSample {
                    LazyVGrid(
                        columns: [GridItem(.adaptive(minimum: 210))],
                        spacing: 12
                    ) {
                        MonitorMetricCard(
                            title: "Charge Level",
                            value: MPLFormat.percent(sample.battery.percent),
                            subtitle: "\(MPLFormat.text(sample.battery.powerSource)) · \(MPLFormat.text(sample.battery.state))",
                            systemImage: "battery.75percent"
                        )
                        MonitorMetricCard(
                            title: "Battery Flow",
                            value: MPLFormat.watts(sample.battery.netW),
                            subtitle: sample.battery.charging == true ? "Battery accepting energy" : "Battery supplying energy",
                            systemImage: "arrow.left.arrow.right"
                        )
                        MonitorMetricCard(
                            title: "Battery Temperature",
                            value: MPLFormat.celsius(sample.battery.temperatureC),
                            subtitle: MPLFormat.fahrenheit(sample.battery.temperatureF),
                            systemImage: "thermometer.medium"
                        )
                        MonitorMetricCard(
                            title: "Battery Health",
                            value: MPLFormat.percent(sample.battery.healthPercent),
                            subtitle: "\(MPLFormat.integer(sample.battery.cycleCount)) cycles",
                            systemImage: "heart.circle"
                        )
                        MonitorMetricCard(
                            title: "Estimated Energy",
                            value: MPLFormat.wattHours(sample.battery.estimatedRemainingWh),
                            subtitle: "Full \(MPLFormat.wattHours(sample.battery.estimatedFullWh))",
                            systemImage: "bolt.circle"
                        )
                        MonitorMetricCard(
                            title: "Adapter Output",
                            value: MPLFormat.watts(sample.adapter.outputEstimateW),
                            subtitle: MPLFormat.text(sample.adapter.outputEstimateSource),
                            systemImage: "powerplug"
                        )
                    }

                    MonitorSection("Electrical", systemImage: "waveform.path.ecg") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Voltage", value: MPLFormat.volts(sample.battery.voltageV)),
                            MonitorStat(title: "Current", value: MPLFormat.amps(sample.battery.currentA)),
                            MonitorStat(title: "Net battery watts", value: MPLFormat.watts(sample.battery.netW)),
                            MonitorStat(title: "Primary system load", value: MPLFormat.watts(sample.primaryLoadW), detail: sample.primaryLoadSource),
                            MonitorStat(title: "BMS SystemPower", value: MPLFormat.watts(sample.battery.bmsSystemPowerW)),
                            MonitorStat(title: "System effective load", value: MPLFormat.watts(sample.battery.systemEffectiveTotalLoadW)),
                            MonitorStat(title: "PowerDistribution input", value: MPLFormat.watts(sample.battery.powerDistributionInputW)),
                            MonitorStat(title: "External power connected", value: MPLFormat.boolean(sample.battery.externalConnected)),
                            MonitorStat(title: "Charging", value: MPLFormat.boolean(sample.battery.charging)),
                        ])
                    }

                    MonitorSection("Capacity & Runtime", systemImage: "chart.bar.fill") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Current capacity", value: MPLFormat.milliampHours(sample.battery.currentCapacityMAh)),
                            MonitorStat(title: "Full-charge capacity", value: MPLFormat.milliampHours(sample.battery.fullChargeCapacityMAh)),
                            MonitorStat(title: "Design capacity", value: MPLFormat.milliampHours(sample.battery.designCapacityMAh)),
                            MonitorStat(title: "Health estimate", value: MPLFormat.percent(sample.battery.healthPercent)),
                            MonitorStat(title: "Estimated remaining energy", value: MPLFormat.wattHours(sample.battery.estimatedRemainingWh)),
                            MonitorStat(title: "Estimated full energy", value: MPLFormat.wattHours(sample.battery.estimatedFullWh)),
                            MonitorStat(title: "Time to empty", value: MPLFormat.minutes(sample.battery.timeToEmptyMinutes)),
                            MonitorStat(title: "Time to full", value: MPLFormat.minutes(sample.battery.timeToFullMinutes)),
                            MonitorStat(title: "Cycle count", value: MPLFormat.integer(sample.battery.cycleCount)),
                        ])
                    }

                    MonitorSection("Cell & Battery Condition", systemImage: "square.3.layers.3d.down.right") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Cell minimum", value: MPLFormat.millivolts(sample.battery.cellVoltageMinMV)),
                            MonitorStat(title: "Cell maximum", value: MPLFormat.millivolts(sample.battery.cellVoltageMaxMV)),
                            MonitorStat(title: "Cell voltage delta", value: MPLFormat.millivolts(sample.battery.cellVoltageDeltaMV)),
                            MonitorStat(title: "QMax delta", value: MPLFormat.number(sample.battery.qMaxDelta, digits: 2)),
                            MonitorStat(title: "Weighted Ra delta", value: MPLFormat.number(sample.battery.weightedRADelta, digits: 2)),
                            MonitorStat(title: "Cell disconnect count", value: MPLFormat.integer(sample.battery.cellDisconnectCount)),
                            MonitorStat(title: "Raw temperature", value: MPLFormat.number(sample.battery.temperatureRaw, digits: 1)),
                            MonitorStat(title: "Virtual temperature", value: MPLFormat.celsius(sample.battery.virtualTemperatureC)),
                        ])
                    }

                    MonitorSection("Charger / Adapter", systemImage: "powerplug.fill") {
                        MonitorStatGrid(items: [
                            MonitorStat(title: "Adapter name", value: MPLFormat.text(sample.adapter.name)),
                            MonitorStat(title: "Connected", value: sample.adapter.connected ? "Yes" : "No"),
                            MonitorStat(title: "Rated power", value: MPLFormat.watts(sample.adapter.ratedW)),
                            MonitorStat(title: "PD contract voltage", value: MPLFormat.volts(sample.adapter.contractVoltageV)),
                            MonitorStat(title: "PD contract current", value: MPLFormat.amps(sample.adapter.contractCurrentA)),
                            MonitorStat(title: "PD contract power", value: MPLFormat.watts(sample.adapter.contractW)),
                            MonitorStat(title: "Estimated live output", value: MPLFormat.watts(sample.adapter.outputEstimateW)),
                            MonitorStat(title: "Output estimate source", value: MPLFormat.text(sample.adapter.outputEstimateSource)),
                            MonitorStat(title: "Adapter load", value: MPLFormat.percent(sample.adapter.loadPercent)),
                            MonitorStat(title: "Headroom", value: MPLFormat.watts(sample.adapter.headroomW)),
                            MonitorStat(title: "Battery assist", value: MPLFormat.watts(sample.adapter.batteryAssistW)),
                            MonitorStat(title: "Port controller max", value: MPLFormat.watts(sample.adapter.portControllerMaxPowerW)),
                        ])
                    }
                } else {
                    MonitorEmptyState(
                        title: "No battery sample yet",
                        message: "Start the monitor to populate battery and charger statistics.",
                        systemImage: "battery.0percent"
                    )
                }
            }
            .padding()
        }
    }
}
