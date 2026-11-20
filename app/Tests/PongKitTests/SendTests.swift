import AppKit
import Network
import SwiftUI
import XCTest
@testable import PongKit

/// Sending a Pong to this Session or to a new one: what goes to the Engine, what the card
/// keeps when the Engine refuses it, and how the send group and its choices look.
@MainActor
final class SendTests: XCTestCase {
    static let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
    var directory: Directory!
    var pings: [Ping] = []

    override func setUp() async throws {
        let text = try String(contentsOf: Self.here.appendingPathComponent("Fixtures/context.sse"), encoding: .utf8)
        directory = Directory()
        for line in text.split(whereSeparator: \.isNewline) { directory.apply(String(line)) }
        var last: [String: Ping] = [:]
        for e in EventLine.parseAll(text) { if let p = e.ping { last[p.id] = p } }
        pings = Array(last.values)
    }

    func ping(_ session: String) throws -> Ping {
        try XCTUnwrap(pings.first { $0.session == session && $0.isWaiting })
    }

    // MARK: What is sent

    func testNewSessionPongCarriesNewSession() throws {
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(Ping.Pong(text: "Start over", newSession: true))) as? [String: Any]
        XCTAssertEqual(body?["text"] as? String, "Start over")
        XCTAssertEqual(body?["new_session"] as? Bool, true)
        let plain = try JSONSerialization.jsonObject(with: JSONEncoder().encode(Ping.Pong(text: "Go on"))) as? [String: Any]
        XCTAssertNil(plain?["new_session"])
    }

    func testThisSessionChoiceSaysHowFullItsContextIs() {
        XCTAssertEqual(SendMenu.thisSessionDetail(0.42), "42% of its memory is used")
        XCTAssertEqual(SendMenu.thisSessionDetail(0.86), "Its memory is 86% full")
        XCTAssertEqual(SendMenu.thisSessionDetail(nil), "Continues where it left off")
    }

    /// The Engine refuses the Pong with 422: the Ping still waits on the card, and the card
    /// has the Engine's words for why. Then a send that the Engine takes clears them.
    func testRefusedPongKeepsThePingAndSaysWhy() async throws {
        let p = try ping("s-migrate")
        let engine = try FakePongEngine(pings: pings)
        defer { engine.stop() }
        let inbox = Inbox()
        let task = Task { await inbox.follow(clientProvider: { engine.client }, retry: .milliseconds(20)) }
        defer { task.cancel() }
        try await until { inbox.deck.contains { $0.id == p.id } }

        engine.refuse = "Claude Code is not signed in: run claude to sign in"
        inbox.send(Ping.Pong(text: "Start over", newSession: true), for: p)
        try await until { inbox.refusals[p.id] != nil }
        XCTAssertEqual(inbox.refusals[p.id], "Claude Code is not signed in: run claude to sign in")
        XCTAssertTrue(inbox.deck.first { $0.id == p.id }?.isWaiting == true)
        let sent = try JSONSerialization.jsonObject(with: XCTUnwrap(engine.pongs.last)) as? [String: Any]
        XCTAssertEqual(sent?["new_session"] as? Bool, true)

        engine.refuse = nil
        inbox.send(Ping.Pong(text: "Go on"), for: p)
        try await until { engine.pongs.count == 2 }
        XCTAssertNil(inbox.refusals[p.id])
    }

    // MARK: How it looks

    func testGroupCaretDisabled() throws {
        try assertSnapshot(card(try ping("s-migrate"), draft: ""), named: "send-caret-disabled")
    }

    func testGroupCaretEnabled() throws {
        try assertSnapshot(card(try ping("s-migrate"), draft: "Only the users table"), named: "send-caret-enabled")
    }

    func testChoicesWithContext() throws {
        try assertSnapshot(card(try ping("s-migrate"), draft: "Only the users table", choosing: true), named: "send-choices-context")
    }

    func testChoicesWithoutContext() throws {
        try assertSnapshot(card(try ping("s-flaky"), draft: "Look at the retry", choosing: true), named: "send-choices-no-context")
    }

    func testAListInTheReplyBox() throws {
        try assertSnapshot(card(try ping("s-migrate"), draft: "Two changes first:\n• keep the users table\n• drop the old index"), named: "send-list")
    }

    func testRefusal() throws {
        try assertSnapshot(card(try ping("s-migrate"), draft: "Only the users table",
                                refusal: "Claude Code is not signed in: run claude to sign in"), named: "send-refused")
    }

    func card(_ ping: Ping, draft: String, choosing: Bool = false, refusal: String? = nil) -> some View {
        CardView(ping: ping, refusal: refusal, draft: draft, choosing: choosing)
            .environment(\.cardContext, CardContext(directory: directory, hover: ContextHover()))
            .environment(\.typingAnimates, false)
    }

}

