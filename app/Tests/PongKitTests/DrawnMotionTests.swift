import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The card's motion as drawn, sampled frame by frame from a real window while the human
/// moves to the next Ping: the content's shift and the progress bar both pass through places
/// between where they start and where they end.
@MainActor
final class DrawnMotionTests: XCTestCase {
    func testTheShiftAndTheBarMoveInSteps() async throws {
        XCTAssertTrue(registerBrandFont())
        let inbox = Inbox(pings: (0..<3).map {
            Ping(id: "p\($0)", session: "s\($0)", question: "Question \($0) is here to be read?", state: "waiting")
        })
        let stage = CardStage()
        stage.set(inbox.scene)
        let size = NSSize(width: cardWidth, height: 300)
        let host = NSHostingView(rootView: InboxCard(inbox: inbox, stage: stage)
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
            .environment(\.typingAnimates, false)
            .frame(width: size.width, height: size.height, alignment: .top))
        host.appearance = NSAppearance(named: .aqua)
        let w = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = host
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(500))

        /// Where the question's ink starts, and where the bar's fill ends, in pixels at 2x.
        func sample() -> (text: Int?, bar: Int?) {
            let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size.width * 2), pixelsHigh: Int(size.height * 2),
                                       bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                       colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
            rep.size = size
            host.cacheDisplay(in: host.bounds, to: rep)
            let p = rep.bitmapData!, bpr = rep.bytesPerRow
            func dark(_ x: Int, _ y: Int) -> Int { 255 - Int(p[y * bpr + x * 4]) }
            var columns = [Int](repeating: 0, count: 400)
            for y in 120..<220 { for x in 0..<400 { columns[x] = max(columns[x], dark(x, y)) } }
            let most = columns.max() ?? 0
            let text = most > 8 ? columns.firstIndex { $0 > most / 2 } : nil
            let bar = (170..<620).first { dark($0, 42) < 150 } // the bar's row: where its fill gives way to the track
            return (text, bar)
        }

        let start = sample()
        inbox.next()
        stage.set(inbox.scene)
        var texts: [Int] = [], bars: [Int] = []
        for _ in 0..<60 {
            let s = sample()
            if let t = s.text { texts.append(t) }
            if let b = s.bar { bars.append(b) }
            try await Task.sleep(for: .milliseconds(5))
        }
        let end = sample()
        let from = try XCTUnwrap(start.text), barFrom = try XCTUnwrap(start.bar), barTo = try XCTUnwrap(end.bar)
        XCTAssertEqual(end.text, from, "the new content comes to rest where the old began")
        XCTAssertTrue(texts.contains { $0 < from && $0 > from - 6 }, "leaving, the content is drawn part of the way: \(texts)")
        XCTAssertTrue(texts.contains { $0 > from && $0 < from + 6 }, "arriving, the content is drawn part of the way: \(texts)")
        XCTAssertGreaterThan(barTo, barFrom)
        XCTAssertTrue(bars.contains { $0 > barFrom + 4 && $0 < barTo - 4 }, "the bar is drawn part of the way: \(bars)")
    }
}

/// The card's box as drawn while the human moves between a tall Ping and a short one: its
/// bottom edge passes through heights between the two, growing and shrinking alike.
@MainActor
final class DrawnBoxTests: XCTestCase {
    func testTheBoxEasesWhenItGrowsAndWhenItShrinks() async throws {
        XCTAssertTrue(registerBrandFont())
        let long = Array(repeating: "This question runs on for a good many lines.", count: 8).joined(separator: " ")
        let inbox = Inbox(pings: [
            Ping(id: "short", session: "s0", question: "Short?", state: "waiting"),
            Ping(id: "tall", session: "s1", question: long, state: "waiting"),
        ])
        let stage = CardStage()
        stage.set(inbox.scene)
        let size = NSSize(width: cardWidth, height: 500)
        let host = NSHostingView(rootView: InboxCard(inbox: inbox, stage: stage)
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
            .environment(\.typingAnimates, false)
            .fixedSize()
            .frame(width: size.width, height: size.height, alignment: .top))
        host.appearance = NSAppearance(named: .aqua)
        let w = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = host
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(500))

        /// The row of the box's bottom edge, in pixels at 2x: the last row drawn in the card's
        /// surface rather than the window's own colour.
        func bottom() -> Int {
            let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size.width * 2), pixelsHigh: Int(size.height * 2),
                                       bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                       colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
            rep.size = size
            host.cacheDisplay(in: host.bounds, to: rep)
            let p = rep.bitmapData!, bpr = rep.bytesPerRow, rows = rep.pixelsHigh
            func differs(_ y: Int) -> Bool { (0..<3).contains { abs(Int(p[y * bpr + 24 + $0]) - Int(p[(rows - 1) * bpr + 24 + $0])) > 4 } }
            return (0..<rows).last(where: differs) ?? 0
        }

        func heights(after move: () -> Void) async throws -> [Int] {
            move()
            stage.set(inbox.scene)
            var seen: [Int] = []
            for _ in 0..<80 {
                seen.append(bottom())
                try await Task.sleep(for: .milliseconds(5))
            }
            try await Task.sleep(for: .milliseconds(300))
            return seen
        }

        let short = bottom()
        let growing = try await heights { inbox.next() }
        let tall = bottom()
        XCTAssertGreaterThan(tall, short + 40)
        XCTAssertTrue(growing.contains { $0 > short + 4 && $0 < tall - 4 }, "growing, the box is drawn part of the way: \(growing)")
        let shrinking = try await heights { inbox.previous() }
        XCTAssertEqual(bottom(), short)
        XCTAssertTrue(shrinking.contains { $0 > short + 4 && $0 < tall - 4 }, "shrinking, the box is drawn part of the way: \(shrinking)")
    }
}
