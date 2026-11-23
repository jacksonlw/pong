import XCTest
@testable import PongCore
@testable import PongKit

/// Speak: what the human says becomes text at the caret, live, and is never sent.
@MainActor
final class SpeakTests: XCTestCase {
    /// A box's text, as a reply box keeps it.
    final class Box {
        var text: String
        init(_ text: String) { self.text = text }
    }

    func speak(refused: String? = nil) -> (Speak, FakeVoiceIO, ManualClock) {
        let io = FakeVoiceIO(), clock = ManualClock()
        let s = Speak(io: io, permissions: FakePermissions(refused: refused), sleep: { await clock.sleep($0) })
        return (s, io, clock)
    }

    func start(_ s: Speak, _ box: Box, at caret: Int?, in b: SpeakBox = .start, clock: ManualClock) async {
        s.start(b, read: { box.text }, write: { box.text = $0 }, caret: caret)
        await clock.settle()
    }

    func testWritingGoesInAtTheCaretApartFromTheTextAround() {
        var w = SpeakWriting(text: "Fix the total", caret: 3)
        w.heard(partial: "checkout")
        XCTAssertEqual(w.text, "Fix checkout the total")
        w.heard(words: "checkout and")
        w.heard(partial: "tax")
        XCTAssertEqual(w.text, "Fix checkout and tax the total")
        XCTAssertEqual(SpeakWriting(text: "", caret: nil).text, "")
        var end = SpeakWriting(text: "Fix it. ", caret: nil)
        end.heard(words: "Now")
        XCTAssertEqual(end.text, "Fix it. Now", "no second space after one already there")
    }

    func testPartialWordsReplaceThemselvesAndFinishedOnesStay() async {
        let (s, io, clock) = speak()
        let box = Box("")
        await start(s, box, at: nil, clock: clock)
        XCTAssertTrue(io.listening)
        XCTAssertEqual(s.box, .start)
        io.hear(.partial("add a"))
        io.hear(.partial("add a coupon"))
        XCTAssertEqual(box.text, "add a coupon")
        io.hear(.words("add a coupon field"))
        io.hear(.partial("on the"))
        XCTAssertEqual(box.text, "add a coupon field on the")
        io.hear(.level(0.5))
        XCTAssertEqual(s.level, 0.5)
    }

    func testStoppingKeepsTheTextAndSendsNothing() async {
        let (s, io, clock) = speak()
        let box = Box("Hello")
        await start(s, box, at: nil, clock: clock)
        io.hear(.words("there"))
        s.stop()
        XCTAssertFalse(io.listening)
        XCTAssertNil(s.box)
        io.hear(.words("ignored"))
        XCTAssertEqual(box.text, "Hello there")
    }

    func testTheButtonStartsAndStopsInItsOwnBoxOnly() async {
        let (s, io, clock) = speak()
        let a = Box(""), b = Box("")
        await start(s, a, at: nil, in: .ping("p1"), clock: clock)
        s.stop(.ping("p2"))
        XCTAssertTrue(io.listening, "another box's stop leaves it")
        await start(s, b, at: nil, in: .ping("p2"), clock: clock)
        XCTAssertEqual(s.box, .ping("p2"), "one box at a time")
        io.hear(.words("second"))
        XCTAssertEqual(a.text, "")
        XCTAssertEqual(b.text, "second")
    }

    func testItStopsByItselfAfterFiveMinutes() async {
        let (s, io, clock) = speak()
        await start(s, Box(""), at: nil, clock: clock)
        await clock.advance(.seconds(299))
        XCTAssertTrue(io.listening)
        await clock.advance(.seconds(1))
        XCTAssertFalse(io.listening)
        XCTAssertNil(s.box)
    }

    func testTextTypedWhileListeningIsKeptAndSpeakWritesAfterIt() async {
        let (s, io, clock) = speak()
        let box = Box("")
        await start(s, box, at: nil, clock: clock)
        io.hear(.words("first"))
        box.text = "first, typed"
        io.hear(.words("then spoken"))
        XCTAssertEqual(box.text, "first, typed then spoken")
    }

    func testARefusedPermissionSaysWhyInThatBoxAndDoesNotListen() async {
        let (s, io, clock) = speak(refused: "Pong needs the microphone.")
        await start(s, Box(""), at: nil, in: .ping("p1"), clock: clock)
        XCTAssertFalse(io.listening)
        XCTAssertEqual(s.problem?.box, .ping("p1"))
        XCTAssertEqual(s.problem?.text, "Pong needs the microphone.")
    }