/// Just enough of the Engine's local API for the Inbox: `/pings`, an `/events` stream that
/// closes at once, and `/pings/{id}/pong`, refused with 422 while `refuse` is set.
final class FakePongEngine: @unchecked Sendable {
    private let listener: NWListener
    private let lock = NSLock()
    private let pingsJSON: Data
    private var _refuse: String?
    private var _pongs: [Data] = []
    private var _requests: [String] = []

    var refuse: String? {
        get { lock.withLock { _refuse } }
        set { lock.withLock { _refuse = newValue } }
    }
    var pongs: [Data] { lock.withLock { _pongs } }
    /// Every request, as "METHOD path".
    var requests: [String] { lock.withLock { _requests } }

    var client: EngineClient? {
        listener.port.map { EngineClient(address: "127.0.0.1:\($0.rawValue)", secret: "s") }
    }

    init(pings: [Ping]) throws {
        // The Inbox decodes snake case; the fixture's Pings are written back out in it.
        let enc = JSONEncoder()
        enc.keyEncodingStrategy = .convertToSnakeCase
        pingsJSON = try enc.encode(pings.map(Wire.init))
        listener = try NWListener(using: .tcp, on: .any)
        listener.newConnectionHandler = { [weak self] c in
            c.start(queue: .global())
            self?.serve(c, buffer: Data())
        }
        let ready = DispatchSemaphore(value: 0)
        listener.stateUpdateHandler = { if case .ready = $0 { ready.signal() } }
        listener.start(queue: .global())
        _ = ready.wait(timeout: .now() + 2)
    }

    func stop() { listener.cancel() }

    private func serve(_ c: NWConnection, buffer: Data) {
        c.receive(minimumIncompleteLength: 1, maximumLength: 65536) { [weak self] data, _, done, _ in
            guard let self else { return }
            var buf = buffer + (data ?? Data())
            guard let head = buf.range(of: Data("\r\n\r\n".utf8)) else {
                if !done { self.serve(c, buffer: buf) }
                return
            }
            let header = String(decoding: buf[..<head.lowerBound], as: UTF8.self)
            let length = header.split(separator: "\r\n").first { $0.lowercased().hasPrefix("content-length:") }
                .flatMap { Int($0.split(separator: ":")[1].trimmingCharacters(in: .whitespaces)) } ?? 0
            let body = buf[head.upperBound...]
            if body.count < length && !done { self.serve(c, buffer: buf); return }
            buf = Data(body.prefix(length))
            let line = header.split(separator: " ")
            let (status, out) = self.answer(String(line[0]), String(line[1]), buf)
            var resp = Data("HTTP/1.1 \(status) X\r\nContent-Type: application/json\r\nContent-Length: \(out.count)\r\nConnection: close\r\n\r\n".utf8)
            resp.append(out)
            c.send(content: resp, completion: .contentProcessed { _ in c.cancel() })
        }
    }

    private func answer(_ method: String, _ path: String, _ body: Data) -> (Int, Data) {
        lock.withLock { _requests.append("\(method) \(path)") }
        if method == "POST", path.hasSuffix("/pong") {
            return lock.withLock {
                _pongs.append(body)
                if let r = _refuse { return (422, try! JSONEncoder().encode(["error": r])) }
                return (200, Data("{}".utf8))
            }
        }
        if path.hasPrefix("/pings") { return (200, pingsJSON) }
        return (200, Data())
    }

    /// The parts of a Ping the Inbox reads.
    struct Wire: Encodable {
        var id, session, question, state: String
        var createdAt: String?
        var blocksAgent: Bool?
        init(_ p: Ping) {
            id = p.id; session = p.session; question = p.question; state = p.state.name
            createdAt = p.createdAt; blocksAgent = p.blocksAgent
        }
    }
}
