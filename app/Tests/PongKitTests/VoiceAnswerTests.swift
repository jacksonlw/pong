import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// A model that always gives the same reply, or fails.
struct FakeModel: VoiceModel {
    var reply: String = #"{"intent":"not_understood"}"#
    var failure: String?

    struct Failed: LocalizedError { var errorDescription: String? }

    func reply(system: String, user: String, schema: String) async throws -> String {
        if let failure { throw Failed(errorDescription: failure) }
        return reply
    }
}

/// A spoken answer becomes a Pong: understanding, the slow fill, and failing open.
@MainActor
final class VoiceAnswerTests: XCTestCase {
    let three = VoicePing(VoiceTests.ping(answers: ["Fix the race", "Quarantine the test", "Revert the retry change", "Nothing more"]))
    let yesNo = VoicePing(VoiceTests.ping(answers: ["Yes", "No", "Nothing more"]))

    // MARK: Local phrases

    func testPhrasesMatchedWithoutAModel() {
        let table: [(String, VoicePing, VoiceIntent?)] = [
            ("The first one.", three, .answer(choices: ["Fix the race"], text: nil)),
            ("the second one", three, .answer(choices: ["Quarantine the test"], text: nil)),
            ("Number three", three, .answer(choices: ["Revert the retry change"], text: nil)),
            ("third", three, .answer(choices: ["Revert the retry change"], text: nil)),
            ("one", three, .answer(choices: ["Fix the race"], text: nil)),
            ("2", three, .answer(choices: ["Quarantine the test"], text: nil)),
            ("Two and three.", three, .answer(choices: ["Quarantine the test", "Revert the retry change"], text: nil)),
            ("the recommended one", three, .answer(choices: ["Fix the race"], text: nil)),
            ("Recommended.", three, .answer(choices: ["Fix the race"], text: nil)),
            ("Yes.", yesNo, .answer(choices: ["Yes"], text: nil)),
            ("no", yesNo, .answer(choices: ["No"], text: nil)),
            ("no", three, .cancel),
            ("yes", three, nil),
            ("Wait.", three, .wait),
            ("hold on", three, .wait),
            ("go", three, .go),
            ("Send it.", three, .go),
            ("stop", three, .stop),
            ("cancel", three, .cancel),
            ("Repeat.", three, .repeatPing),
            ("say that again", three, .repeatPing),
            ("the sixth one", three, nil),
            ("two and three, and add tests", three, nil),
            ("let's quarantine it for now", three, nil),
        ]
        for (words, ping, want) in table {
            XCTAssertEqual(VoiceUnderstanding.local(words, ping), want, words)
        }
        XCTAssertEqual(VoiceUnderstanding.local("no", yesNo, pending: true), .cancel, "no cancels an answer on its way")
    }

    func testWithoutAModelAnythingElseIsNotUnderstood() async throws {
        let i = try await VoiceUnderstanding.understand("let's quarantine it for now", three, model: nil)
        XCTAssertEqual(i, .notUnderstood)
    }

    // MARK: The model

