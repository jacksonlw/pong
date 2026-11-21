import Foundation
import Testing
import PongCore

/// A feed that sends a burst of events and counts how often its list is read.
final class BurstFeed: PingFeedAtSeq, @unchecked Sendable {
    let lock = NSLock()
    var reads = 0
    var since: Int?
    var pings = [Ping(id: "a", session: "s", question: "One?", state: .waiting)]
    func allPings() async throws -> [Ping] { try await allPingsAtSeq().0 }
    func allPingsAtSeq() async throws -> ([Ping], Int) {
        try await Task.sleep(for: .milliseconds(20)) // a fetch takes a moment, as the Engine's does
        return lock.withLock { reads += 1; return (pings, 41) }
    }
    func pong(_ id: String, _ p: Ping.Pong) async throws {}
    func handBack(_ id: String) async throws {}
    func dismiss(_ id: String) async throws {}
    func resource(_ r: String, of p: String) async throws -> Data { Data() }
    func events(since: Int) -> AsyncThrowingStream<EngineEvent, Error> {
        lock.withLock { self.since = since }
        return AsyncThrowingStream { c in
            Task {
                for i in 1...100 { c.yield(EngineEvent(seq: 41 + i, kind: "session.changed")) }
                try? await Task.sleep(for: .milliseconds(50))
                self.lock.withLock { self.pings.append(Ping(id: "b", session: "t", question: "Two?", state: .waiting)) }
                c.yield(EngineEvent(seq: 142, kind: "ping.created"))
            }
        }
    }
    var unreachable: String { "out of reach" }
}

/// The Inbox follows from the list's seq, and reads the list once for a burst of events.
@MainActor
@Test func aBurstOfEventsIsOneFetchNotOneEach() async throws {
    let feed = BurstFeed()
    let inbox = Inbox()
    let follow = Task { await inbox.follow(clientProvider: { feed }) }
    defer { follow.cancel() }
    for _ in 0..<200 where !inbox.deck.contains(where: { $0.id == "b" }) { try await Task.sleep(for: .milliseconds(10)) }
    #expect(inbox.deck.map(\.id) == ["a", "b"])
    #expect(feed.lock.withLock { feed.since } == 41)
    let reads = feed.lock.withLock { feed.reads }
    #expect(reads < 10, "\(reads) reads for 101 events")
}

/// A snapshot replaces what the Directory held: a Session or Ping it leaves out is gone.
@MainActor
@Test func aSnapshotReplacesWhatTheDirectoryHeld() throws {
    let d = Directory()
    d.apply(#"data: {"seq":1,"kind":"session.changed","data":{"harness":"claude-code","id":"old","state":"ended","started_by":"human"}}"#)
    d.apply(#"data: {"seq":2,"kind":"ping.created","data":{"id":"p1","session":"old","question":"Q?","state":"waiting"}}"#)
    #expect(d.sessions.count == 1)
    let snapshot = #"{"seq":9,"events":[{"seq":9,"kind":"session.changed","data":{"harness":"claude-code","id":"new","state":"working","started_by":"human"}}]}"#
    #expect(d.apply(snapshot: Data(snapshot.utf8)) == 9)
    #expect(d.sessions.values.map(\.id) == ["new"])
    #expect(d.pings.isEmpty)
}
