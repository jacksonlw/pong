import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Stands in for the Engine, answering from responses recorded from a real one
/// (Fixtures/settings/) and keeping what each change asked for.
actor FakeEngine: SettingsAPI {
    static let dir = CardSnapshotTests.here.appendingPathComponent("Fixtures/settings")

    var current: EngineSettings
    var harnessList: [HarnessStatus]
    var calls: [String] = []
    var failing: String?
    /// While set, `harnesses()` and `repairHarness` wait for `release()`, as the slow Engine does.
    private var held: [CheckedContinuation<Void, Never>] = []
    var holding = false

    init() throws {
        func load<T: Decodable>(_ name: String) throws -> T {
            try JSON.decoder.decode(T.self, from: Data(contentsOf: Self.dir.appendingPathComponent(name)))
        }
        current = try load("settings.json")
        harnessList = try load("harnesses.json")
    }

    func hold() { holding = true }
    func release() { holding = false; held.forEach { $0.resume() }; held = [] }
    func fail(with message: String?) { failing = message }
    var waiting: Int { held.count }

    private func wait() async {
        if holding { await withCheckedContinuation { held.append($0) } }
    }

    private func check() throws {
        if let failing { throw EngineClient.Failure(description: failing) }
    }

    func settings() async throws -> EngineSettings { try check(); return current }
    func changeSettings(_ change: SettingsChange) async throws -> EngineSettings {
        try check()
        if let c = change.cardCorner { current.cardCorner = c; calls.append("corner \(c.rawValue)") }
        if let m = change.pingSound { current.pingSound = m; calls.append("ping sound \(m.rawValue)") }
        if let v = change.voice { current.voice = v; calls.append("voice \(v)") }
        if let v = change.voiceAutoListen { current.voiceAutoListen = v; calls.append("auto-listen \(v)") }
        if let m = change.voiceModel { current.voiceModel = m; calls.append("voice model \(m.rawValue)") }
        if let e = change.voiceEndpoint { current.voiceEndpoint = e; calls.append("endpoint \(e)") }
        if let e = change.voiceEndpointModel { current.voiceEndpointModel = e; calls.append("endpoint model \(e)") }
        if let f = change.cardFollowsActiveDisplay { current.cardFollowsActiveDisplay = f; calls.append("follows \(f)") }
        if let f = change.firstLaunchDone { current.firstLaunchDone = f; calls.append("first launch \(f)") }
        if let t = change.toldLoginDisallowed { current.toldLoginDisallowed = t; calls.append("told \(t)") }
        return current
    }
    var key: String?
    func voiceKey() async throws -> String? { try check(); return key }
    func setVoiceKey(_ k: String?) async throws {
        try check()
        key = k
        calls.append(k == nil ? "key cleared" : "key set")
    }
    func harnesses() async throws -> [HarnessStatus] {
        await wait()
        try check()
        return harnessList
    }
    func setSurface(_ id: String, _ surface: HarnessSurface) async throws {
        calls.append("surface \(id) \(surface.rawValue)")
        try check()
    }
    func repairHarness(_ id: String) async throws -> HarnessStatus {
        calls.append("repair \(id)")
        await wait()
        try check()
        let i = harnessList.firstIndex { $0.id == id }!
        harnessList[i].connected = true
        return harnessList[i]
    }
}

@MainActor
final class SettingsTests: XCTestCase {
    func until(_ what: String, _ cond: () async -> Bool) async {
        for _ in 0..<200 {
            if await cond() { return }
            try? await Task.sleep(for: .milliseconds(5))
        }
        XCTFail("timed out waiting for \(what)")
    }

    func testShowsSettingsBeforeTheSlowHarnessCheck() async throws {
        let engine = try FakeEngine()
        await engine.hold()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        let loading = Task { await model.load() }
        await until("the Harness check to start") { await engine.waiting == 1 }
        XCTAssertEqual(model.corner, .topRight)
        XCTAssertNil(model.harnesses, "still checking")
        XCTAssertTrue(model.startsAtLogin)
        await engine.release()
        await loading.value
        XCTAssertEqual(model.harnesses?.map(\.id), ["claude-code", "codex", "gemini", "amp", "factory"])
        let codex = try XCTUnwrap(model.harnesses?.first { $0.id == "codex" })
        XCTAssertTrue(codex.warning?.contains("run /hooks and trust them") == true, "the manual step and what to do")
    }

