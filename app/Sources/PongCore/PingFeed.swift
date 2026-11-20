import Foundation

/// What feeds the model of waiting Pings, and takes the human's Pongs back. On the Mac it is
/// the local Engine; on the phone it is the set, through the wire. Nothing above
/// this interface knows which.
public protocol PingFeed: Sendable {
    /// Every Ping the feed keeps, in the Queue's order.
    func allPings() async throws -> [Ping]
    /// Sends a Pong: any mix of one answer, ticked answers and typed text.
    func pong(_ pingID: String, _ pong: Ping.Pong) async throws
    /// Hands a Ping back to its Harness.
    func handBack(_ pingID: String) async throws
    /// Sends an undelivered or unconfirmed Pong again: into its own Session, or as a new one.
    func sendAgain(_ pingID: String, newSession: Bool) async throws
    /// Keeps an undelivered or unconfirmed Pong without sending it again.
    func leave(_ pingID: String) async throws
    /// Withdraws a Pong still waiting to be delivered; the Ping waits again.
    func withdraw(_ pingID: String) async throws
    /// Keeps the human's typed text for a Ping, so other Devices show it as a draft.
    func draft(_ pingID: String, text: String) async throws
    /// A Resource's content, from the computer the Ping waits on.
    func resource(_ resourceID: String, of pingID: String) async throws -> Data
    /// Dismisses a Note.
    func dismiss(_ pingID: String) async throws
    /// Changes from after `since`; ends when the connection drops.
    func events(since: Int) -> AsyncThrowingStream<EngineEvent, Error>
    /// What the card says when a Pong could not reach the feed at all.
    var unreachable: String { get }
}

/// A feed that cannot yet send a Pong again refuses, in words the card shows.
struct CannotSendAgain: FeedRefusal {
    var message: String? { "This Pong can only be sent again from the Mac." }
}

public extension PingFeed {
    func sendAgain(_ pingID: String, newSession: Bool) async throws { throw CannotSendAgain() }
    func leave(_ pingID: String) async throws { throw CannotSendAgain() }
    func withdraw(_ pingID: String) async throws { throw CannotWithdraw() }
    /// A feed that keeps no drafts drops them; the text stays on the card.
    func draft(_ pingID: String, text: String) async throws {}
}

struct CannotWithdraw: FeedRefusal {
    var message: String? { "This Pong can no longer be withdrawn." }
}

/// A feed's refusal, with words to show the human when it gave some.
public protocol FeedRefusal: Error {
    var message: String? { get }
}
