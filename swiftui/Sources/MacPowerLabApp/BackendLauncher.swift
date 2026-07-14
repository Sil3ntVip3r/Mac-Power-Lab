import Foundation

@MainActor
final class BackendLauncher {
    enum LaunchError: LocalizedError {
        case backendMissing
        case launcherWriteFailed(String)
        case terminalOpenFailed(String)
        case backendTimeout

        var errorDescription: String? {
            switch self {
            case .backendMissing:
                return "The bundled macpowerlab backend is missing."
            case .launcherWriteFailed(let message):
                return "Could not create the backend launcher: \(message)"
            case .terminalOpenFailed(let message):
                return "Could not open the backend launcher in Terminal: \(message)"
            case .backendTimeout:
                return "The backend did not become ready in time. Check the Terminal window for the sudo prompt or startup errors."
            }
        }
    }

    struct Connection {
        let url: URL
        let token: String
    }

    private let fileManager = FileManager.default
    private let connectionTimeout: Duration = .seconds(60)

    /// Reuses a live local backend or opens an executable `.command` launcher.
    ///
    /// This deliberately avoids NSAppleScript and Terminal Apple Events, so the
    /// app does not require macOS Automation permission to start its backend.
    func launch() async throws -> Connection {
        let support = try applicationSupportDirectory()
        let tokenFile = support.appendingPathComponent("api.token")
        let addressFile = support.appendingPathComponent("api.token.address")

        if let existing = try? readConnection(tokenFile: tokenFile, addressFile: addressFile),
           await isHealthy(existing.url) {
            return existing
        }

        try? fileManager.removeItem(at: tokenFile)
        try? fileManager.removeItem(at: addressFile)

        guard let backend = bundledBackend() else {
            throw LaunchError.backendMissing
        }

        let resourceRoot = Bundle.main.resourceURL ?? backend.deletingLastPathComponent()
        let launcher = support.appendingPathComponent("Launch MacPowerLab Backend.command")
        let logFile = support.appendingPathComponent("backend.log")

        let script = """
        #!/bin/zsh
        set -euo pipefail
        cd \(shellQuote(resourceRoot.path))
        echo "MacPowerLab backend launcher"
        echo "Enter your macOS password if sudo asks for it."
        /usr/bin/sudo -v
        exec \(shellQuote(backend.path)) serve \
          --auto-monitor \
          --data-dir \(shellQuote(support.path)) \
          --token-file \(shellQuote(tokenFile.path)) \
          --native-dir \(shellQuote(resourceRoot.appendingPathComponent("native").path)) \
          --native-bin-dir \(shellQuote(resourceRoot.appendingPathComponent("bin/native").path)) \
          2>&1 | /usr/bin/tee -a \(shellQuote(logFile.path))
        """

        do {
            try script.write(to: launcher, atomically: true, encoding: .utf8)
            try fileManager.setAttributes(
                [.posixPermissions: NSNumber(value: Int16(0o700))],
                ofItemAtPath: launcher.path
            )
        } catch {
            throw LaunchError.launcherWriteFailed(error.localizedDescription)
        }

        try openInTerminal(launcher)

        let deadline = ContinuousClock.now.advanced(by: connectionTimeout)
        while ContinuousClock.now < deadline {
            if let connection = try? readConnection(tokenFile: tokenFile, addressFile: addressFile),
               await isHealthy(connection.url) {
                return connection
            }
            try await Task.sleep(for: .milliseconds(250))
        }
        throw LaunchError.backendTimeout
    }

    private func openInTerminal(_ launcher: URL) throws {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        process.arguments = ["-a", "Terminal", launcher.path]
        let stderr = Pipe()
        process.standardError = stderr

        do {
            try process.run()
            process.waitUntilExit()
        } catch {
            throw LaunchError.terminalOpenFailed(error.localizedDescription)
        }

        guard process.terminationStatus == 0 else {
            let data = stderr.fileHandleForReading.readDataToEndOfFile()
            let message = String(data: data, encoding: .utf8)?
                .trimmingCharacters(in: .whitespacesAndNewlines)
            throw LaunchError.terminalOpenFailed(
                message?.isEmpty == false
                    ? message!
                    : "open exited with status \(process.terminationStatus)"
            )
        }
    }

    private func readConnection(tokenFile: URL, addressFile: URL) throws -> Connection {
        let token = try String(contentsOf: tokenFile, encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let address = try String(contentsOf: addressFile, encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !token.isEmpty, let url = URL(string: "http://\(address)") else {
            throw LaunchError.backendTimeout
        }
        return Connection(url: url, token: token)
    }

    private func isHealthy(_ baseURL: URL) async -> Bool {
        guard let url = URL(string: "/health", relativeTo: baseURL) else {
            return false
        }
        var request = URLRequest(url: url)
        request.timeoutInterval = 1.5
        do {
            let (_, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                return false
            }
            return (200..<300).contains(http.statusCode)
        } catch {
            return false
        }
    }

    private func applicationSupportDirectory() throws -> URL {
        let base = try fileManager.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        let directory = base.appendingPathComponent("MacPowerLab", isDirectory: true)
        try fileManager.createDirectory(
            at: directory,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )
        return directory
    }

    private func bundledBackend() -> URL? {
        if let resource = Bundle.main.resourceURL?.appendingPathComponent("macpowerlab"),
           fileManager.isExecutableFile(atPath: resource.path) {
            return resource
        }
        let development = URL(fileURLWithPath: fileManager.currentDirectoryPath)
            .appendingPathComponent("bin/macpowerlab")
        return fileManager.isExecutableFile(atPath: development.path) ? development : nil
    }

    private func shellQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}
