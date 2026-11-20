import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Stands in for the Mac's hearing and speaking: keeps what was said, and lets a test speak.
@MainActor
final class FakeVoiceIO: VoiceIO {
    var spoken: [String] = []
    var listening = false
    private var onHeard: (@MainActor (Heard) -> Void)?

    func speak(_ text: String) async { spoken.append(text) }
    func stopSpeaking() {}
    func listen(_ onHeard: @escaping @MainActor (Heard) -> Void) {
        listening = true
        self.onHeard = onHeard
    }
    func stopListening() {
        listening = false
        onHeard = nil
    }

    func hear(_ h: Heard) { onHeard?(h) }
}

struct FakePermissions: VoicePermissions {
    var refused: String?
    func request() async -> String? { refused }
}

/// Voice mode turned on: reading aloud, listening with the indicator, and
/// when listening stops.
@MainActor
final class VoiceTests: XCTestCase {
    static func ping(_ id: String = "p1", answers: [String] = ["Users and orders", "Users only", "Nothing more"],
                     several: Bool = false, note: Bool = false) -> Ping {
        var p = Ping(id: id, session: "s", question: "Which tables should the migration touch?", state: "waiting")
        p.answers = answers.map { Ping.Answer(label: $0) }
        p.allowSeveral = several ? true : nil
        p.isNote = note ? true : nil
        return p
    }

    func session(auto: Bool = true) -> (VoiceSession, FakeVoiceIO, ManualClock) {
        let io = FakeVoiceIO(), clock = ManualClock()
        let v = VoiceSession(io: io, sleep: { await clock.sleep($0) })
        v.on = true
        v.autoListen = auto
        return (v, io, clock)
    }

    func testReadsTheQuestionThenTheAnswersNumberedWithTheRecommendedOne() {
        XCTAssertEqual(VoiceScript.reading(Self.ping()),
                       "Which tables should the migration touch? 1: Users and orders, recommended. 2: Users only. 3: Nothing more.")
        XCTAssertEqual(VoiceScript.reading(Self.ping(answers: [], note: true)), "Which tables should the migration touch?")
    }

    func testAutoListenReadsAndListensAsAPingAppears() async {
        let (v, io, clock) = session()
        v.show(Self.ping())
        await clock.settle()
        XCTAssertEqual(io.spoken.count, 1)
        XCTAssertTrue(v.listening)
        XCTAssertTrue(io.listening)
    }

    func testWithoutAutoListenTheMicrophoneButtonStartsIt() async {
        let (v, io, clock) = session(auto: false)
        v.show(Self.ping())
        await clock.settle()
        XCTAssertFalse(v.listening)
        XCTAssertTrue(io.spoken.isEmpty)
        v.start()
        await clock.settle()
        XCTAssertTrue(v.listening)
        XCTAssertEqual(io.spoken.count, 1)
    }

    func testVoiceOffNeverListens() async {
        let (v, io, clock) = session()
        v.on = false
        v.show(Self.ping())
        v.start()
        await clock.settle()
        XCTAssertFalse(io.listening)
        XCTAssertTrue(io.spoken.isEmpty)
    }

    func testListeningShowsTheLevel() async {
        let (v, io, clock) = session()
        v.show(Self.ping())
        await clock.settle()
        io.hear(.level(0.6))
        XCTAssertEqual(v.level, 0.6)
    }

    func testThirtySecondsOfSilenceStopsListening() async {
        let (v, io, clock) = session()
        v.show(Self.ping())
        await clock.settle()
        await clock.advance(.seconds(20))
        io.hear(.words("hmm"))                      // speech restarts the silence timer
        await clock.advance(.seconds(20))
        XCTAssertTrue(v.listening)
        await clock.advance(.seconds(10))
        XCTAssertFalse(v.listening)
        XCTAssertFalse(io.listening)
    }

    func testStopEndsListening() async {
        let (v, io, clock) = session()
        v.show(Self.ping())
        await clock.settle()
        io.hear(.words("Stop."))
        await clock.settle()
        XCTAssertFalse(v.listening)
        XCTAssertFalse(io.listening)
    }

    func testAnotherPingOrAHiddenCardEndsListening() async {
        let (v, io, clock) = session(auto: false)
        v.show(Self.ping())
        v.start()
        await clock.settle()
        v.show(nil)
        XCTAssertFalse(io.listening)
    }

