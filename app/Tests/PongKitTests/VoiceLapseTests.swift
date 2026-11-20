import XCTest
@testable import PongCore
@testable import PongKit

/// A hosted voice model that refuses, as when its account may no longer use it.
struct LapsedModel: VoiceModel {
    func reply(system: String, user: String, schema: String) async throws -> String { throw HostedVoiceLapsed() }
}

@MainActor
final class VoiceLapseTests: XCTestCase {
    func testALapseIsSaidOnceAndVoiceFallsBackToPhrasesMatchedOnTheMac() async {
        let io = FakeVoiceIO(), clock = ManualClock()
        let v = VoiceSession(io: io, sleep: { await clock.sleep($0) })
        v.on = true
        v.autoListen = true
        v.model = { LapsedModel() }
        v.show(VoiceTests.ping())
        await clock.settle()
        io.hear(.words("let's do just the users table"))
        for _ in 0..<5 { await clock.settle() }
        for _ in 0..<5 { await clock.settle() }
        XCTAssertEqual(io.spoken.filter { $0 == HostedVoiceLapsed.said }.count, 1, "\(io.spoken)")
        XCTAssertTrue(v.listening, "voice is not turned off")
        XCTAssertNil(v.problem)
        io.hear(.words("just users please, thanks"))
        for _ in 0..<5 { await clock.settle() }
        XCTAssertEqual(io.spoken.filter { $0 == HostedVoiceLapsed.said }.count, 1, "said once")
        XCTAssertEqual(io.spoken.last, "I didn't understand. Say a number, like \"the first one\".")
        io.hear(.words("the second one"))
        await clock.settle()
        XCTAssertEqual(v.fill.choices, ["Users only"])
    }
}
