import Foundation
import Testing
import PongCore

/// A feed that is neither an Engine nor the wire: the model must not care.
final class FakeFeed: PingFeed, @unchecked Sendable {
    var pings: [Ping]
    var pongs: [(String, Ping.Pong)] = []
    init(_ pings: [Ping]) { self.pings = pings }
    func allPings() async throws -> [Ping] { pings }
    func pong(_ id: String, _ p: Ping.Pong) async throws {
        pongs.append((id, p))
        if let i = pings.firstIndex(where: { $0.id == id }) { pings[i].state = .answered; pings[i].pong = p }
    }
    func handBack(_ id: String) async throws {}
    func dismiss(_ id: String) async throws {}
    func resource(_ r: String, of p: String) async throws -> Data { Data() }
    func events(since: Int) -> AsyncThrowingStream<EngineEvent, Error> { AsyncThrowingStream { _ in } }
    var unreachable: String { "The feed is out of reach." }
}

@MainActor
@Test func inboxFollowsAnyFeedAndSendsPongsToIt() async throws {
    let feed = FakeFeed([Ping(id: "a", session: "s", question: "One?", state: .waiting),
                         Ping(id: "b", session: "t", question: "Two?", state: .waiting)])
    let inbox = Inbox()
    let follow = Task { await inbox.follow(clientProvider: { feed }) }
    defer { follow.cancel() }
    while inbox.current == nil { try await Task.sleep(for: .milliseconds(10)) }
    #expect(inbox.current?.id == "a")
    inbox.send(Ping.Pong(answer: "Yes"), for: inbox.current!)
    while inbox.current?.id != "b" { try await Task.sleep(for: .milliseconds(10)) }
    #expect(feed.pongs.map(\.0) == ["a"])
}
