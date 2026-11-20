import XCTest
@testable import PongKit

/// Voice explains and shows context.
@MainActor
final class VoiceExplainTests: XCTestCase {
    final class Log { var lines: [String] = [] }

    static func ping() -> Ping {
        var p = VoiceTests.ping()
        p.resources = [Resource(id: "r1", title: "Schema diff", kind: "change", contentType: "text/x-diff"),
                       Resource(id: "r2", title: "Orders screenshot", kind: "image", contentType: "image/png"),
                       Resource(id: "r3", title: "Users screenshot", kind: "image", contentType: "image/png")]
        return p
    }

    func listening(model: VoiceModel? = nil) async -> (VoiceSession, FakeVoiceIO, ManualClock, Log) {
        let io = FakeVoiceIO(), clock = ManualClock(), log = Log()
        let v = VoiceSession(io: io, sleep: { await clock.sleep($0) })
        v.on = true
        v.autoListen = true
        v.model = { model }
        v.quickContext = { _ in "Orders need a new column. Users keep theirs. The migration runs at night. Nothing else changes." }
        v.actions = VoiceActions(showContext: { _, open in log.lines.append(open ? "context shown" : "context hidden") },
                                 openResource: { r, _ in log.lines.append("opened \(r.title)") })
        v.show(Self.ping())
        await clock.settle()
        return (v, io, clock, log)
    }

    func say(_ words: String, _ io: FakeVoiceIO, _ clock: ManualClock) async {
        io.hear(.words(words))
        await clock.settle()
    }

    func testExplainReadsTheModelsExplanation() async {
        let (v, io, clock, _) = await listening(model: FakeModel(reply: #"{"explanation":"The agent wants to know which tables to change. The first answer touches both."}"#))
        await say("What is this about?", io, clock)
        XCTAssertEqual(io.spoken.last, "The agent wants to know which tables to change. The first answer touches both.")
        XCTAssertTrue(v.listening)
    }

    func testWithoutAModelExplainReadsTheQuickContextInThreeSentences() async {
        let (_, io, clock, _) = await listening()
        await say("explain", io, clock)
        XCTAssertEqual(io.spoken.last, "Orders need a new column. Users keep theirs. The migration runs at night.")
    }

    func testShowAndHideTheContext() async {
        let (v, io, clock, log) = await listening()
        await say("Show me the context.", io, clock)
        await say("hide it", io, clock)
        XCTAssertEqual(log.lines, ["context shown", "context hidden"])
        XCTAssertTrue(v.listening)
    }

    func testNamingAResourceOpensIt() async {
        let (_, io, clock, log) = await listening()
        await say("show the diff", io, clock)
        await say("open the orders screenshot", io, clock)
        XCTAssertEqual(log.lines, ["opened Schema diff", "opened Orders screenshot"])
    }

    func testMoreThanOneMatchAsksWhichOne() async {
        let (_, io, clock, log) = await listening()
        await say("open the screenshot", io, clock)
        XCTAssertEqual(log.lines, [])
        XCTAssertEqual(io.spoken.last, "Which one: Orders screenshot, or Users screenshot?")
    }

    func testRepeatReadsThePingAgain() async {
        let (_, io, clock, _) = await listening()
        await say("repeat", io, clock)
        XCTAssertEqual(io.spoken, [VoiceScript.reading(Self.ping()), VoiceScript.reading(Self.ping())])
    }

    func testListeningContinuesAndTheSilenceTimerRestarts() async {
        let (v, io, clock, _) = await listening()
        await clock.advance(.seconds(25))
        await say("show the context", io, clock)
        await clock.advance(.seconds(25))
        XCTAssertTrue(v.listening)
        await clock.advance(.seconds(5))
        XCTAssertFalse(v.listening)
    }
}