    func testTheModelUnderstandsTheRest() async throws {
        let model = FakeModel(reply: #"{"intent":"answer","choices":["Quarantine the test","Revert the retry change"],"text":"and add tests"}"#)
        let i = try await VoiceUnderstanding.understand("two and three, and add tests", three, model: model)
        XCTAssertEqual(i, .answer(choices: ["Quarantine the test", "Revert the retry change"], text: "and add tests"))
    }

    func testARepliesThatDoNotMatchTheStructureAreNotUnderstood() {
        for reply in ["Sure! The second one.", #"{"intent":"answer"}"#, #"{"intent":"answer","choices":["Something else"]}"#,
                      #"{"intent":"dance"}"#, #"{"choices":["Fix the race"]}"#, #"{"intent":"show_resource","resource":"nope"}"#, ""] {
            XCTAssertEqual(VoiceUnderstanding.parse(reply, three), .notUnderstood, reply)
        }
        XCTAssertEqual(VoiceUnderstanding.parse("```json\n{\"intent\":\"wait\"}\n```", three), .wait, "a fenced reply")
    }

    func testTheModelSeesOnlyTheTranscriptAndThePingsFields() throws {
        var p = VoiceTests.ping()
        p.folder = "/Users/someone/secret"
        p.resources = [Resource(id: "r1", title: "Schema diff", kind: "change", contentType: "text/plain")]
        let req = VoiceUnderstanding.request("the first one", VoicePing(p, quickContext: "Orders need a new column."))
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(req.utf8)) as? [String: Any])
        XCTAssertEqual(Set(json.keys), ["transcript", "ping"])
        let ping = try XCTUnwrap(json["ping"] as? [String: Any])
        XCTAssertEqual(Set(ping.keys), ["question", "answers", "recommended", "allow_several", "quick_context", "resources"])
        XCTAssertFalse(req.contains("secret"))
    }

    // MARK: The slow fill

    func fill() -> (VoiceFill, ManualClock, Box) {
        let clock = ManualClock(), sent = Box()
        let f = VoiceFill(sleep: { await clock.sleep($0) })
        f.onSend = { sent.pongs.append($0) }
        return (f, clock, sent)
    }

    final class Box { var pongs: [Ping.Pong] = [] }

    func testFillsAtReadingPaceThenCountsDownThenSends() async {
        let (f, clock, sent) = fill()
        f.start(Ping.Pong(answer: "Fix the race", text: "add tests"))
        XCTAssertEqual(f.phase, .filling)
        XCTAssertEqual(f.choices, ["Fix the race"], "the choice shows at once")
        await clock.advance(VoiceFill.perCharacter * 3)
        XCTAssertEqual(f.text, "add")
        await clock.advance(VoiceFill.perCharacter * 6)
        XCTAssertEqual(f.text, "add tests")
        XCTAssertEqual(f.phase, .countingDown)
        await clock.advance(.milliseconds(1500))
        XCTAssertEqual(f.progress, 0.5, accuracy: 0.001)
        XCTAssertTrue(sent.pongs.isEmpty)
        await clock.advance(.milliseconds(1500))
        XCTAssertEqual(f.phase, .sent)
        XCTAssertEqual(sent.pongs, [Ping.Pong(answer: "Fix the race", text: "add tests")])
    }

    func testWaitPausesTheBarUntilGo() async {
        let (f, clock, sent) = fill()
        f.start(Ping.Pong(answer: "Fix the race"))
        await clock.advance(.seconds(1))
        f.wait()
        await clock.advance(.seconds(30))
        XCTAssertTrue(f.paused)
        XCTAssertTrue(sent.pongs.isEmpty)
        let held = f.progress
        XCTAssertLessThan(held, 0.5)
        f.go()
        await clock.advance(.milliseconds(1500))
        XCTAssertTrue(sent.pongs.isEmpty, "the bar goes on from where it stopped")
        await clock.advance(.milliseconds(500))
        XCTAssertEqual(sent.pongs.count, 1)
    }

    func testCancelClearsTheAnswer() async {
        let (f, clock, sent) = fill()
        f.start(Ping.Pong(answer: "Fix the race", text: "now"))
        await clock.advance(.seconds(1))
        f.cancel()
        XCTAssertEqual(f.phase, .idle)
        XCTAssertEqual(f.choices, [])
        XCTAssertEqual(f.text, "")
        await clock.advance(.seconds(10))
        XCTAssertTrue(sent.pongs.isEmpty)
    }

    func testANewAnswerReplacesTheOldAndStartsAgain() async {
        let (f, clock, sent) = fill()
        f.start(Ping.Pong(answer: "Fix the race"))
        await clock.advance(.seconds(2))
        f.start(Ping.Pong(answer: "Quarantine the test"))
        XCTAssertEqual(f.progress, 0)
        await clock.advance(.seconds(2))
        XCTAssertTrue(sent.pongs.isEmpty, "the bar started again")
        await clock.advance(.seconds(1))
        XCTAssertEqual(sent.pongs, [Ping.Pong(answer: "Quarantine the test")])
    }

    func testTakingOverStopsTheBarAndKeepsTheAnswer() async {
        let (f, clock, sent) = fill()
        f.start(Ping.Pong(answers: ["Fix the race"], text: "hi"))
        await clock.advance(.seconds(1))
        f.takeOver()
        await clock.advance(.seconds(10))
        XCTAssertEqual(f.phase, .takenOver)
        XCTAssertEqual(f.choices, ["Fix the race"])
        XCTAssertEqual(f.text, "hi")
        XCTAssertTrue(sent.pongs.isEmpty)
    }

    // MARK: Through the session

    func listening(_ ping: Ping, model: VoiceModel? = nil) async -> (VoiceSession, FakeVoiceIO, ManualClock, Box) {
        let io = FakeVoiceIO(), clock = ManualClock(), sent = Box()
        let v = VoiceSession(io: io, sleep: { await clock.sleep($0) })
        v.on = true
        v.autoListen = true
        v.model = { model }
        v.actions = VoiceActions(send: { pong, _ in sent.pongs.append(pong) })
        v.show(ping)
        await clock.settle()
        return (v, io, clock, sent)
    }

    func testASpokenAnswerIsSentAfterTheCountdownAndListeningStops() async {
        let (v, io, clock, sent) = await listening(VoiceTests.ping())
        io.hear(.words("the second one"))
        await clock.settle()
        XCTAssertEqual(v.fill.choices, ["Users only"])
        await clock.advance(.seconds(3))
        XCTAssertEqual(sent.pongs, [Ping.Pong(answer: "Users only")])
        XCTAssertFalse(v.listening)
    }

    func testSeveralChoicesOnASingleChoicePingAreNotSent() async {
        let (v, io, clock, sent) = await listening(VoiceTests.ping())
        io.hear(.words("one and two"))
        await clock.settle()
        await clock.advance(.seconds(5))
        XCTAssertTrue(sent.pongs.isEmpty)
        XCTAssertEqual(io.spoken.last, "Which one: Users and orders, or Users only?")
        XCTAssertTrue(v.listening)
    }

    func testSeveralChoicesOnAMultiChoicePingAreTicked() async {
        let (_, io, clock, sent) = await listening(VoiceTests.ping(several: true))
        io.hear(.words("one and two"))
        await clock.settle()
        await clock.advance(.seconds(3))
        XCTAssertEqual(sent.pongs, [Ping.Pong(answers: ["Users and orders", "Users only"])])
    }

    func testWithoutAModelPongSuggestsSayingANumber() async {
        let (_, io, clock, sent) = await listening(VoiceTests.ping())
        io.hear(.words("let's do just users"))
        await clock.settle()
        XCTAssertEqual(io.spoken.last, "I didn't understand. Say a number, like \"the first one\".")
        XCTAssertTrue(sent.pongs.isEmpty)
    }

    func testNoCancelsAndListeningContinues() async {
        let (v, io, clock, sent) = await listening(VoiceTests.ping())
        io.hear(.words("first"))
        await clock.settle()
        await clock.advance(.seconds(1))
        io.hear(.words("No."))
        await clock.settle()
        await clock.advance(.seconds(5))
        XCTAssertTrue(sent.pongs.isEmpty)
        XCTAssertEqual(v.fill.phase, .idle)
        XCTAssertTrue(v.listening)
    }

    func testAModelFailureIsSaidOnceStopsListeningAndSendsNothing() async {
        let (v, io, clock, sent) = await listening(VoiceTests.ping(), model: FakeModel(failure: "the endpoint could not be reached"))
        io.hear(.words("first"))
        await clock.settle()
        await clock.advance(.seconds(1))
        io.hear(.words("let's do just users"))
        await clock.settle()
        await clock.advance(.seconds(5))
        XCTAssertTrue(sent.pongs.isEmpty, "the answer on its way is dropped too")
        XCTAssertEqual(v.fill.phase, .idle)
        XCTAssertFalse(v.listening)
        XCTAssertEqual(v.problem, "The voice model failed: the endpoint could not be reached")
        XCTAssertEqual(io.spoken.filter { $0 == v.problem }.count, 1)
    }

    func testClickingTheCardTakesOver() async {
        let (v, io, clock, sent) = await listening(VoiceTests.ping())
        io.hear(.words("first"))
        await clock.settle()
        v.takeOver()
        await clock.advance(.seconds(5))
        XCTAssertTrue(sent.pongs.isEmpty)
        XCTAssertEqual(v.fill.phase, .takenOver)
        XCTAssertFalse(v.listening)
    }

    // MARK: The card

    func testSnapshotCountdown() async throws {
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 3 && $0.allowSeveral == nil })
        let (v, io, clock, _) = await listening(ping)
        let model = FakeModel(reply: #"{"intent":"answer","choices":["Quarantine the test"],"text":"and open an issue"}"#)
        v.model = { model }
        io.hear(.words("quarantine it and open an issue"))
        await clock.settle()
        await clock.advance(VoiceFill.perCharacter * 17 + .milliseconds(1500))
        XCTAssertEqual(v.fill.phase, .countingDown)
        try assertSnapshot(CardView(ping: ping, voice: v), named: "voice-countdown")
    }
}
