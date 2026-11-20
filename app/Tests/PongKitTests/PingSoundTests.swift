import XCTest
@testable import PongKit

@MainActor
final class PingSoundTests: XCTestCase {
    func arrived(_ id: String, first: Bool, note: Bool = false, at: String = "2026-11-19T09:00:01.5Z") -> Ping {
        var p = ping(id)
        p.first = first
        p.isNote = note
        p.createdAt = at
        return p
    }

    func testWhichSoundAPingMakes() {
        let cases: [(PingSoundMode, Bool, Bool, PingSound?)] = [
            // mode, first, note, sound
            (.first, true, false, .first),
            (.first, false, false, nil),
            (.firstAndFollowUp, true, false, .first),
            (.firstAndFollowUp, false, false, .followUp),
            (.off, true, false, nil),
            (.off, false, false, nil),
            (.first, true, true, nil),
            (.firstAndFollowUp, false, true, nil),
        ]
        for (mode, first, note, want) in cases {
            XCTAssertEqual(PingSound.for(arrived("a", first: first, note: note), mode: mode), want, "\(mode) first \(first) note \(note)")
        }
    }

    func testOnlyNewPingsSoundAndTheFirstSoundWins() {
        let start = try! Date("2026-11-19T09:00:00Z", strategy: .iso8601)
        let a = PingArrivals(start: start)
        let old = arrived("old", first: true, at: "2026-11-19T08:59:00Z")
        XCTAssertEqual(a.sounds([old], mode: .first), [:], "waiting before the app started")
        let x = arrived("x", first: false), y = arrived("y", first: true)
        let got = a.sounds([old, x, y], mode: .firstAndFollowUp)
        XCTAssertEqual(got, ["x": .followUp, "y": .first])
        XCTAssertEqual(PingSound.one(of: got.values), .first)
        XCTAssertEqual(a.sounds([old, x, y], mode: .firstAndFollowUp), [:], "each Ping sounds once")
    }

    func testANotifiedPingCarriesItsSound() {
        let n = FakeNotifier(), lock = LockNotifications(notifier: n)
        let posted = lock.update(waiting: [ping("a"), ping("b")], screenLocked: true, sounds: ["b": .first])
        XCTAssertEqual(posted, ["a", "b"])
        XCTAssertEqual(n.events, ["post a", "post b ping-first"])
        XCTAssertEqual(lock.update(waiting: [ping("a"), ping("b")], screenLocked: false, sounds: [:]), [])
    }

    func testBothSoundsAreBundledAndUnderASecond() throws {
        for s in [PingSound.first, .followUp] {
            let url = try XCTUnwrap(s.url, s.rawValue)
            let bytes = try Data(contentsOf: url).count - 44
            XCTAssertLessThan(Double(bytes) / (44100 * 2), 1, s.rawValue)
        }
    }

    func testSettingsKeepThePingSoundInTheEngine() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        await model.load()
        XCTAssertEqual(model.pingSound, .first)
        await model.setPingSound(.firstAndFollowUp)
        XCTAssertEqual(model.pingSound, .firstAndFollowUp)
        let d = Directory()
        d.apply(#"data: {"seq": 3, "kind": "settings.changed", "at": "2026-11-19T10:00:00Z", "data": {"card_corner": "top-right", "ping_sound": "off", "palette": []}}"#)
        XCTAssertEqual(d.pingSound, .off)
    }

    /// A Thought never sounds nor is posted on the lock screen, even marked as waiting and first.
    func testAThoughtIsSilentAndNeverNotified() {
        var t = arrived("t", first: true)
        t.thought = true
        XCTAssertNil(PingSound.for(t, mode: .firstAndFollowUp))
        let n = FakeNotifier()
        let lock = LockNotifications(notifier: n)
        lock.update(waiting: [t, ping("p")], screenLocked: true)
        XCTAssertEqual(n.events, ["post p"])
    }
}
