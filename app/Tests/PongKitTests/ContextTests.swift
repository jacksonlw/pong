import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The Project line, the ring and the context beside the card, fed by a stream recorded from a
/// real Engine (Fixtures/context.sse; its context-window figures are hand-added, see
/// Fixtures/README.md). References are in Snapshots/context-*.png; `PONG_RECORD_SNAPSHOTS=1` rewrites.
@MainActor
final class ContextTests: XCTestCase {
    static let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent()

    var directory: Directory!
    var pings: [String: Ping] = [:]

    override func setUp() async throws {
        let text = try String(contentsOf: Self.here.appendingPathComponent("Fixtures/context.sse"), encoding: .utf8)
        directory = Directory()
        for line in text.split(whereSeparator: \.isNewline) { directory.apply(String(line)) }
        for e in EventLine.parseAll(text) { if let p = e.ping { pings[p.session] = p } }
    }

    func card(_ session: String) throws -> (Ping, Directory.Card) {
        let ping = try XCTUnwrap(pings[session])
        return (ping, directory.card(for: ping))
    }

    func testCardSaysWhereThePingIsFrom() throws {
        let (_, c) = try card("s-migrate")
        XCTAssertEqual(c.project?.name, "shop")
        XCTAssertEqual(c.project?.colour, "#E5484D")
        XCTAssertEqual(c.session?.title, "Backfill customer emails")
        XCTAssertEqual(c.session?.context?.usedFraction, 0.42)
        XCTAssertNil(try card("s-flaky").1.session?.context, "no figures from this Harness")
    }

    func testAnswerWithNothingToExplainShowsNothing() throws {
        let (_, c) = try card("s-migrate")
        XCTAssertNotNil(ContextPanel(topic: .answer("Users and orders"), card: c))
        XCTAssertNil(ContextPanel(topic: .answer("Ask the data team"), card: c))
        XCTAssertNil(ContextPanel(topic: .question, card: try card("s-flaky").1), "nothing to explain")
        XCTAssertNil(ContextPanel(topic: .ring, card: try card("s-flaky").1))
    }

    func testRingRedFromEightyPercent() {
        XCTAssertFalse(ContextRing.isFull(0.79))
        XCTAssertTrue(ContextRing.isFull(0.8))
    }

    // The ring normal, red and absent.
    func testProjectLineRingNormal() throws { try assertLine("s-migrate", named: "context-line-ring-normal") }
    func testProjectLineRingRed() throws { try assertLine("s-release", named: "context-line-ring-red") }
    func testProjectLineRingAbsent() throws { try assertLine("s-flaky", named: "context-line-ring-absent") }

    func testContextBesideCard() throws { try assertPanel("s-migrate", .question, named: "context-question") }
    func testAnswerMeaning() throws { try assertPanel("s-release", .answer("Production"), named: "context-answer-hard") }
    func testRingFigures() throws { try assertPanel("s-migrate", .ring, named: "context-ring-figures") }
    func testRingFiguresPartlyKnown() throws { try assertPanel("s-release", .ring, named: "context-ring-figures-red") }

    /// Context written in Markdown is drawn as Markdown: emphasis, code, lists and links, and
    /// inline Markdown in what an answer leads to.
    func testContextMarkdown() throws {
        let answers = try JSONDecoder().decode([PingContext.Answer].self, from: Data("""
            [{"label": "Run it", "leadsTo": "Starts the backfill on **prod**", "hardToUndo": true},
             {"label": "Wait", "leadsTo": "Leaves `customers` as it is"}]
            """.utf8))
        let sections = [
            ContextSection(label: "Background", body: .text("The backfill writes **every** row in `customers`.\n\n- Users first\n- Then *orders*\n\nSee [the runbook](https://example.com).")),
            ContextSection(label: "Answers", body: .answers(answers)),
        ]
        try assertSnapshot(ContextSectionList(sections: sections)
            .padding(14).frame(width: ContextPanel.width, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
            .background(SurfaceFill()), named: "context-markdown")
    }

    func assertLine(_ session: String, named name: String) throws {
        let (ping, _) = try card(session)
        let ctx = CardContext(directory: directory, hover: ContextHover())
        try assertSnapshot(ProjectLine(ping: ping)
            .padding(.horizontal, 16).padding(.vertical, 10)
            .frame(width: cardWidth, alignment: .leading)
            .background(SurfaceFill())
            .environment(\.cardContext, ctx), named: name)
    }

    func assertPanel(_ session: String, _ topic: ContextHover.Topic, named name: String) throws {
        let panel = try XCTUnwrap(ContextPanel(topic: topic, card: try card(session).1))
        try assertSnapshot(panel.padding(8), named: name)
    }

    struct SurfaceFill: View {
        var body: some View { Brand.surface }
    }

}
