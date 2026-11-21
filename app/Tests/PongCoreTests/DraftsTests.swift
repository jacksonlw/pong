import Foundation
import Testing
import PongCore

/// A reply the human started is never lost: not by leaving, quitting or a refused Pong.
@MainActor
struct DraftsTests {
    let file = FileManager.default.temporaryDirectory
        .appendingPathComponent("drafts-\(UUID().uuidString)", isDirectory: true)
        .appendingPathComponent("drafts.json")

    @Test func aDraftOutlivesTheApp() {
        let session = SessionThread.SessionRef(harness: "claude-code", id: "s")
        Drafts(file: file).set("half a thought", .ping("p"))
        Drafts(file: file).set("to the Session", .session(session))
        let reopened = Drafts(file: file)
        #expect(reopened.text(.ping("p")) == "half a thought")
        #expect(reopened.text(.session(session)) == "to the Session")
        reopened.sent(.ping("p"))
        #expect(Drafts(file: file).text(.ping("p")) == "")
        #expect(Drafts(file: file).text(.session(session)) == "to the Session")
    }

    @Test func typedTextIsKeptAtOnceAndGoesOnlyWhenThePongIsTaken() async throws {
        let feed = RefusingFeed([Ping(id: "a", session: "s", question: "One?", state: .waiting)])
        let inbox = Inbox()
        inbox.drafts = Drafts(file: file)
        let follow = Task { await inbox.follow(clientProvider: { feed }) }
        defer { follow.cancel() }
        while inbox.current == nil { try await Task.sleep(for: .milliseconds(10)) }
        let ping = inbox.current!

        inbox.draft("keep this", for: ping)
        #expect(Drafts(file: file).text(.ping("a")) == "keep this", "on disk before it is even shared")

        feed.refuses = true
        inbox.send(Ping.Pong(text: "keep this"), for: ping)
        while inbox.refusals["a"] == nil { try await Task.sleep(for: .milliseconds(10)) }
        #expect(Drafts(file: file).text(.ping("a")) == "keep this", "a refused Pong keeps its text")

        feed.refuses = false
        inbox.send(Ping.Pong(text: "keep this"), for: ping)
        while !feed.taken { try await Task.sleep(for: .milliseconds(10)) }
        while inbox.drafts.text(.ping("a")) != "" { try await Task.sleep(for: .milliseconds(10)) }
        #expect(Drafts(file: file).text(.ping("a")) == "")
    }
}

final class RefusingFeed: PingFeed, @unchecked Sendable {
    var pings: [Ping]
    var refuses = false
    var taken = false
    init(_ pings: [Ping]) { self.pings = pings }
    func allPings() async throws -> [Ping] { pings }
    func pong(_ id: String, _ p: Ping.Pong) async throws {
        if refuses { throw No() }
        taken = true
    }
    func handBack(_ id: String) async throws {}
    func dismiss(_ id: String) async throws {}
    func resource(_ r: String, of p: String) async throws -> Data { Data() }
    func events(since: Int) -> AsyncThrowingStream<EngineEvent, Error> { AsyncThrowingStream { _ in } }
    var unreachable: String { "The feed is out of reach." }
}

private struct No: FeedRefusal { var message: String? { "No." } }
