import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Voice runs on a model the human brings.
@MainActor
final class VoiceModelTests: XCTestCase {
    static func fixture(_ name: String) throws -> Data {
        try Data(contentsOf: CardSnapshotTests.here.appendingPathComponent("Fixtures/voice-models/\(name).json"))
    }

    func testRequestShapes() throws {
        let a = try AnthropicVoiceModel(key: "sk-test").request(system: "S", user: "U", schema: VoiceUnderstanding.schema)
        XCTAssertEqual(a.url?.absoluteString, "https://api.anthropic.com/v1/messages")
        XCTAssertEqual(a.value(forHTTPHeaderField: "x-api-key"), "sk-test")
        XCTAssertNotNil(a.value(forHTTPHeaderField: "anthropic-version"))
        let ab = try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(a.httpBody)) as? [String: Any])
        XCTAssertEqual(ab["model"] as? String, "claude-haiku-5-5")
        XCTAssertEqual(ab["system"] as? String, "S")
        let format = try XCTUnwrap((ab["output_config"] as? [String: Any])?["format"] as? [String: Any])
        XCTAssertEqual(format["type"] as? String, "json_schema")

        let e = try EndpointVoiceModel(url: URL(string: "http://localhost:11434/v1")!, model: "llama3.2").request(system: "S", user: "U")
        XCTAssertEqual(e.url?.absoluteString, "http://localhost:11434/v1/chat/completions")
        XCTAssertNil(e.value(forHTTPHeaderField: "Authorization"))
        let eb = try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(e.httpBody)) as? [String: Any])
        XCTAssertEqual(eb["model"] as? String, "llama3.2")
        XCTAssertEqual((eb["response_format"] as? [String: String])?["type"], "json_object")
    }

    func testRepliesFromEachAPIBecomeIntents() throws {
        let ping = VoiceSample.ping
        let want = VoiceIntent.answer(choices: ["Users only"], text: "add a test for it")
        XCTAssertEqual(VoiceUnderstanding.parse(try AnthropicVoiceModel.text(Self.fixture("anthropic-answer"), status: 200), ping), want)
        XCTAssertEqual(VoiceUnderstanding.parse(try EndpointVoiceModel.text(Self.fixture("endpoint-answer"), status: 200), ping), want)
        XCTAssertEqual(VoiceUnderstanding.parse(try EndpointVoiceModel.text(Self.fixture("endpoint-loose"), status: 200), ping), .notUnderstood,
                       "a reply in another structure is not understood")
    }

    func testABadKeyOrAMissingModelIsAnError() throws {
        XCTAssertThrowsError(try AnthropicVoiceModel.text(Self.fixture("anthropic-401"), status: 401)) {
            XCTAssertEqual($0.localizedDescription, "Anthropic refused the API key")
        }
        XCTAssertThrowsError(try EndpointVoiceModel.text(Self.fixture("endpoint-404"), status: 404)) {
            XCTAssertTrue($0.localizedDescription.contains("not found"))
        }
    }

    func testAnUnreachableEndpointIsAnError() async {
        let m = EndpointVoiceModel(url: URL(string: "http://127.0.0.1:9/v1")!, model: "x") // the discard port: nothing listens
        do {
            _ = try await m.reply(system: "", user: "", schema: "{}")
            XCTFail("reached")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("could not be reached"), error.localizedDescription)
        }
    }

    func testNoModelUntilOneIsSetUp() {
        XCTAssertNil(VoiceModels.make(.none, endpoint: nil, endpointModel: nil, key: nil))
        XCTAssertNil(VoiceModels.make(.endpoint, endpoint: "http://localhost:11434/v1", endpointModel: "", key: nil))
        XCTAssertNil(VoiceModels.make(.anthropic, endpoint: nil, endpointModel: nil, key: nil))
        XCTAssertNotNil(VoiceModels.make(.anthropic, endpoint: nil, endpointModel: nil, key: "sk"))
    }

    // MARK: Settings

    func model(_ engine: FakeEngine, fake: VoiceModel) -> SettingsModel {
        SettingsModel(api: engine, login: FakeLogin(.enabled), permissions: FakePermissions(), makeModel: { choice, _, _, key in
            choice == .none || (choice == .anthropic && key == nil) ? nil : fake
        })
    }

    func testTheKeyGoesToTheEnginesKeychainNotTheSettings() async throws {
        let engine = try FakeEngine()
        let m = model(engine, fake: FakeModel())
        await m.load()
        await m.setVoiceModel(.anthropic)
        await m.setVoiceKey("sk-ant-test")
        XCTAssertTrue(m.voiceKeySet)
        let calls = await engine.calls
        XCTAssertEqual(calls, ["voice model anthropic", "key set"])
        let kept = await engine.current
        XCTAssertFalse(String(describing: kept).contains("sk-ant-test"))
        await m.setVoiceKey(nil)
        XCTAssertFalse(m.voiceKeySet)
    }

    func testTestShowsWhatTheModelUnderstood() async throws {
        let engine = try FakeEngine()
        let m = model(engine, fake: FakeModel(reply: #"{"intent":"answer","choices":["Users only"],"text":"add a test for it"}"#))
        await m.load()
        await m.testVoiceModel()
        XCTAssertEqual(m.voiceTest, "Choose a model to test.")
        await m.setVoiceModel(.endpoint)
        await m.setVoiceEndpoint("http://localhost:11434/v1", model: "llama3.2")
        await m.testVoiceModel()
        XCTAssertEqual(m.voiceTest, "Works. It understood: Users only, with “add a test for it”.")
    }

    func testTestShowsAnUnreachableModel() async throws {
        let engine = try FakeEngine()
        let m = model(engine, fake: FakeModel(failure: "localhost could not be reached (Could not connect to the server.)"))
        await m.load()
        await m.setVoiceModel(.endpoint)
        await m.testVoiceModel()
        XCTAssertEqual(m.voiceTest, "Failed: localhost could not be reached (Could not connect to the server.)")
    }

    func testCardFollowsTheModelSetting() {
        let d = Directory()
        d.apply(#"data: {"seq": 3, "kind": "settings.changed", "at": "2026-11-18T10:00:00Z", "data": {"card_corner": "top-right", "voice_model": "endpoint", "voice_endpoint": "http://localhost:1234/v1", "voice_endpoint_model": "qwen", "palette": []}}"#)
        XCTAssertEqual(d.voiceModel, .endpoint)
        XCTAssertEqual(d.voiceEndpointModel, "qwen")
    }

    func testSnapshotModelSettings() async throws {
        let engine = try FakeEngine()
        let m = model(engine, fake: FakeModel(failure: "Anthropic refused the API key"))
        await m.load()
        await m.setVoice(true)
        await m.setVoiceModel(.anthropic)
        await m.setVoiceKey("sk-ant-test")
        await m.testVoiceModel()
        try assertSnapshot(SettingsView(model: m), named: "settings-voice-model")
    }
}
