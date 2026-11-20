import AppKit
import SwiftUI
import XCTest
@testable import PongKit
import PongDocument

/// The card's Resource bar and the Resource previews, rendered from a Ping in Fixtures/resources.sse
/// (recorded from a real Engine with `pong ping -file … -text …`, one Resource of each previewed
/// kind) and the Resource contents read back from `GET /pings/{id}/resources/{id}` into
/// Fixtures/resources/. References are in Snapshots/resources-*.png; `PONG_RECORD_SNAPSHOTS=1`
/// rewrites them.
@MainActor
final class ResourcesTests: XCTestCase {
    static let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent()

    func recordedPing() throws -> Ping {
        let text = try String(contentsOf: Self.here.appendingPathComponent("Fixtures/resources.sse"), encoding: .utf8)
        return try XCTUnwrap(EventLine.parseAll(text).compactMap(\.ping).last { !($0.resources ?? []).isEmpty })
    }

    func store(for ping: Ping) throws -> ResourceStore {
        let s = ResourceStore()
        for r in ping.resources ?? [] {
            s.contents[r.id] = try Data(contentsOf: Self.here.appendingPathComponent("Fixtures/resources/\(r.id)"))
        }
        return s
    }

    func testRecordedPingListsResourcesInOrder() throws {
        let rs = try XCTUnwrap(recordedPing().resources)
        XCTAssertEqual(rs.map(\.kind), ["image", "document", "change", "file", "error", "output", "text"])
        XCTAssertEqual(rs.first?.title, "Checkout screen")
        XCTAssertEqual(rs.first?.contentType, "image/png")
        XCTAssertNil(rs.first { $0.kind == "error" }?.file, "inline text has no file")
    }

    /// The Resources sit on their own card below the question card: a bar of files, each an
    /// icon or thumbnail with its title beneath. Seven fill two rows.
    func testResourceBar() throws {
        let ping = try recordedPing()
        try assertSnapshot(CardView(ping: ping).environment(\.resourceStore, store(for: ping)), named: "resources-bar")
    }

    /// Past two rows the bar keeps its height and scrolls.
    func testResourceBarScrollsPastTwoRows() throws {
        var ping = try recordedPing()
        let rs = try XCTUnwrap(ping.resources)
        ping.resources = rs + rs.map { Resource(id: $0.id + "-again", title: $0.title, kind: $0.kind, file: $0.file, contentType: $0.contentType) }
        let two = try recordedPing()
        let tall = NSHostingView(rootView: CardView(ping: ping).environment(\.resourceStore, ResourceStore())).fittingSize.height
        let fits = NSHostingView(rootView: CardView(ping: two).environment(\.resourceStore, ResourceStore())).fittingSize.height
        XCTAssertEqual(tall, fits, accuracy: 0.5, "fourteen Resources take the same height as seven")
        try assertSnapshot(CardView(ping: ping).environment(\.resourceStore, store(for: two)), named: "resources-bar-scrolls")
    }

    func testOpeningAResourceAsksForItsPreview() throws {
        let ping = try recordedPing()
        let s = ResourceStore()
        var opened: [String] = []
        s.onOpen = { p, r in opened.append("\(p.id)/\(r.id)") }
        let doc = try XCTUnwrap(ping.resources?.first { $0.kind == "document" })
        s.open(doc, of: ping)
        XCTAssertEqual(opened, ["\(ping.id)/\(doc.id)"])
        XCTAssertEqual(s.opened, [doc])
    }

    /// A plain click opens one preview in place of the docked ones, Command adds or closes one,
    /// Shift adds the run from the last one opened, and detached previews stay through all of it.
    func testClicksOpenOneOrSeveralPreviews() throws {
        let ping = try recordedPing()
        let rs = try XCTUnwrap(ping.resources)
        let s = ResourceStore()
        s.open(rs[0], of: ping, .alone, among: rs)
        s.open(rs[1], of: ping, .alone, among: rs)
        XCTAssertEqual(s.opened, [rs[1]], "a plain click replaces the docked preview")
        s.open(rs[3], of: ping, .added, among: rs)
        XCTAssertEqual(s.opened, [rs[1], rs[3]])
        s.open(rs[1], of: ping, .added, among: rs)
        XCTAssertEqual(s.opened, [rs[3]], "a Command click on an open one closes it")
        s.open(rs[5], of: ping, .through, among: rs)
        XCTAssertEqual(s.opened, [rs[3], rs[4], rs[5]])
        s.detach(rs[4])
        s.open(rs[0], of: ping, .alone, among: rs)
        XCTAssertEqual(s.opened, [rs[4], rs[0]], "a detached preview stays when another is opened")
        s.open(rs[6], of: ping, .detached, among: rs)
        s.closeDocked()
        XCTAssertEqual(s.opened, [rs[4], rs[6]])
        s.dock(rs[4])
        XCTAssertEqual(s.docked, [rs[4]])
        s.close(rs[6])
        XCTAssertEqual(s.opened, [rs[4]])
        XCTAssertTrue(s.detached.isEmpty)
    }

