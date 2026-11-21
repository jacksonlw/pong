import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Pictures of Pong's real views drawn from the test fixtures, for documentation: each view in
/// light and dark at 2x, on a transparent background, cut to the view plus a small clear margin.
/// Runs only with `PONG_DOCS_CAPTURES=<absolute folder>`, and writes `mac-<scenario>-<theme>.png`
/// there; otherwise it is skipped.
@MainActor
final class DocsCaptureTests: XCTestCase {
    func testCapture() async throws {
        guard let path = ProcessInfo.processInfo.environment["PONG_DOCS_CAPTURES"], !path.isEmpty else {
            throw XCTSkip("set PONG_DOCS_CAPTURES to a folder to write the captures")
        }
        let out = URL(fileURLWithPath: path, isDirectory: true)
        try FileManager.default.createDirectory(at: out, withIntermediateDirectories: true)
        func capture(_ name: String, _ view: some View) throws {
            for (theme, appearance) in [("light", NSAppearance.Name.aqua), ("dark", .darkAqua)] {
                try renderClear(view, appearance: appearance).write(to: out.appendingPathComponent("mac-\(name)-\(theme).png"))
            }
        }

        try capture("card-question", try questionCard())
        try capture("card-plan", try planCard())
        try capture("card-finished", try finishedCard())

        let (rows, figures) = try threads()
        try capture("bar", BarView(figures: figures, threads: rows).environment(\.typingAnimates, false))
        try capture("threads", BarView(figures: figures, threads: rows, open: .threads).environment(\.typingAnimates, false))
        try capture("thread", try thread())

        let start = StartPanelModel(api: try FakeStarter())
        await start.load()
        start.choose(project: try XCTUnwrap(start.projects.first { $0.name == "billing-api" }).id)
        start.text = "Add invoices to the export:\n• one row per invoice\n• totals in the last row"
        try capture("start", BarView(figures: BarFigures(), threads: [], open: .start, start: StartPanelView(model: start))
            .environment(\.typingAnimates, false))

        let settings = SettingsModel(api: try FakeEngine(), login: FakeLogin(.enabled))
        await settings.load()
        try capture("settings", SettingsView(model: settings))
    }

    // MARK: Scenarios

    static let tags = [Tag(text: "staging", icon: "server.rack", colour: "peach"),
                       Tag(text: "feat/emails", icon: "arrow.triangle.branch")]

    /// A question with its answers, under the Project line with its ring, and Tags.
    func questionCard() throws -> some View {
        let text = try String(contentsOf: CardSnapshotTests.here.appendingPathComponent("Fixtures/context.sse"), encoding: .utf8)
        let directory = Directory()
        for line in text.split(whereSeparator: \.isNewline) { directory.apply(String(line)) }
        var ping = try XCTUnwrap(EventLine.parseAll(text).compactMap(\.ping).last { $0.session == "s-migrate" })
        ping.tags = Self.tags
        return CardView(ping: ping, position: 0, count: 3)
            .environment(\.cardContext, CardContext(directory: directory, hover: ContextHover()))
    }

