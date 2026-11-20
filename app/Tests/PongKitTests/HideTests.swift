import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Hiding the card: interface state only, and when the card comes back. Then how the hide
/// control and the caret's hand back look.
@MainActor
final class HideTests: XCTestCase {
    func waiting() throws -> [Ping] { try CardSnapshotTests.pings().filter(\.isWaiting) }

    /// Hiding tells the Engine nothing: the Ping keeps waiting and its agent stays held.
    func testHideSendsNoRequest() async throws {
        let pings = try waiting()
        let engine = try FakePongEngine(pings: pings)
        defer { engine.stop() }
        let inbox = Inbox()
        let task = Task { await inbox.follow(clientProvider: { engine.client }, retry: .milliseconds(20)) }
        defer { task.cancel() }
        try await until { inbox.scene != nil }
        let before = engine.requests.filter { !$0.hasPrefix("GET") }
        inbox.hide()
        XCTAssertNil(inbox.scene)
        // Refetches keep happening; none may write or bring the card back. Each refetch starts
        // only after the one before was applied, so two more means at least one was applied.
        let fetches = { engine.requests.filter { $0.hasPrefix("GET /pings") }.count }
        let hiddenAt = fetches()
        try await until { fetches() >= hiddenAt + 2 }
        XCTAssertEqual(engine.requests.filter { !$0.hasPrefix("GET") }, before)
        XCTAssertTrue(inbox.waiting.count == pings.count)
        XCTAssertNil(inbox.scene, "refetches do not bring the card back")
    }

    func testOnlyANewPingBringsTheCardBack() throws {
        let all = try waiting()
        let first = Array(all.dropLast()), new = all.last!
        let inbox = Inbox(pings: first)
        inbox.hide()
        // One already waiting is answered elsewhere: the card stays hidden.
        var answered = first[0]
        answered.state = "answered"
        inbox.apply([answered] + Array(first.dropFirst()))
        XCTAssertNil(inbox.scene)
        inbox.apply(Array(first.dropFirst()))
        XCTAssertNil(inbox.scene)
        // A Ping that arrives while hidden brings it back, on that Ping.
        inbox.apply(Array(first.dropFirst()) + [new])
        XCTAssertEqual(inbox.scene?.ping.id, new.id)
    }

    func testTheBarsWaitingButtonShowsTheFirstWaitingPing() throws {
        let all = try waiting()
        let inbox = Inbox(pings: all)
        inbox.next()
        inbox.hide()
        XCTAssertEqual(inbox.waitingCount, all.count)
        inbox.unhide()
        XCTAssertEqual(inbox.scene?.ping.id, all[0].id)
    }

    func testOpeningANotificationShowsItsPing() throws {
        let all = try waiting()
        let inbox = Inbox(pings: all)
        inbox.hide()
        inbox.show(all[2].id)
        XCTAssertEqual(inbox.scene?.ping.id, all[2].id)
    }

    func testCountIsTheEngineWaitingList() throws {
        XCTAssertEqual(Inbox(pings: try CardSnapshotTests.pings()).waitingCount, try waiting().count)
        XCTAssertEqual(Inbox().waitingCount, 0)
    }

    // MARK: How it looks

    func ping(_ session: String) throws -> Ping {
        try XCTUnwrap(CardSnapshotTests.pings().first { $0.session == session && $0.isWaiting })
    }

    func testCaretMenuWithoutText() throws {
        try assertSnapshot(card(try ping("s-perm"), draft: "", choosing: true), named: "caret-hand-back-only")
    }

    /// A Ping that does not block: both send choices shown, disabled until there is text.
    func testCaretMenuSendChoicesDisabled() throws {
        let p = try XCTUnwrap(waiting().first { $0.blocksAgent != true && $0.isNote != true && $0.answers?.count == 2 })
        try assertSnapshot(card(p, draft: "", choosing: true), named: "caret-choices-disabled")
    }

    func testCaretMenuWithText() throws {
        try assertSnapshot(card(try ping("s-perm"), draft: "Only in the tests folder", choosing: true),
                                       named: "caret-hand-back-with-text")
    }

    func testSettingsHeader() throws {
        try assertSnapshot(SettingsLogo().padding(20).background(Brand.page), named: "settings-header")
    }

    func card(_ ping: Ping, draft: String, choosing: Bool) -> some View {
        CardView(ping: ping, draft: draft, choosing: choosing)
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
            .environment(\.typingAnimates, false)
    }
}
