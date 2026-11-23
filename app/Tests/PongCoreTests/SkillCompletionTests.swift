import XCTest
@testable import PongCore

final class SkillCompletionTests: XCTestCase {
    let names = ["code-review", "grilling", "pong:pong", "review-agent", "tdd"]

    func testTheTriggerIsTheHarnesss() {
        XCTAssertEqual(SkillCompletion.trigger(for: "claude-code"), "/")
        XCTAssertEqual(SkillCompletion.trigger(for: "codex"), "$")
        XCTAssertNil(SkillCompletion.trigger(for: "zed"))
        XCTAssertNil(SkillCompletion.trigger(for: nil))
    }

    func testAQueryStartsAtTheTriggerAtTheStartOfAWord() {
        XCTAssertEqual(SkillCompletion.query(in: "/gr", caret: 3, trigger: "/"),
                       .init(range: NSRange(location: 0, length: 3), typed: "gr"))
        XCTAssertEqual(SkillCompletion.query(in: "run /", caret: 5, trigger: "/")?.typed, "")
        XCTAssertEqual(SkillCompletion.query(in: "a\n$td", caret: 5, trigger: "$")?.range, NSRange(location: 2, length: 3))
        XCTAssertNil(SkillCompletion.query(in: "src/gr", caret: 6, trigger: "/"), "a path is not a Skill")
        XCTAssertNil(SkillCompletion.query(in: "/gr x", caret: 5, trigger: "/"), "the caret has left the word")
        XCTAssertNil(SkillCompletion.query(in: "/a/b", caret: 4, trigger: "/"))
        XCTAssertNil(SkillCompletion.query(in: "", caret: 0, trigger: "/"))
    }

    func testMatchesPutNamesStartingWithItFirst() {
        XCTAssertEqual(SkillCompletion.matches("rev", in: names), ["review-agent", "code-review"])
        XCTAssertEqual(SkillCompletion.matches("GRI", in: names), ["grilling"])
        XCTAssertEqual(SkillCompletion.matches("", in: names), names)
        XCTAssertEqual(SkillCompletion.matches("x", in: names), [])
    }

    func testPickingReplacesThePartialNameAndLeavesTheCaretAfterIt() {
        let q = SkillCompletion.query(in: "please /gr", caret: 10, trigger: "/")!
        let (text, caret) = SkillCompletion.pick("grilling", for: q, in: "please /gr", trigger: "/")
        XCTAssertEqual(text, "please /grilling ")
        XCTAssertEqual(caret, 17)
        let mid = SkillCompletion.query(in: "/gr the plan", caret: 3, trigger: "/")!
        let (t2, c2) = SkillCompletion.pick("grilling", for: mid, in: "/gr the plan", trigger: "/")
        XCTAssertEqual(t2, "/grilling the plan", "no second space")
        XCTAssertEqual(c2, 10)
    }

    func testKnownSkillsAreWholeWordsPongKnows() {
        let text = "Run /grilling, then /unknown and src/tdd and /tdd."
        let ns = text as NSString
        let found = SkillCompletion.known(in: text, trigger: "/", names: Set(names)).map { ns.substring(with: $0) }
        XCTAssertEqual(found, ["/grilling", "/tdd"])
        XCTAssertEqual(SkillCompletion.known(in: "/pong:pong", trigger: "/", names: Set(names)).count, 1)
        XCTAssertEqual(SkillCompletion.known(in: "$tdd", trigger: "/", names: Set(names)), [])
    }
}