    /// A plan to approve, with the plan as a document Resource.
    func planCard() throws -> some View {
        var ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.session == "s-plan" })
        let recorded = try String(contentsOf: CardSnapshotTests.here.appendingPathComponent("Fixtures/resources.sse"), encoding: .utf8)
        let doc = try XCTUnwrap(EventLine.parseAll(recorded).compactMap(\.ping).flatMap { $0.resources ?? [] }.first { $0.kind == "document" })
        ping.resources = [doc]
        let store = ResourceStore()
        store.contents[doc.id] = try Data(contentsOf: CardSnapshotTests.here.appendingPathComponent("Fixtures/resources/\(doc.id)"))
        let waiting = try CardSnapshotTests.pings().filter(\.isWaiting)
        return CardView(ping: ping, position: waiting.firstIndex { $0.id == ping.id } ?? 0, count: waiting.count)
            .environment(\.resourceStore, store)
    }

    /// Finished work handed over, with next directions.
    func finishedCard() throws -> some View {
        var p = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 3 })
        p.question = "The checkout redesign is live on staging. What next?"
        p.answers = [.init(label: "Ship it to prod"), .init(label: "Polish the empty cart"), .init(label: "Nothing more")]
        p.finished = true
        p.tags = [Tag(text: "staging", icon: "server.rack", colour: "peach"), Tag(text: "feat/checkout", icon: "arrow.triangle.branch")]
        return CardView(ping: p, position: 0, count: 1)
    }

    /// Sessions in two Projects: some working, one asking, one failed, one done.
    func threads() throws -> ([ThreadRow], BarFigures) {
        let d = Directory()
        d.apply(##"data: {"seq": 1, "kind": "project.changed", "at": "2026-11-19T08:00:00Z", "data": {"id": "p-shop", "name": "shop", "colour": "#E5484D"}}"##)
        d.apply(##"data: {"seq": 2, "kind": "project.changed", "at": "2026-11-19T08:00:00Z", "data": {"id": "p-api", "name": "billing-api", "colour": "#c4ecd4"}}"##)
        let sessions: [(String, String, String, String, String, Double)] = [
            ("s-checkout", "Fix the checkout total", "p-shop", "working", "2026-11-19T14:58:00Z", 2.40),
            ("s-migrate", "Backfill customer emails", "p-shop", "working", "2026-11-19T14:55:00Z", 3.18),
            ("s-release", "Cut release 2.4 and roll it out", "p-api", "working", "2026-11-19T14:50:00Z", 1.02),
            ("s-split", "Split the billing service", "p-api", "failed", "2026-11-19T14:20:00Z", 0.85),
            ("s-export", "Nightly export", "p-api", "idle", "2026-11-19T13:00:00Z", 0.30),
        ]
        for (i, s) in sessions.enumerated() {
            d.apply(#"data: {"seq": \#(i + 3), "kind": "session.changed", "at": "\#(s.4)", "data": {"harness": "claude-code", "id": "\#(s.0)", "title": "\#(s.1)", "project": "\#(s.2)", "state": "\#(s.3)", "updated_at": "\#(s.4)", "context": {"cost_usd": \#(s.5)}}}"#)
        }
        var asking = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true })
        asking.session = "s-migrate"
        let now = BarFigures.date("2026-11-19T15:00:00Z")!
        var utc = Calendar(identifier: .gregorian)
        utc.timeZone = TimeZone(identifier: "UTC")!
        return (ThreadRow.recent(d, waiting: [asking]),
                // No waiting button: beside the other figures its label would not fit.
                BarFigures(directory: d, asking: [asking], waiting: 0, now: now, calendar: utc))
    }

    /// One Session's Thread as its panel shows it: the title strip, the Pings and Pongs, the reply row.
    func thread() throws -> some View {
        let t = try ThreadTests.fixture("s-new")
        return VStack(spacing: 0) {
            ThreadPanelTitle(thread: t, opensIn: "Claude", stop: nil, actions: .init(), moved: {}, putBack: {}, onClose: {})
            ThreadView(thread: t)
            ThreadReply(target: t.replyTarget)
                .brandShadow()
                .padding(.top, ThreadScroll.replyGap)
        }
        .frame(width: ThreadView.width)
        .background(Brand.page)
        .environment(\.resourceStore, ResourceStore())
    }
}

@MainActor
extension XCTestCase {
    /// Renders a view at 2x with nothing drawn around it: the view's own surfaces stay opaque, the
    /// rest is clear, and the picture is cut to what was drawn plus `margin` points of clear edge.
    func renderClear<V: View>(_ view: V, appearance: NSAppearance.Name, margin: Int = 12) throws -> Data {
        let png = render(view, appearance: appearance)
        let image = try XCTUnwrap(NSBitmapImageRep(data: png)?.cgImage)
        let px = try Self.pixels(png)
        var (minX, minY, maxX, maxY) = (px.width, px.height, -1, -1)
        for y in 0..<px.height {
            for x in 0..<px.width where px.bytes[(y * px.width + x) * 4 + 3] > 0 {
                minX = min(minX, x); maxX = max(maxX, x)
                minY = min(minY, y); maxY = max(maxY, y)
            }
        }
        guard maxX >= 0 else { return png }
        let crop = CGRect(x: minX, y: minY, width: maxX - minX + 1, height: maxY - minY + 1)
        let cut = try XCTUnwrap(image.cropping(to: crop))
        let m = margin * 2
        let w = cut.width + 2 * m, h = cut.height + 2 * m
        let ctx = try XCTUnwrap(CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
                                          space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        ctx.clear(CGRect(x: 0, y: 0, width: w, height: h))
        ctx.draw(cut, in: CGRect(x: m, y: m, width: cut.width, height: cut.height))
        let rep = NSBitmapImageRep(cgImage: try XCTUnwrap(ctx.makeImage()))
        rep.size = NSSize(width: w / 2, height: h / 2)
        return try XCTUnwrap(rep.representation(using: .png, properties: [:]))
    }
}
