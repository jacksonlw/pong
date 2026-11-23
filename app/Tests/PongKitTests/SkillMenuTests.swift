import SwiftUI
import XCTest
@testable import PongCore
@testable import PongKit

/// The Skill menu over a reply box, and known Skills in a sent Pong.
@MainActor
final class SkillMenuTests: XCTestCase {
    func testSnapshotMenu() throws {
        let q = SkillCompletion.Query(range: NSRange(location: 4, length: 3), typed: "re")
        let menu = SkillMenu(state: SkillMenuState(query: q, items: ["review-agent", "code-review"], index: 1), trigger: "/") { _ in }
        try assertSnapshot(menu.padding(12).background(Brand.surface), named: "skill-menu")
    }

    func testSnapshotSentSkill() throws {
        let sent = SentText(text: "Run /grilling on the plan, then /nope", skills: SkillOffer(trigger: "/", names: ["grilling"]))
            .padding(.horizontal, 14).padding(.vertical, 10)
            .background(Brand.selected, in: Rectangle())
            .padding(12)
            .background(Brand.surface)
        try assertSnapshot(sent, named: "skill-sent")
    }

    func testANamePongDoesNotKnowIsNotOffered() async {
        let names = SkillNames(fetch: { _ in ["grilling", "tdd"] })
        let shop = SkillNames.Place(harness: "claude-code", project: "p1")
        XCTAssertEqual(names.offer(shop).names, [], "nothing until the Engine answers")
        await names.load(shop)
        XCTAssertEqual(names.offer(shop), SkillOffer(trigger: "/", names: ["grilling", "tdd"]))
        XCTAssertEqual(names.offer(SkillNames.Place(harness: "zed", project: "p1")), .none, "a Harness without a trigger")
    }
}