    func testEngineRefusalIsShown() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        await model.load()
        await engine.fail(with: "card_corner is not a corner")
        await model.setCorner(.bottomLeft)
        XCTAssertTrue(model.problem?.contains("not a corner") == true)
        await engine.fail(with: nil)
        await model.setCorner(.bottomLeft)
        XCTAssertNil(model.problem)
    }

    func testCornerIsKeptByTheEngine() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        await model.load()
        await model.setCorner(.bottomLeft)
        XCTAssertEqual(model.corner, .bottomLeft)
        let calls = await engine.calls
        XCTAssertEqual(calls, ["corner bottom-left"])
    }

    func testStartAtLoginShowsWhatMacOSReports() async throws {
        let login = FakeLogin(.notRegistered)
        let model = SettingsModel(api: try FakeEngine(), login: login)
        XCTAssertFalse(model.startsAtLogin)
        model.setStartsAtLogin(true)
        XCTAssertTrue(model.startsAtLogin)
        model.setStartsAtLogin(false)
        XCTAssertFalse(model.startsAtLogin)
        login.denies = true
        model.setStartsAtLogin(true)
        XCTAssertFalse(model.startsAtLogin, "macOS did not allow it")
        XCTAssertEqual(model.loginStatus, .requiresApproval)
        XCTAssertNotNil(model.problem)
    }

    func testRepairShowsProgressThenTheNewStatus() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        await model.load()
        XCTAssertEqual(model.harnesses?.first?.connected, false)
        await engine.hold()
        let repair = Task { await model.repair("claude-code") }
        await until("the repair to start") { await engine.waiting == 1 }
        XCTAssertEqual(model.repairing, ["claude-code"])
        await engine.release()
        await repair.value
        XCTAssertEqual(model.repairing, [])
        XCTAssertEqual(model.harnesses?.first?.connected, true)
    }

    func testLaunchRecordIsKeptByTheEngine() async throws {
        let engine = try FakeEngine()
        let record = try await EngineLaunchRecord.load(engine)
        XCTAssertFalse(record.firstLaunchDone)
        await setUpBackground(login: FakeLogin(.notRegistered), record: record, notifier: FakeNotifier()) { _ in }
        for t in record.saving { await t.value }
        let again = try await EngineLaunchRecord.load(engine)
        XCTAssertTrue(again.firstLaunchDone)
        XCTAssertFalse(again.toldLoginDisallowed)
    }

    func testCardFollowsTheCornerSetting() {
        let d = Directory()
        XCTAssertEqual(d.corner, .topRight)
        d.apply(#"data: {"seq": 3, "kind": "settings.changed", "at": "2026-11-18T10:00:00Z", "data": {"card_corner": "bottom-left", "first_launch_done": true, "told_login_disallowed": false, "palette": []}}"#)
        XCTAssertEqual(d.corner, .bottomLeft)
        XCTAssertFalse(d.followsActiveDisplay, "an event from before the display setting keeps the card on the main display")
        d.apply(#"data: {"seq": 4, "kind": "settings.changed", "at": "2026-11-19T10:00:00Z", "data": {"card_corner": "bottom-left", "card_follows_active_display": true, "first_launch_done": true, "told_login_disallowed": false, "palette": []}}"#)
        XCTAssertTrue(d.followsActiveDisplay)
    }

    func testFollowingTheActiveDisplayIsKeptByTheEngine() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        await model.load()
        XCTAssertFalse(model.followsActiveDisplay)
        await model.setFollowsActiveDisplay(true)
        XCTAssertTrue(model.followsActiveDisplay)
        let calls = await engine.calls
        XCTAssertEqual(calls, ["follows true"])
    }

    /// Where the human works in a Harness is kept by the Engine, and put back when it refuses.
    func testWhereYouWorkIsKeptByTheEngine() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.enabled))
        await model.load()
        XCTAssertEqual(model.harnesses?.first?.surface, "app")
        await model.setSurface("claude-code", .terminal)
        XCTAssertEqual(model.harnesses?.first?.surface, "terminal")
        let calls = await engine.calls
        XCTAssertEqual(calls, ["surface claude-code terminal"])
        await engine.fail(with: "the Engine said no")
        await model.setSurface("claude-code", .app)
        XCTAssertEqual(model.harnesses?.first?.surface, "terminal")
        XCTAssertNotNil(model.problem)
    }

    // Snapshots of the window's content from the recorded responses.

    func testSnapshotLoaded() async throws {
        let model = SettingsModel(api: try FakeEngine(), login: FakeLogin(.enabled))
        await model.load()
        try assertSettings(model, named: "settings")
    }

    func testSnapshotCheckingAndRepairing() async throws {
        let engine = try FakeEngine()
        let model = SettingsModel(api: engine, login: FakeLogin(.requiresApproval))
        await engine.hold()
        let loading = Task { await model.load() }
        await until("the Harness check to start") { await engine.waiting == 1 }
        try assertSettings(model, named: "settings-checking")
        await engine.release()
        await loading.value
        await engine.hold()
        let repair = Task { await model.repair("codex") }
        await until("the repair to start") { await engine.waiting == 1 }
        try assertSettings(model, named: "settings-repairing")
        await engine.release()
        await repair.value
    }

    func testSnapshotInfoPopover() throws {
        try assertSnapshot(InfoPopover(text: "Speech is recognised on this Mac, and no audio leaves it. Without Listen when a Ping appears, start from the microphone on the card.",
                                       learnMore: Docs.page("settings", "voice")),
                           named: "settings-info")
    }

    func testDocsLinkGoesToTheSection() {
        XCTAssertEqual(Docs.page("settings", "ping-sound").absoluteString, "https://pong.fly/docs/settings#ping-sound")
    }

    func assertSettings(_ model: SettingsModel, named name: String, file: StaticString = #filePath, line: UInt = #line) throws {
        try assertSnapshot(SettingsView(model: model), named: name, file: file, line: line)
    }
}
