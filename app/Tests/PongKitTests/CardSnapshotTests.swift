import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The card rendered from Pings in a recorded event stream (Fixtures/events.sse, captured
/// from a real Engine with the `pong ping` CLI), compared with reference PNGs in Snapshots/.
/// Run with `PONG_RECORD_SNAPSHOTS=1` to write new references, then look at them.
@MainActor
final class CardSnapshotTests: XCTestCase {
    static let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent()

    static func fixture() throws -> [EngineEvent] {
        EventLine.parseAll(try String(contentsOf: here.appendingPathComponent("Fixtures/events.sse"), encoding: .utf8))
    }

    /// Each Ping as it stood when created, or as it last stood, in the order it arrived.
    static func pings(final: Bool = true) throws -> [Ping] {
        var last: [String: Ping] = [:]
        var order: [String] = []
        for e in try fixture() {
            guard let p = e.ping else { continue }
            if last[p.id] == nil { order.append(p.id) } else if !final { continue }
            last[p.id] = p
        }
        return order.compactMap { last[$0] }
    }

    func waitingPings() throws -> [Ping] { try Self.pings().filter(\.isWaiting) }

    func ping(answers n: Int) throws -> Ping {
        try XCTUnwrap(waitingPings().first { $0.answers?.count == n && $0.approves == nil && $0.allowSeveral == nil },
                      "no waiting Ping with \(n) answers in the fixture")
    }

    func ping(session: String) throws -> Ping {
        try XCTUnwrap(Self.pings().first { $0.session == session }, "no Ping from \(session) in the fixture")
    }

    func testTwoAnswers() throws { try assertCard(ping(answers: 2), named: "two-answers") }
    /// Another computer's Ping names that computer beside the Project, and says when it is unreachable.
    /// A Pong waiting for an unreachable computer shows as sent and can be withdrawn.
    func testPongWaitingForAnUnreachableComputer() throws {
        var p = try ping(answers: 2)
        p.state = .answered
        p.pong = Ping.Pong(answer: p.answers?.first?.label)
        p.pong?.delivery = "sent"
        p.computer = .init(id: "d1", name: "Desktop", reachable: false)
        p.remote = true
        try assertCard(p, named: "pong-waiting-for-computer")
    }

    func testAnotherComputersPing() throws {
        var p = try ping(answers: 2)
        p.computer = .init(id: "d1", name: "Desktop", reachable: false)
        p.remote = true
        p.draft = .init(text: "after lunch")
        try assertCard(p, named: "another-computer")
    }
    func testThreeAnswers() throws { try assertCard(ping(answers: 3), named: "three-answers") }
    func testFourAnswers() throws { try assertCard(ping(answers: 4), named: "four-answers") }
    func testSeveral() throws { try assertCard(ping(session: "s-checks"), named: "several") }
    func testApprovePlan() throws { try assertCard(ping(session: "s-plan"), named: "approve-plan") }
    func testPermission() throws { try assertCard(ping(session: "s-perm"), named: "permission") }
    func testNote() throws { try assertCard(ping(session: "s-done"), named: "note") }
    func testAnswered() throws { try assertCard(ping(session: "s-release2"), named: "answered") }
    func testAnsweredWithText() throws { try assertCard(ping(session: "s-typed"), named: "answered-with-text") }
    func testAnsweredTicked() throws { try assertCard(ping(session: "s-ticked"), named: "answered-ticked") }
    func testHandedBack() throws { try assertCard(ping(session: "s-back"), named: "handed-back") }

    func testFixtureHoldsEveryKind() throws {
        XCTAssertEqual(try ping(session: "s-release2").pong?.answer, "No")
        XCTAssertEqual(try ping(session: "s-ticked").pong?.answers, ["express", "axios"])
        XCTAssertEqual(try ping(session: "s-back").state, "handed_back")
        XCTAssertEqual(try ping(session: "s-done").isNote, true)
        XCTAssertEqual(try ping(session: "s-perm").approves, "action")
    }

    /// Pings that stopped waiting before the card showed them stay out of the deck; one the
    /// card showed stays, behind the waiting ones, and going back to it shows what was chosen.
    func testAnsweredPingStaysInTheDeck() throws {
        let inbox = Inbox(pings: try Self.pings(final: false))
        XCTAssertEqual(inbox.deck.count, 11)
        while inbox.current?.session != "s-release2" { inbox.next() }
        inbox.next()
        inbox.apply(try Self.pings())
        XCTAssertEqual(inbox.deck.count, 8, "waiting Pings plus the one shown that was answered")
        while inbox.position > 0 { inbox.previous() }
        XCTAssertEqual(inbox.current?.session, "s-release2")
        XCTAssertEqual(inbox.current?.pong?.answer, "No")
    }

    func assertCard(_ ping: Ping, named name: String, file: StaticString = #filePath, line: UInt = #line) throws {
        let all = try Self.pings()
        let deck = ping.isWaiting ? all.filter(\.isWaiting) : all
        let at = deck.firstIndex { $0.id == ping.id } ?? 0
        try assertSnapshot(CardView(ping: ping, position: at, count: deck.count), named: name, file: file, line: line)
    }

}
