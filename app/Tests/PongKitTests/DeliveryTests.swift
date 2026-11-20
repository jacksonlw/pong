import XCTest
@testable import PongKit

/// A sent Pong reads differently from a delivered one, and an undelivered Pong comes back
/// to the human.
@MainActor
final class DeliveryTests: XCTestCase {
    func waiting() throws -> [Ping] { try CardSnapshotTests.pings().filter(\.isWaiting) }

    func answered(_ p: Ping, _ delivery: String?) -> Ping {
        var p = p
        p.state = "answered"
        p.pong = Ping.Pong(answer: "Yes")
        p.pong?.delivery = delivery
        return p
    }

    func testSentAndDeliveredReadDifferently() {
        var pong = Ping.Pong(answer: "Yes")
        pong.delivery = "sent"
        let sent = pong.deliveryLine
        pong.delivery = "delivered"
        XCTAssertNotEqual(sent, pong.deliveryLine)
        XCTAssertFalse(pong.needsHuman)
        pong.delivery = nil
        XCTAssertNil(pong.deliveryLine, "a Pong from before delivery was tracked says nothing more")
    }

    func testUndeliveredPongBringsTheCardBack() throws {
        let all = try waiting()
        let inbox = Inbox(pings: [all[0]])
        XCTAssertEqual(inbox.scene?.ping.id, all[0].id)
        inbox.apply([answered(all[0], "sent")])
        XCTAssertNil(inbox.scene, "a sent Pong needs nothing from the human")
        inbox.hide()
        inbox.apply([answered(all[0], "undelivered")])
        XCTAssertEqual(inbox.scene?.ping.id, all[0].id, "an undelivered Pong comes back, even to a hidden card")
        XCTAssertEqual(inbox.waitingCount, 1)
        inbox.apply([answered(all[0], "left")])
        XCTAssertNil(inbox.scene)
    }

    /// An undelivered Pong reads in red with the error mark, as in the Thread, without the
    /// Engine's reason, and offers Send again into its own Session first.
    func testUndeliveredCard() throws {
        let blocking = try waiting().first { $0.blocksAgent == true } ?? waiting()[0]
        var p = answered(blocking, "undelivered")
        p.pong?.answer = blocking.answers?.first?.label
        p.pong?.notDelivered = "Its prompt is no longer held: the terminal may have closed or the Harness quit."
        try CardSnapshotTests().assertCard(p, named: "card-undelivered")
    }

    func testUnconfirmedPongComesBackToo() throws {
        let p = answered(try waiting()[0], "unconfirmed")
        XCTAssertTrue(p.needsHuman)
        XCTAssertEqual(Inbox(pings: [p]).scene?.ping.id, p.id)
    }
}