    /// A dragged preview let go near another of Pong's windows comes to rest beside it, level
    /// with it; let go far from any, it stays where it is.
    func testADraggedPreviewSnapsBesideWhatIsNear() {
        let card = CGRect(x: 1000, y: 400, width: 380, height: 300)
        let size = CGSize(width: 520, height: 560)
        let near = CGRect(origin: CGPoint(x: 462, y: 150), size: size) // 18 left of the card, top 10 above its top
        XCTAssertEqual(PreviewSnap.rest(near, beside: [card], gap: 10), CGRect(origin: CGPoint(x: 470, y: 140), size: size))
        let far = CGRect(origin: CGPoint(x: 100, y: 900), size: size)
        XCTAssertEqual(PreviewSnap.rest(far, beside: [card], gap: 10), far)
    }

    func testDocumentIsRenderedNotRaw() throws {
        let ping = try recordedPing()
        let doc = try XCTUnwrap(ping.resources?.first { $0.kind == "document" })
        let src = String(decoding: try store(for: ping).contents[doc.id]!, as: UTF8.self)
        let blocks = MarkdownReader.blocks(src)
        guard case let .heading(1, title) = blocks.first else { return XCTFail("\(blocks)") }
        XCTAssertEqual(String(title.characters), "Migration plan")
        XCTAssertEqual(blocks.filter { if case .list = $0 { true } else { false } }.count, 2)
        XCTAssertEqual(String(inline("**two tables**").characters), "two tables")
    }

    /// Everything a document may hold, drawn: a table, a task list, nested and numbered lists,
    /// a quote, highlighted code, a diff, inline styles and a break. (Mermaid draws in a web
    /// view, which a snapshot cannot capture; PongDocumentTests checks it.)
    static let richDocument = """
    # Tickets for stopping a turn

    Tickets for [Stop a turn from any Device](09.md). Each is a slice that can be **checked on its own**; ~~nothing~~ is `uncommitted`.

    | # | Ticket | Blocked by |
    |---|---|---:|
    | [01](01.md) | Record how a hook halts a turn | None |
    | 03 | Claude Code ends the turn | 01, 02 |
    | 06 | The iPhone Thread has Stop, shown beside the reply box when a turn is running | 02, 04 |

    ## Notes

    1. **01 decides whether the rest goes ahead as written.**
       - If the hook cannot halt a turn, the spec goes back.
    2. 02 to 04 touch only the Engine.

    - [x] Glossary entry for Stop
    - [ ] Commit it

    > Codex has no ticket until its hooks have been recorded.

    ---

    ```swift
    // Halts the turn when the Session is told to stop.
    func stop(_ session: Session) async throws -> Bool {
        guard session.running else { return false }
        return try await hook.halt(session.id, reason: "stopped", after: 30)
    }
    ```

    ```diff
    - let x = 1
    + let x = 2
    ```
    """

    func testRichDocument() throws {
        // The whole document at the preview's width, not cut off at the preview's height.
        let page = MarkdownView(source: Self.richDocument).frame(width: previewSize.width - 32).padding(16).background(Brand.surface)
        try assertSnapshot(page, named: "resources-preview-rich")
    }

    func testEachKindPreviews() throws {
        let ping = try recordedPing()
        let s = try store(for: ping)
        for r in ping.resources ?? [] {
            try assertSnapshot(ResourcePreview(resource: r, data: s.contents[r.id], onClose: {}), named: "resources-preview-\(r.kind)")
        }
    }

    /// A link in a document reads as a link: underlined, in the link role.
    func testDocumentLink() throws {
        let doc = Resource(id: "r-link", title: "Rollout notes", kind: "document", file: nil, contentType: "text/markdown")
        let md = "# Rollout\n\nSee [the migration plan](https://example.com/plan) before you answer."
        try assertSnapshot(ResourcePreview(resource: doc, data: Data(md.utf8), onClose: {}), named: "resources-preview-link")
    }

    // MARK: snapshots (same method as CardSnapshotTests)

}
