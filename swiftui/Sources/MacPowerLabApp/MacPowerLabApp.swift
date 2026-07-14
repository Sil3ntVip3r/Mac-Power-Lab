import SwiftUI

@main
struct MacPowerLabApplication: App {
    var body: some Scene {
        WindowGroup { ContentView() }
        .defaultSize(width: 1200, height: 780)
        Settings { SettingsView(status: nil).frame(width: 520, height: 360) }
    }
}
