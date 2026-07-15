import SwiftUI

@main
struct MacPowerLabApplication: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup { ContentView(model: model) }
        .defaultSize(width: 1200, height: 780)
        Settings {
            SettingsView(model: model)
                .frame(width: 640, height: 760)
        }
    }
}
