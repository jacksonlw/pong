import XCTest
@testable import PongCore
@testable import PongKit

/// A new Ping never replaces the card being shown, wherever the Queue puts it.
@MainActor
final class QueueOrderTests: XCTestCase {
    func testNewPingTakesItsPlaceBehindTheShownCard() throws {
        let all = try CardSnapshotTests.pings().filter(\.isWaiting)
        let inbox = Inbox(pings: [all[0]])
        XCTAssertEqual(inbox.scene?.ping.id, all[0].id)
        // The Engine puts the new Ping first (same Session as the last Pong, say).
        inbox.apply([all[1], all[0]])
        XCTAssertEqual(inbox.scene?.ping.id, all[0].id, "the shown card stays")
        XCTAssertEqual(inbox.deck.count, 2, "X of Y counts the new Ping")
        XCTAssertEqual(inbox.position, 1, "and X follows the shown card")
    }

    /// Answered Pings the card has shown stay behind the waiting ones, so the count never
    /// reads as if Pings were still waiting after the one on the card.
    func testAnsweredPingsStayBehindTheWaitingOne() throws {
        let all = try CardSnapshotTests.pings().filter(\.isWaiting)
        var done = all[0]
        let inbox = Inbox(pings: [done])
        done.state = .answered
        done.pong = Ping.Pong(answer: "Yes")
        inbox.apply([done])
        XCTAssertNil(inbox.scene, "nothing waits: the card closes")
        // The Engine puts the new Ping first (it blocks its agent, say).
        inbox.apply([all[1], done])
        XCTAssertEqual(inbox.scene?.ping.id, all[1].id)
        XCTAssertEqual(inbox.deck.map(\.id), [done.id, all[1].id])
        XCTAssertEqual(inbox.position, 1, "2 of 2: the last Ping, with nothing waiting after it")
    }
}
