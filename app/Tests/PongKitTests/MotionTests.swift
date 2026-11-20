import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// A clock the test moves by hand: `sleep` waits until `advance` passes its deadline.
@MainActor
final class ManualClock {
    private var now: Duration = .zero
    private var sleepers: [(due: Duration, wake: CheckedContinuation<Void, Never>)] = []

    func sleep(_ d: Duration) async {
        await withCheckedContinuation { sleepers.append((now + d, $0)) }
    }

    /// Moves time on by `d`, waking each sleeper at its own deadline in turn, so sleeps that
    /// begin along the way are honoured too.
    func advance(_ d: Duration) async {
        let end = now + d
        while true {
            await yieldAll()
            guard let next = sleepers.map(\.due).min(), next <= end else { break }
            now = max(now, next)
            let due = sleepers.filter { $0.due <= now }
            sleepers.removeAll { $0.due <= now }
            due.forEach { $0.wake.resume() }
        }
        now = end
    }

    func settle() async { await advance(.zero) }

    private func yieldAll() async {
        for _ in 0..<20 { await Task.yield() }
    }
}

/// The card's motion and "more coming", from Pings recorded from a real Engine
/// (Fixtures/more-coming.sse) and the CardStage that sequences changes.
@MainActor
final class MotionTests: XCTestCase {
    static func pings(upTo seq: Int = .max) throws -> [Ping] {
        let text = try String(contentsOf: CardSnapshotTests.here.appendingPathComponent("Fixtures/more-coming.sse"), encoding: .utf8)
        var last: [String: Ping] = [:]
        var order: [String] = []
        for e in EventLine.parseAll(text) where e.seq <= seq {
            guard let p = e.ping else { continue }
            if last[p.id] == nil { order.append(p.id) }
            last[p.id] = p
        }
        return order.compactMap { last[$0] }
    }

    func scene(_ id: String, writing: Bool = false) -> CardScene {
        CardScene(ping: Ping(id: id, session: "s", question: "Question \(id)?", state: "waiting"), writing: writing)
    }

    // MARK: More coming

    func testBadgeWhileTheSessionsLatestPingSaysMoreAreComing() throws {
        let inbox = Inbox(pings: try Self.pings())
        while inbox.current?.session != "s-asking" { inbox.next() }
        XCTAssertTrue(inbox.moreComing)
        while inbox.current?.session != "s-steps" { inbox.next() }
        XCTAssertFalse(inbox.moreComing, "s-steps' last Ping has arrived")
    }

    func testAnsweringBeforeTheNextArrivesShowsItIsBeingWritten() throws {
        let first = try Self.pings(upTo: 3)
        let inbox = Inbox(pings: first)
        let ping = try XCTUnwrap(inbox.current)
        XCTAssertEqual(ping.moreComing, true)
        inbox.apply(try Self.pings(upTo: 4))                 // the Engine reports it answered
        XCTAssertTrue(inbox.isWriting)
        XCTAssertEqual(inbox.scene, CardScene(ping: try XCTUnwrap(inbox.current), writing: true))
        XCTAssertFalse(inbox.moreComing, "the card says it itself; no badge as well")
        inbox.apply(try Self.pings(upTo: 6))                 // another Session's Ping does not end the wait
        XCTAssertTrue(inbox.isWriting)
        inbox.apply(try Self.pings())                        // s-steps' next Ping lands
        XCTAssertFalse(inbox.isWriting)
        XCTAssertEqual(inbox.current?.question, "Run the migrations on deploy?")
    }

    func testTheWaitEndsWhenTheEngineTakesMoreComingBack() throws {
        let inbox = Inbox(pings: try Self.pings(upTo: 3))
        inbox.apply(try Self.pings(upTo: 6))                 // answered, with another Session's Ping waiting
        XCTAssertTrue(inbox.isWriting)
        var taken = try Self.pings(upTo: 6)                  // the Session's turn ended: its next Ping will not come
        for i in taken.indices where taken[i].session == "s-steps" { taken[i].moreComing = nil }
        inbox.apply(taken)
        XCTAssertFalse(inbox.isWriting)
        XCTAssertEqual(inbox.current?.session, "s-asking", "the card moves on to the next waiting Ping")
    }

    // MARK: Sequencing

    func testOldContentFadesOutBeforeNewFadesIn() async {
        let clock = ManualClock()
        let stage = CardStage(sleep: { await clock.sleep($0) })
        var steps: [String] = []
        stage.onStep = { s, v in steps.append("\(s?.key ?? "-") \(v ? "in" : "out")") }
        stage.set(scene("a"))
        stage.measured(200, for: "a")
        stage.set(scene("b"))
        await clock.settle()
        XCTAssertEqual(stage.shown?.key, "a", "still fading out")
        stage.measured(300, for: "b")                       // not shown yet: ignored
        XCTAssertEqual(stage.height, 200)
        await clock.advance(.milliseconds(100))
        XCTAssertEqual(stage.shown?.key, "b")
        XCTAssertFalse(stage.visible, "hidden while the box eases to its height")
        stage.measured(120, for: "b")
        XCTAssertEqual(stage.height, 120)
        await clock.advance(.milliseconds(49))
        XCTAssertFalse(stage.visible)
        await clock.advance(.milliseconds(1))
        XCTAssertTrue(stage.visible)
        await clock.advance(.milliseconds(200))
        XCTAssertEqual(steps, ["a in", "a out", "b out", "b in"])
    }

