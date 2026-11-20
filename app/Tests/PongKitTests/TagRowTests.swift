import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The row of Tags under the Project line: how each Tag is drawn, and the row drawn on the
/// card and in the Thread from Pings recorded from a real Engine (Fixtures/tags.sse and
/// Fixtures/thread/s-many.json). References are in Snapshots/tags-*.png and thread-tags-*.png.
@MainActor
final class TagRowTests: XCTestCase {
    // MARK: How each Tag is drawn

    func testIconFromTheVocabularyElseThePlainTag() {
        XCTAssertEqual(TagView.icon("server.rack"), "server.rack")
        XCTAssertEqual(TagView.icon("no.such.icon"), "tag")
        XCTAssertEqual(TagView.icon(nil), "tag")
    }

    func testColourIsMatchedWithoutRegardToCaseAndUnknownIsNeutralWithoutAnOutline() {
        let rose = TagView.Look(text: Brand.tagText, fill: Brand.tagRoseTint, edge: Brand.tagRoseOutline)
        let neutral = TagView.Look(text: Brand.secondary, fill: Brand.row, edge: nil)
        XCTAssertEqual(TagView.look("rose"), rose)
        XCTAssertEqual(TagView.look("Rose"), rose)
        XCTAssertEqual(TagView.look("crimson"), neutral)
        XCTAssertEqual(TagView.look(nil), neutral)
    }

    func testVoiceDoesNotReadTags() throws {
        let ping = try ping("s-coloured")
        XCTAssertEqual(ping.tags?.first?.text, "prod")
        XCTAssertEqual(VoiceScript.reading(ping), "Run the migration on prod? 1: Yes, recommended. 2: No. 3: Nothing more.")
    }

    // MARK: Snapshots

    static let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent()

    func stream() throws -> String {
        try String(contentsOf: Self.here.appendingPathComponent("Fixtures/tags.sse"), encoding: .utf8)
    }

    func ping(_ session: String) throws -> Ping {
        try XCTUnwrap(EventLine.parseAll(stream()).compactMap(\.ping).last { $0.session == session }, "no Ping from \(session)")
    }

    func testFixtureHoldsEachCase() throws {
        XCTAssertNil(try ping("s-none").tags)
        XCTAssertEqual(try ping("s-neutral").tags, [Tag(text: "feat/login", icon: "arrow.triangle.branch")])
        XCTAssertEqual(try ping("s-unknown").tags?.first?.icon, "no.such.icon")
        XCTAssertEqual(try ping("s-many").tags?.count, 8)
    }

    func testCardWithoutTags() throws { try assertCard("s-none", named: "tags-none") }
    func testCardWithANeutralTag() throws { try assertCard("s-neutral", named: "tags-neutral") }
    func testCardWithAColouredTag() throws { try assertCard("s-coloured", named: "tags-coloured") }
    func testCardWithAnUnknownIcon() throws { try assertCard("s-unknown", named: "tags-unknown-icon") }
    func testCardWithMoreTagsThanFit() throws { try assertCard("s-many", named: "tags-overflow") }

    func testThreadShowsTags() throws {
        let t = try ThreadTests.fixture("s-many")
        try assertSnapshot(ThreadView(thread: t, notice: nil).environment(\.resourceStore, ResourceStore()), named: "thread-tags")
    }

    func assertCard(_ session: String, named name: String, file: StaticString = #filePath, line: UInt = #line) throws {
        let directory = Directory()
        for l in try stream().split(whereSeparator: \.isNewline) { directory.apply(String(l)) }
        let ctx = CardContext(directory: directory, hover: ContextHover())
        try assertSnapshot(CardView(ping: try ping(session), position: 0, count: 1).environment(\.cardContext, ctx),
                           named: name, file: file, line: line)
    }
}