    func testANoteIsReadAndListenedToOnlyForDismiss() async {
        let (v, io, clock) = session()
        var dismissed: [String] = []
        var sent = 0
        v.actions = VoiceActions(send: { _, _ in sent += 1 }, dismiss: { dismissed.append($0.id) })
        v.show(Self.ping("n1", answers: [], note: true))
        await clock.settle()
        XCTAssertEqual(io.spoken, ["Which tables should the migration touch?"])
        io.hear(.words("the first one"))
        await clock.settle()
        XCTAssertEqual(sent, 0)
        XCTAssertTrue(dismissed.isEmpty)
        io.hear(.words("Dismiss."))
        await clock.settle()
        XCTAssertEqual(dismissed, ["n1"])
        XCTAssertFalse(v.listening)
    }

    func testRecognitionFailureIsSaidOnceAndListeningStops() async {
        let (v, io, clock) = session()
        var sent = 0
        v.actions = VoiceActions(send: { _, _ in sent += 1 })
        v.show(Self.ping())
        await clock.settle()
        io.hear(.failed("the microphone is in use"))
        await clock.settle()
        XCTAssertFalse(v.listening)
        XCTAssertEqual(v.problem, "Speech recognition stopped: the microphone is in use")
        XCTAssertEqual(io.spoken.last, v.problem)
        v.start()
        await clock.settle()
        io.hear(.failed("the microphone is in use"))
        await clock.settle()
        XCTAssertEqual(io.spoken.filter { $0 == v.problem }.count, 1, "said once")
        XCTAssertEqual(sent, 0)
    }

    // MARK: Settings

    func testTurningVoiceOnAsksForPermissionAndStaysOffIfRefused() async throws {
        let engine = try FakeEngine()
        let refused = SettingsModel(api: engine, login: FakeLogin(.enabled), permissions: FakePermissions(refused: "Open System Settings"))
        await refused.load()
        await refused.setVoice(true)
        XCTAssertFalse(refused.voice)
        XCTAssertEqual(refused.voiceProblem, "Open System Settings")
        let calls = await engine.calls
        XCTAssertEqual(calls, [], "nothing kept")

        let granted = SettingsModel(api: engine, login: FakeLogin(.enabled), permissions: FakePermissions())
        await granted.load()
        XCTAssertFalse(granted.voice, "off by default")
        await granted.setVoice(true)
        await granted.setVoiceAutoListen(true)
        XCTAssertTrue(granted.voice)
        XCTAssertTrue(granted.voiceAutoListen)
        XCTAssertNil(granted.voiceProblem)
        let after = await engine.calls
        XCTAssertEqual(after, ["voice true", "auto-listen true"])
    }

    func testCardFollowsTheVoiceSettings() {
        let d = Directory()
        d.apply(#"data: {"seq": 3, "kind": "settings.changed", "at": "2026-11-18T10:00:00Z", "data": {"card_corner": "top-right", "voice": true, "voice_auto_listen": true, "palette": []}}"#)
        XCTAssertTrue(d.voice)
        XCTAssertTrue(d.voiceAutoListen)
    }

    func testSnapshotRefusedPermission() async throws {
        let model = SettingsModel(api: try FakeEngine(), login: FakeLogin(.enabled),
                                  permissions: FakePermissions(refused: "Pong needs the microphone. Open System Settings > Privacy & Security > Microphone and switch Pong on."))
        await model.load()
        await model.setVoice(true)
        try assertSnapshot(SettingsView(model: model), named: "settings-voice-refused")
    }

    // MARK: The card

    func testSnapshotListening() async throws {
        let (v, io, clock) = session()
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 3 && $0.allowSeveral == nil })
        v.show(ping)
        await clock.settle()
        io.hear(.level(0.55))
        try assertSnapshot(CardView(ping: ping, voice: v), named: "voice-listening")
    }

    func testSnapshotMicrophoneButton() async throws {
        let (v, _, _) = session(auto: false)
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 3 && $0.allowSeveral == nil })
        v.show(ping)
        try assertSnapshot(CardView(ping: ping, voice: v), named: "voice-microphone")
    }
}
