import Foundation

actor APIClient {
    enum ClientError: LocalizedError {
        case notConfigured
        case invalidResponse
        case server(String)

        var errorDescription: String? {
            switch self {
            case .notConfigured: return "The MacPowerLab backend is not configured."
            case .invalidResponse: return "The backend returned an invalid response."
            case .server(let message): return message
            }
        }
    }

    private var baseURL: URL?
    private var token: String?
    private let decoder: JSONDecoder

    init() {
        decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
    }

    func configure(baseURL: URL, token: String) {
        self.baseURL = baseURL
        self.token = token.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    func status() async throws -> EngineStatus {
        try await request(path: "/status", method: "GET", body: Optional<Data>.none)
    }

    func startMonitor() async throws -> EngineStatus {
        try await request(path: "/monitor/start", method: "POST", body: Data("{}".utf8))
    }

    func stopMonitor() async throws -> EngineStatus {
        try await request(path: "/monitor/stop", method: "POST", body: Data("{}".utf8))
    }

    func benchmarks() async throws -> [BenchmarkDefinition] {
        try await request(path: "/benchmarks", method: "GET", body: Optional<Data>.none)
    }

    func startBenchmark(name: String, duration: TimeInterval) async throws -> EngineStatus {
        let payload = try JSONSerialization.data(
            withJSONObject: [
                "name": name,
                "duration_seconds": duration,
            ]
        )
        return try await request(path: "/benchmark/start", method: "POST", body: payload)
    }

    func startCustomBenchmark(_ custom: CustomBenchmarkPayload) async throws -> EngineStatus {
        let encoder = JSONEncoder()
        let customData = try encoder.encode(custom)
        guard let customObject = try JSONSerialization.jsonObject(with: customData) as? [String: Any] else {
            throw ClientError.invalidResponse
        }
        let payload = try JSONSerialization.data(
            withJSONObject: [
                "name": "custom",
                "custom": customObject,
            ]
        )
        return try await request(path: "/benchmark/start", method: "POST", body: payload)
    }

    func stopBenchmark() async throws -> EngineStatus {
        try await request(path: "/benchmark/stop", method: "POST", body: Data("{}".utf8))
    }

    func generateReport() async throws -> EngineStatus {
        _ = try await requestData(path: "/report", method: "POST", body: Data("{}".utf8))
        return try await status()
    }

    private func request<T: Decodable>(path: String, method: String, body: Data?) async throws -> T {
        let data = try await requestData(path: path, method: method, body: body)
        return try decoder.decode(T.self, from: data)
    }

    private func requestData(path: String, method: String, body: Data?) async throws -> Data {
        guard let baseURL, let token else { throw ClientError.notConfigured }
        guard let url = URL(string: path, relativeTo: baseURL) else { throw ClientError.notConfigured }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = body
        request.timeoutInterval = 15
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw ClientError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else {
            throw ClientError.server(String(data: data, encoding: .utf8) ?? "HTTP \(http.statusCode)")
        }
        return data
    }
}