    func testContentSlidesTheWayTheHumanMoves() async {
        let clock = ManualClock()
        let stage = CardStage(sleep: { await clock.sleep($0) })
        let a = CardScene(ping: scene("a").ping, position: 0), b = CardScene(ping: scene("b").ping, position: 1)
        stage.set(a)
        stage.set(b)
        await clock.settle()
        XCTAssertEqual(stage.slide, -1, "the old content leaves to the left")
        await clock.advance(.milliseconds(100))
        XCTAssertEqual(stage.slide, 1, "the new content waits on the right")
        await clock.advance(.milliseconds(50))
        XCTAssertEqual(stage.slide, 0)
        await clock.advance(.milliseconds(200))
        stage.set(a)
        await clock.settle()
        XCTAssertEqual(stage.slide, 1, "going back, the old content leaves to the right")
        await clock.advance(.milliseconds(100))
        XCTAssertEqual(stage.slide, -1)
    }

    func testChosenAnswerShowsBeforeTheCardMovesOn() async {
        let clock = ManualClock()
        let stage = CardStage(sleep: { await clock.sleep($0) })
        stage.set(scene("a"))
        stage.choose(Ping.Pong(answer: "Yes"))
        stage.set(scene("b"))
        await clock.advance(.milliseconds(299))
        XCTAssertEqual(stage.shown?.ping.pong?.answer, "Yes")
        XCTAssertTrue(stage.visible)
        await clock.advance(.milliseconds(1))
        XCTAssertFalse(stage.visible)
        await clock.advance(.milliseconds(100))
        XCTAssertEqual(stage.shown?.key, "b")
    }

    func testReplyMovesOnLikeTheArrows() async {
        let clock = ManualClock()
        let stage = CardStage(sleep: { await clock.sleep($0) })
        var steps: [String] = []
        stage.onStep = { s, v in steps.append("\(s?.key ?? "-") \(v ? "in" : "out")") }
        stage.set(scene("a"))
        stage.choose(Ping.Pong(answers: ["Yes"], text: "After lunch"))
        XCTAssertNil(stage.shown?.ping.pong, "the reply is not added to the answers")
        stage.set(scene("b"))
        await clock.settle()
        XCTAssertFalse(stage.visible, "no hold: it fades out at once")
        await clock.advance(.milliseconds(350))
        XCTAssertEqual(steps, ["a in", "a out", "b out", "b in"])
    }

    func testTextTypedWithAChosenAnswerIsNotShown() {
        let stage = CardStage(sleep: { _ in })
        stage.set(scene("a"))
        stage.choose(Ping.Pong(answer: "Yes", text: "After lunch"))
        XCTAssertEqual(stage.shown?.ping.pong, Ping.Pong(answer: "Yes"))
    }

    /// Many Pongs and arrivals in quick succession, at uneven moments: the card ends on the
    /// latest scene at its height, and a change of content only ever happens while hidden.
    func testRapidAnsweringLeavesNoStaleContentOrHeight() async {
        var rng = SystemRandomNumberGenerator()
        for reduced in [false, true] {
            let clock = ManualClock()
            let stage = CardStage(timing: reduced ? .reduced : .standard, sleep: { await clock.sleep($0) })
            var last: (key: String?, visible: Bool) = (nil, true)
            var violations: [String] = []
            stage.onStep = { s, v in
                if last.key != nil, s?.key != last.key, last.visible || v {
                    violations.append("\(last.key ?? "-") → \(s?.key ?? "-") while visible")
                }
                last = (s?.key, v)
            }
            var latest = scene("p0")
            stage.set(latest)
            for i in 1...300 {
                switch Int.random(in: 0..<4, using: &rng) {
                case 0: stage.choose(Ping.Pong(answer: "Yes"))
                case 1: latest = scene("p\(i)", writing: Bool.random(using: &rng))
                case 2: latest = scene("p\(i)")
                default: break
                }
                stage.set(latest)
                stage.measured(CGFloat(i), for: "p\(i - 1)") // late layouts of earlier content
                if let k = stage.shown?.key { stage.measured(CGFloat(1000 + i), for: k) }
                await clock.advance(.milliseconds(Int.random(in: 0...150, using: &rng)))
            }
            await clock.advance(.seconds(2))
            XCTAssertEqual(stage.shown, latest)
            XCTAssertTrue(stage.visible)
            stage.measured(321, for: latest.key)
            stage.measured(999, for: "p0")
            XCTAssertEqual(stage.height, 321)
            XCTAssertEqual(violations, [], reduced ? "reduced motion" : "standard")
        }
    }

    func testHidingAfterTheLastAnswerStillShowsTheChoice() async {
        let clock = ManualClock()
        let stage = CardStage(sleep: { await clock.sleep($0) })
        stage.set(scene("a"))
        stage.choose(Ping.Pong(answer: "Yes"))
        stage.set(nil)
        await clock.advance(.milliseconds(100))
        XCTAssertNotNil(stage.shown)
        await clock.advance(.seconds(1))
        XCTAssertNil(stage.shown)
    }

    // MARK: Snapshots

    func testTypingBadge() throws {
        try assertSnapshot(TypingBadge().padding(8).environment(\.typingAnimates, false), named: "typing-badge")
    }

    func testNextQuestionBeingWritten() throws {
        let answered = try XCTUnwrap(Self.pings(upTo: 4).first)
        try assertSnapshot(CardView(ping: answered, writing: true).environment(\.typingAnimates, false), named: "being-written")
    }

}
