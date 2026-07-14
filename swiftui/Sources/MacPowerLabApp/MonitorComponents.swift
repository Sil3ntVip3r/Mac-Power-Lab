import SwiftUI

enum MPLFormat {
    static func watts(_ value: Double?) -> String {
        value.map { String(format: "%.2f W", $0) } ?? "n/a"
    }

    static func percent(_ value: Double?) -> String {
        value.map { String(format: "%.1f%%", $0) } ?? "n/a"
    }

    static func number(_ value: Double?, digits: Int = 2) -> String {
        value.map { String(format: "%.*f", digits, $0) } ?? "n/a"
    }

    static func integer(_ value: Int?) -> String {
        value.map(String.init) ?? "n/a"
    }

    static func integer64(_ value: Int64?) -> String {
        value.map(String.init) ?? "n/a"
    }

    static func celsius(_ value: Double?) -> String {
        value.map { String(format: "%.1f °C", $0) } ?? "n/a"
    }

    static func fahrenheit(_ value: Double?) -> String {
        value.map { String(format: "%.1f °F", $0) } ?? "n/a"
    }

    static func volts(_ value: Double?) -> String {
        value.map { String(format: "%.3f V", $0) } ?? "n/a"
    }

    static func amps(_ value: Double?) -> String {
        value.map { String(format: "%.3f A", $0) } ?? "n/a"
    }

    static func megahertz(_ value: Double?) -> String {
        value.map { String(format: "%.0f MHz", $0) } ?? "n/a"
    }

    static func millivolts(_ value: Double?) -> String {
        value.map { String(format: "%.1f mV", $0) } ?? "n/a"
    }

    static func milliampHours(_ value: Double?) -> String {
        value.map { String(format: "%.0f mAh", $0) } ?? "n/a"
    }

    static func wattHours(_ value: Double?) -> String {
        value.map { String(format: "%.2f Wh", $0) } ?? "n/a"
    }

    static func minutes(_ value: Double?) -> String {
        guard let value, value.isFinite, value >= 0 else { return "n/a" }
        let total = Int(value.rounded())
        if total >= 60 {
            return String(format: "%dh %02dm", total / 60, total % 60)
        }
        return "\(total) min"
    }

    static func duration(_ seconds: Double?) -> String {
        guard let seconds, seconds.isFinite, seconds >= 0 else { return "n/a" }
        let total = Int(seconds.rounded())
        if total >= 3600 {
            return String(format: "%dh %02dm %02ds", total / 3600, (total % 3600) / 60, total % 60)
        }
        if total >= 60 {
            return String(format: "%dm %02ds", total / 60, total % 60)
        }
        return "\(total)s"
    }

    static func bytesPerSecond(_ value: Double?) -> String {
        guard let value, value.isFinite else { return "n/a" }
        let absolute = abs(value)
        if absolute >= 1_000_000_000 {
            return String(format: "%.2f GB/s", value / 1_000_000_000)
        }
        if absolute >= 1_000_000 {
            return String(format: "%.2f MB/s", value / 1_000_000)
        }
        if absolute >= 1_000 {
            return String(format: "%.1f KB/s", value / 1_000)
        }
        return String(format: "%.0f B/s", value)
    }

    static func boolean(_ value: Bool?) -> String {
        guard let value else { return "n/a" }
        return value ? "Yes" : "No"
    }

    static func date(_ value: Date?) -> String {
        guard let value else { return "n/a" }
        return value.formatted(date: .abbreviated, time: .standard)
    }

    static func text(_ value: String?) -> String {
        guard let value, !value.isEmpty else { return "n/a" }
        return value
    }
}

struct MonitorPageHeader: View {
    let title: String
    let subtitle: String
    let systemImage: String

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: systemImage)
                .font(.system(size: 30))
                .foregroundStyle(.tint)
                .frame(width: 42)
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(.largeTitle.bold())
                Text(subtitle)
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
    }
}

struct MonitorMetricCard: View {
    let title: String
    let value: String
    let subtitle: String
    var systemImage: String? = nil
    var emphasis: Color? = nil

    var body: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 5) {
                Text(value)
                    .font(.title2.bold().monospacedDigit())
                    .foregroundStyle(emphasis ?? .primary)
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        } label: {
            if let systemImage {
                Label(title, systemImage: systemImage)
            } else {
                Text(title)
            }
        }
    }
}

struct MonitorStat: Identifiable {
    var id: String { title }
    let title: String
    let value: String
    var detail: String? = nil
    var systemImage: String? = nil
}

struct MonitorStatGrid: View {
    let items: [MonitorStat]
    var minimumWidth: CGFloat = 190

    var body: some View {
        LazyVGrid(
            columns: [GridItem(.adaptive(minimum: minimumWidth), alignment: .topLeading)],
            alignment: .leading,
            spacing: 10
        ) {
            ForEach(items) { item in
                VStack(alignment: .leading, spacing: 4) {
                    if let systemImage = item.systemImage {
                        Label(item.title, systemImage: systemImage)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    } else {
                        Text(item.title)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Text(item.value)
                        .font(.headline.monospacedDigit())
                        .textSelection(.enabled)
                    if let detail = item.detail, !detail.isEmpty {
                        Text(detail)
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .lineLimit(2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(10)
                .background(.quaternary.opacity(0.45), in: RoundedRectangle(cornerRadius: 8))
            }
        }
    }
}

struct MonitorSection<Content: View>: View {
    let title: String
    let systemImage: String
    @ViewBuilder let content: Content

    init(
        _ title: String,
        systemImage: String,
        @ViewBuilder content: () -> Content
    ) {
        self.title = title
        self.systemImage = systemImage
        self.content = content()
    }

    var body: some View {
        GroupBox {
            content
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(4)
        } label: {
            Label(title, systemImage: systemImage)
                .font(.headline)
        }
    }
}

struct MonitorEmptyState: View {
    let title: String
    let message: String
    let systemImage: String

    var body: some View {
        ContentUnavailableView(
            title,
            systemImage: systemImage,
            description: Text(message)
        )
    }
}

struct StatusBadge: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text)
            .font(.caption.bold())
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(color.opacity(0.18), in: Capsule())
            .foregroundStyle(color)
    }
}
