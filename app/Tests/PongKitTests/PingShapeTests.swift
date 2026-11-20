import AppKit
import PDFKit
import SwiftUI
import XCTest
@testable import PongKit

/// Pings that say their shape, on the Mac: the Finished mark, link, sheet and slides Resources, the folded question
/// and a Note's Resources. The Pings are the recorded ones from Fixtures/events.sse and
/// Fixtures/thread/, given the new fields; the Resource contents are in Fixtures/shape/ (see its
/// README). References are in Snapshots/shape-*.png.
@MainActor
final class PingShapeTests: XCTestCase {
    static let here = URL(fileURLWithPath: #filePath).deletingLastPathComponent()

    static func shape(_ name: String) throws -> Data {
        try Data(contentsOf: here.appendingPathComponent("Fixtures/shape/\(name)"))
    }

    /// A finished Ping: work handed over, with next directions and Nothing more.
    func finished(tags: [Tag]? = nil) throws -> Ping {
        var p = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 3 })
        p.question = "The checkout redesign is live on staging. What next?"
        p.answers = [.init(label: "Ship it to prod"), .init(label: "Polish the empty cart"), .init(label: "Nothing more")]
        p.finished = true
        p.tags = tags
        return p
    }

    static let tags = [Tag(text: "staging", icon: "server.rack", colour: "peach"),
                       Tag(text: "feat/checkout", icon: "arrow.triangle.branch")]

    // MARK: Finished

    func testFinishedDecodesAndIsFalseWhenAbsent() throws {
        let on = try JSON.decoder.decode(Ping.self, from: Data(#"{"id":"a","session":"s","question":"Done.","state":"waiting","finished":true}"#.utf8))
        let off = try JSON.decoder.decode(Ping.self, from: Data(#"{"id":"b","session":"s","question":"Q?","state":"waiting"}"#.utf8))
        XCTAssertTrue(on.isFinished)
        XCTAssertFalse(off.isFinished)
    }

    func testVoiceDoesNotReadTheFinishedMark() throws {
        let p = try finished(tags: Self.tags)
        XCTAssertEqual(VoiceScript.reading(p),
                       "The checkout redesign is live on staging. What next? 1: Ship it to prod, recommended. 2: Polish the empty cart. 3: Nothing more.")
    }

    func testTheRowShowsForTheMarkAloneAndNotForAnUnfinishedPingWithoutTags() throws {
        var p = try finished()
        XCTAssertTrue(TagLine.shows(p))
        p.finished = nil
        XCTAssertFalse(TagLine.shows(p))
    }

    func testCardFinishedAlone() throws {
        try assertSnapshot(CardView(ping: finished()), named: "shape-finished-alone")
    }

    func testCardFinishedWithTags() throws {
        try assertSnapshot(CardView(ping: finished(tags: Self.tags)), named: "shape-finished-tags")
    }

    func testThreadShowsTheFinishedMark() throws {
        var t = try ThreadTests.fixture("s-many")
        var first = try XCTUnwrap(t.pings.first)
        first.finished = true
        first.question = "The checkout redesign is live on staging. What next?"
        first.tags = Self.tags
        var alone = first
        alone.id = "finished-alone"
        alone.tags = nil
        alone.state = .answered
        alone.pong = Ping.Pong(answer: "Nothing more")
        t.pings = [alone, first]
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, ResourceStore()), named: "shape-thread-finished")
    }

    // MARK: Links, sheets and slides

    static let link = Resource(id: "r-link", title: "Checkout preview", kind: "link", contentType: "text/uri-list")
    static let sheet = Resource(id: "r-sheet", title: "Orders this week", kind: "sheet", contentType: "application/vnd.pong.sheet+json")
    static let slides = Resource(id: "r-slides", title: "Rollout deck", kind: "slides", contentType: "application/pdf")

    func store() throws -> ResourceStore {
        let s = ResourceStore()
        s.contents = [Self.link.id: try Self.shape("link.txt"), Self.sheet.id: try Self.shape("sheet.json"), Self.slides.id: Self.twoPages()]
        return s
    }

    func testALinkShowsItsHost() throws {
        XCTAssertEqual(Resource.linkAddress(try Self.shape("link.txt")), "https://preview.shop.example.com/checkout")
        XCTAssertEqual(Resource.linkHost(try Self.shape("link.txt")), "preview.shop.example.com")
        XCTAssertEqual(Resource.linkAddress(Data("# a comment\n\nvscode://file/x\n".utf8)), "vscode://file/x")
        XCTAssertEqual(symbol(forKind: "link"), "link")
        XCTAssertEqual(symbol(forKind: "sheet"), "tablecells")
    }

    /// A link opens as a Tag link does, asking once for an app's scheme; it never opens a preview.
    func testALinkOpensLikeATagLink() async throws {
        let system = FakeSystem()
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "PingShapeTests-\(UUID())"))
        let clicks = LinkOpener(system: system, defaults: defaults)
        let s = ResourceStore()
        s.contents[Self.link.id] = try Self.shape("link.txt")
        var ping = try finished()
        ping.resources = [Self.link]
        await s.openLink(Self.link, of: ping, clicks: clicks)
        XCTAssertEqual(system.opened, ["https://preview.shop.example.com/checkout"])
        XCTAssertTrue(s.opened.isEmpty, "a link has no preview")

        let app = Resource(id: "r-app", title: "Open in editor", kind: "link", contentType: "text/uri-list")
        s.contents[app.id] = Data("vscode://file/Users/me/shop".utf8)
        await s.openLink(app, of: ping, clicks: clicks)
        await s.openLink(app, of: ping, clicks: clicks)
        XCTAssertEqual(system.asked, ["Open links to Visual Studio Code from Resources?"], "asked once")
        XCTAssertEqual(system.opened.last, "vscode://file/Users/me/shop")
    }

    func testCardWithALink() throws {
        var p = try finished()
        p.resources = [Self.link, Self.sheet, Self.slides]
        try assertSnapshot(CardView(ping: p).environment(\.resourceStore, store()), named: "shape-resources")
    }

    func testThreadRowOfALink() throws {
        var t = try ThreadTests.fixture("s-many")
        var p = try XCTUnwrap(t.pings.first)
        p.tags = nil
        p.question = "The preview is deployed."
        p.isNote = true
        p.answers = nil
        p.resources = [Self.link, Self.sheet]
        t.pings = [p]
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, store()), named: "shape-thread-link")
    }

    func testSheetDecodes() throws {
        let sheet = try XCTUnwrap(SheetContent.decode(Self.shape("sheet.json")))
        XCTAssertEqual(sheet.sheets.map(\.name), ["Orders", "Refunds"])
        XCTAssertEqual(sheet.sheets[0].rows.first?.first, "Day")
        XCTAssertNil(SheetContent.decode(Data("a,b\n1,2".utf8)))
        let ragged = SheetContent.Sheet(name: "S", rows: [["a", "b", "c"], ["1"]])
        XCTAssertEqual(ragged.row(1), ["1", "", ""])
    }

    func testSheetPreviewWithTwoTabs() throws {
        try assertSnapshot(ResourcePreview(resource: Self.sheet, data: Self.shape("sheet.json"), onClose: {}), named: "shape-preview-sheet")
    }

    func testSlidesPreviewFromATwoPagePDF() throws {
        XCTAssertEqual(PDFDocument(data: Self.twoPages())?.pageCount, 2)
        try assertSnapshot(ResourcePreview(resource: Self.slides, data: Self.twoPages(), onClose: {}), named: "shape-preview-slides")
    }

    // MARK: Folding and Notes

    static let long = """
        The checkout redesign is on staging, and before it goes further I want your call on three things that \
        came up while testing it on real orders this morning: the empty cart now shows suggested products, \
        which pushes the order summary below the fold on small laptops; the address form asks for the \
        postcode first, which saves a step for most buyers but confused two of the five people I watched; \
        and the new payment step keeps the card form open after a failed charge instead of starting over, \
        which is what support asked for but means a second charge can be tried without the buyer seeing \
        the first one failed. Should I keep all three as they are?
        """

    func testALongQuestionFolds() throws {
        var p = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 2 })
        p.question = Self.long
        try assertSnapshot(CardView(ping: p), named: "shape-question-folded")
        try assertSnapshot(CardView(ping: p, unfolded: true), named: "shape-question-unfolded")
        let short = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 2 })
        XCTAssertLessThan(NSHostingView(rootView: CardView(ping: short)).fittingSize.height,
                          NSHostingView(rootView: CardView(ping: p)).fittingSize.height)
        XCTAssertLessThan(NSHostingView(rootView: CardView(ping: p)).fittingSize.height,
                          NSHostingView(rootView: CardView(ping: p, unfolded: true)).fittingSize.height)
    }

    func testANoteShowsItsResources() throws {
        var p = try XCTUnwrap(CardSnapshotTests.pings().first { $0.session == "s-done" })
        p.question = "The preview is deployed."
        p.resources = [Self.link]
        try assertSnapshot(CardView(ping: p).environment(\.resourceStore, store()), named: "shape-note-resource")
    }

    // MARK: Support

    final class FakeSystem: LinkSystem {
        var opened: [String] = []
        var asked: [String] = []
        func open(_ url: URL) { opened.append(url.absoluteString) }
        func appName(opening url: URL) -> String? { url.scheme == "vscode" ? "Visual Studio Code" : "Safari" }
        func confirm(_ question: String, detail: String) async -> Bool { asked.append(question); return true }
        func tell(_ text: String) {}
    }

    /// Two slides drawn in the light theme's colours, as a picture's are whatever the theme.
    static func twoPages() -> Data {
        let data = NSMutableData()
        var box = CGRect(x: 0, y: 0, width: 640, height: 360)
        let ctx = CGContext(consumer: CGDataConsumer(data: data)!, mediaBox: &box, nil)!
        NSAppearance(named: .aqua)!.performAsCurrentDrawingAppearance {
            for (title, bars) in [("Checkout redesign", 3), ("Rollout in two steps", 2)] {
                ctx.beginPDFPage(nil)
                ctx.setFillColor(NSColor(Brand.surface).cgColor)
                ctx.fill(box)
                ctx.setFillColor(NSColor(Brand.tagSkyTint).cgColor)
                ctx.fill(CGRect(x: 40, y: 250, width: 560, height: 70))
                ctx.setFillColor(NSColor(Brand.track).cgColor)
                for i in 0..<bars { ctx.fill(CGRect(x: 40, y: 190 - i * 44, width: 420 - i * 80, height: 20)) }
                NSGraphicsContext.saveGraphicsState()
                NSGraphicsContext.current = NSGraphicsContext(cgContext: ctx, flipped: false)
                NSAttributedString(string: title, attributes: [.font: Brand.Style.bodyNSFont, .foregroundColor: NSColor(Brand.fixedInk)])
                    .draw(at: CGPoint(x: 56, y: 276))
                NSGraphicsContext.restoreGraphicsState()
                ctx.endPDFPage()
            }
        }
        ctx.closePDF()
        return data as Data
    }
}
