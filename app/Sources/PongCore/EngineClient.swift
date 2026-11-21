import Foundation

/// Talks to the Engine's local API.
public struct EngineClient: PingFeedAtSeq {
    public var address: String
    public var secret: String

    public init(address: String, secret: String) {
        self.address = address
        self.secret = secret
    }

    /// Reads the address and secret from `engine.json` in the data directory.
    public static func load(dir: URL) throws -> EngineClient {
        struct File: Decodable { var address: String; var secret: String }
        let data = try Data(contentsOf: dir.appendingPathComponent("engine.json"))
        let f = try JSONDecoder().decode(File.self, from: data)
        return EngineClient(address: f.address, secret: f.secret)
    }

    public struct Failure: FeedRefusal, CustomStringConvertible {
        public var description: String
        /// The Engine's `error`, in words to show the human, when it gave one.
        public var message: String? = nil
        /// The HTTP status, when the Engine answered.
        public var status = 0

        public init(description: String, message: String? = nil, status: Int = 0) {
            self.description = description; self.message = message; self.status = status
        }
    }

    private static let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.timeoutIntervalForRequest = 3600 // the event stream stays open
        return URLSession(configuration: c)
    }()

    package func request(_ path: String, method: String = "GET", body: Data? = nil) -> URLRequest {
        var r = URLRequest(url: URL(string: "http://\(address)\(path)")!)
        r.httpMethod = method
        r.setValue("Bearer \(secret)", forHTTPHeaderField: "Authorization")
        if let body {
            r.httpBody = body
            r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        return r
    }

    package func send(_ r: URLRequest, timeout: TimeInterval = 10) async throws -> Data {
        var r = r
        r.timeoutInterval = timeout
        let (data, resp) = try await Self.session.data(for: r)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            struct Body: Decodable { var error: String }
            throw Failure(description: "\(r.httpMethod ?? "") \(r.url?.path ?? ""): \(status) \(String(decoding: data, as: UTF8.self))",
                          message: (try? JSONDecoder().decode(Body.self, from: data))?.error, status: status)
        }
        return data
    }

    public var unreachable: String { "Pong could not reach the Engine." }

    public func health() async throws {
        _ = try await send(request("/health"))
    }

    /// Every Ping the Engine keeps, in the Engine's order.
    public func allPings() async throws -> [Ping] {
        try await allPingsAtSeq().0
    }

    /// Every Ping, and the event seq the list is at least as new as (`X-Pong-Seq`).
    public func allPingsAtSeq() async throws -> ([Ping], Int) {
        var r = request("/pings")
        r.timeoutInterval = 10
        let (data, resp) = try await Self.session.data(for: r)
        let http = resp as? HTTPURLResponse
        guard http?.statusCode == 200 else {
            throw Failure(description: "GET /pings: \(http?.statusCode ?? 0) \(String(decoding: data, as: UTF8.self))", message: nil, status: http?.statusCode ?? 0)
        }
        let seq = (http?.value(forHTTPHeaderField: "X-Pong-Seq")).flatMap(Int.init) ?? 0
        return (try JSON.decoder.decode([Ping].self, from: data), seq)
    }

    /// Sends a Pong: any mix of one answer, ticked answers and typed text.
    public func pong(_ pingID: String, _ pong: Ping.Pong) async throws {
        let body = try JSONEncoder().encode(pong)
        _ = try await send(request("/pings/\(pingID)/pong", method: "POST", body: body))
    }

    /// Hands a Ping back to its Harness.
    public func handBack(_ pingID: String) async throws {
        _ = try await send(request("/pings/\(pingID)/hand-back", method: "POST"))
    }

    /// Sends an undelivered or unconfirmed Pong again: into its own Session, or as a new one.
    public func sendAgain(_ pingID: String, newSession: Bool) async throws {
        let body = try JSONEncoder().encode(["new_session": newSession])
        _ = try await send(request("/pings/\(pingID)/send-again", method: "POST", body: body), timeout: 60)
    }

    /// Keeps an undelivered or unconfirmed Pong without sending it again.
    public func leave(_ pingID: String) async throws {
        _ = try await send(request("/pings/\(pingID)/leave", method: "POST"))
    }

    /// Shares the Pong being written (`PUT /pings/{id}/draft`).
    public func draft(_ pingID: String, text: String) async throws {
        _ = try await send(request("/pings/\(pingID)/draft", method: "PUT", body: JSONEncoder().encode(Ping.Draft(text: text))))
    }

    /// Takes back a Pong not yet delivered (`POST /pings/{id}/withdraw-pong`).
    public func withdraw(_ pingID: String) async throws {
        _ = try await send(request("/pings/\(pingID)/withdraw-pong", method: "POST"))
    }

    /// Dismisses a Note.
    public func dismiss(_ pingID: String) async throws {
        _ = try await send(request("/pings/\(pingID)/dismiss", method: "POST"))
    }

    /// The event stream from after `since`; ends when the connection drops.
    public func events(since: Int) -> AsyncThrowingStream<EngineEvent, Error> {
        let r = request("/events?since=\(since)")
        return AsyncThrowingStream { cont in
            let task = Task {
                do {
                    let (bytes, resp) = try await Self.session.bytes(for: r)
                    guard (resp as? HTTPURLResponse)?.statusCode == 200 else {
                        throw Failure(description: "GET /events: \((resp as? HTTPURLResponse)?.statusCode ?? 0)")
                    }
                    for try await line in bytes.lines {
                        if let e = EventLine.parse(line) { cont.yield(e) }
                    }
                    cont.finish()
                } catch {
                    cont.finish(throwing: error)
                }
            }
            cont.onTermination = { _ in task.cancel() }
        }
    }
}