    func testFailingRecognitionStopsAndSaysWhy() async {
        let (s, io, clock) = speak()
        await start(s, Box(""), at: nil, clock: clock)
        io.hear(.failed("no network"))
        XCTAssertNil(s.box)
        XCTAssertEqual(s.problem?.text, "Speech recognition stopped: no network")
    }

    func testStartingSpeakStopsVoiceModeListening() async {
        let io = FakeVoiceIO(), clock = ManualClock()
        let voice = VoiceSession(io: io, sleep: { await clock.sleep($0) })
        voice.on = true
        voice.autoListen = true
        voice.show(VoiceTests.ping())
        await clock.settle()
        XCTAssertTrue(voice.listening)
        let s = Speak(io: io, sleep: { await clock.sleep($0) })
        s.voice = voice
        await start(s, Box(""), at: nil, clock: clock)
        XCTAssertFalse(voice.listening)
        XCTAssertTrue(io.listening, "the microphone is Speak's now")
    }

    func testSpokenTextSurvivesLeavingTheBoxAndANewDraftsStore() async throws {
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("speak-drafts-\(UUID()).json")
        defer { try? FileManager.default.removeItem(at: file) }
        let drafts = Drafts(file: file)
        let (s, io, clock) = speak()
        s.start(.ping("p1"), read: { drafts.text(.ping("p1")) }, write: { drafts.set($0, .ping("p1")) }, caret: nil)
        await clock.settle()
        io.hear(.partial("use the"))
        io.hear(.words("use the second plan"))
        s.stop(.ping("p1")) // the box closes
        XCTAssertEqual(Drafts(file: file).text(.ping("p1")), "use the second plan")
    }

    // MARK: The shortcut

    func testTheShortcutsTargetIsTheFocusedBoxThenAWaitingCardThenNone() {
        let card = SpeakBox.ping("p1"), thread = SpeakBox.session("claude-code/s1")
        XCTAssertEqual(Speak.target(focused: thread, on: [card, thread]), thread)
        XCTAssertEqual(Speak.target(focused: nil, on: [card, thread]), card)
        XCTAssertEqual(Speak.target(focused: .start, on: [card]), card, "a box no longer on screen is not chosen")
        XCTAssertNil(Speak.target(focused: nil, on: [thread]), "a Thread without focus is not chosen")
        XCTAssertNil(Speak.target(focused: nil, on: []))
    }

    func testTheShortcutStartsInTheFocusedBoxAndStopsOnTheNextPress() async {
        let (s, io, clock) = speak()
        let card = Box(""), thread = Box("")
        s.attach(.ping("p1"), read: { card.text }, write: { card.text = $0 }, caret: { nil })
        s.attach(.session("c/s1"), read: { thread.text }, write: { thread.text = $0 }, caret: { nil })
        s.focus(.session("c/s1"), true)
        var opened = 0
        s.shortcut(openStart: { opened += 1 })
        await clock.settle()
        XCTAssertEqual(s.box, .session("c/s1"))
        io.hear(.words("hello"))
        XCTAssertEqual(thread.text, "hello")
        s.shortcut(openStart: { opened += 1 })
        XCTAssertNil(s.box)
        XCTAssertFalse(io.listening)
        XCTAssertEqual(opened, 0)
    }

    func testTheShortcutOpensTheStartPanelAndStartsThereOnceItShows() async {
        let (s, io, clock) = speak()
        let start = Box("")
        var opened = 0
        s.shortcut(openStart: { opened += 1 })
        XCTAssertEqual(opened, 1)
        XCTAssertFalse(io.listening, "nothing to write in yet")
        s.attach(.start, read: { start.text }, write: { start.text = $0 }, caret: { nil })
        await clock.settle()
        XCTAssertEqual(s.box, .start)
        io.hear(.words("fix the build"))
        XCTAssertEqual(start.text, "fix the build")
    }

    func testABoxThatGoesAwayStopsSpeakInIt() async {
        let (s, io, clock) = speak()
        let card = Box("")
        s.attach(.ping("p1"), read: { card.text }, write: { card.text = $0 }, caret: { nil })
        s.shortcut(openStart: {})
        await clock.settle()
        XCTAssertTrue(io.listening)
        s.detach(.ping("p1"))
        XCTAssertFalse(io.listening)
        XCTAssertNil(s.box)
    }
}
