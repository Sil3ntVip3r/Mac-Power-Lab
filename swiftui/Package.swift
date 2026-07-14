// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "MacPowerLabApp",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "MacPowerLabApp", targets: ["MacPowerLabApp"])],
    targets: [.executableTarget(name: "MacPowerLabApp")]
)
