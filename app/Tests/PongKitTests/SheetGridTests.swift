import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// A sheet is a light spreadsheet: cells chosen by click, Shift-click and drag copy as a
/// spreadsheet pastes them, and the header row stays in view while the rest scrolls.
@MainActor
final class SheetGridTests: XCTestCase {
    let sheet = SheetContent.Sheet(name: "S", rows: [
        ["Day", "Orders", "Note"],
        ["Mon", "1,204", "plain"],
        ["Tue", "1,318", "has\ttab"],
        ["Wed", "1,276", "said \"hi\""],
    ] + (4..<40).map { ["Day \($0)", "\($0)", ""] })

    func testAPointMapsToItsCell() {
        let widths: [CGFloat] = [50, 80, 60]
        let h = SheetGrid.rowHeight
        XCTAssertEqual(SheetGrid.cell(at: CGPoint(x: 10, y: 5), widths: widths, rows: 4), SheetCell(row: 0, column: 0))
        XCTAssertEqual(SheetGrid.cell(at: CGPoint(x: 129, y: h * 2.5), widths: widths, rows: 4), SheetCell(row: 2, column: 1))
        XCTAssertEqual(SheetGrid.cell(at: CGPoint(x: 999, y: 999), widths: widths, rows: 4), SheetCell(row: 3, column: 2),
                       "past the grid is its last cell")
        XCTAssertEqual(SheetGrid.cell(at: CGPoint(x: -5, y: -5), widths: widths, rows: 4), SheetCell(row: 0, column: 0))
    }

    func testCopiedCellsPasteIntoASpreadsheet() {
        let s = SheetSelection(anchor: SheetCell(row: 3, column: 2), end: SheetCell(row: 1, column: 1))
        XCTAssertEqual(SheetGrid.text(sheet, s), "1,204\tplain\n1,318\t\"has\ttab\"\n1,276\t\"said \"\"hi\"\"\"")
    }

    func testClickShiftClickAndCopyInAWindow() async throws {
        XCTAssertTrue(registerBrandFont())
        let host = NSHostingView(rootView: SheetTable(sheet: sheet).frame(width: 400, height: 300))
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 400, height: 300), styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = host
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(150))
        let widths = SheetGrid.widths(sheet)
        /// The views taking the grid's clicks: the header's, pinned at the top, and the body's.
        func areas() -> [NSView] {
            var found: [NSView] = []
            func walk(_ v: NSView) {
                if String(describing: type(of: v)).hasSuffix("Area") { found.append(v) }
                v.subviews.forEach(walk)
            }
            walk(host)
            return found.sorted { $0.frame.height < $1.frame.height }
        }
        /// A click in the middle of a cell, as the window takes it.
        func click(_ row: Int, _ column: Int, shift: Bool = false) async throws {
            let both = areas()
            XCTAssertEqual(both.count, 2)
            let area = try XCTUnwrap(row == 0 ? both.first : both.last)
            let x = widths.prefix(column).reduce(0, +) + widths[column] / 2
            let y = (CGFloat(row == 0 ? 0 : row) + 0.5) * SheetGrid.rowHeight
            let at = area.convert(NSPoint(x: x, y: y), to: nil)
            XCTAssertTrue(w.contentView?.hitTest(at) === area, "the click lands on the grid")
            for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                let e = try XCTUnwrap(NSEvent.mouseEvent(with: type, location: at, modifierFlags: shift ? .shift : [], timestamp: 0,
                                                         windowNumber: w.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
                w.sendEvent(e)
            }
            try await Task.sleep(for: .milliseconds(80))
        }
        func copied() async throws -> String? {
            NSPasteboard.general.clearContents()
            XCTAssertTrue(w.firstResponder?.tryToPerform(#selector(NSText.copy(_:)), with: nil) == true, "⌘C reaches the sheet")
            try await Task.sleep(for: .milliseconds(80))
            return NSPasteboard.general.string(forType: .string)
        }

        try await click(1, 0)
        let one = try await copied()
        XCTAssertEqual(one, "Mon")
        try await click(2, 1, shift: true)
        let block = try await copied()
        XCTAssertEqual(block, "Mon\t1,204\nTue\t1,318")
        try await click(0, 2)
        let header = try await copied()
        XCTAssertEqual(header, "Note", "a header cell is chosen too")

        // Scrolled well down, the header row is still drawn at the top.
        let scroll = try XCTUnwrap(ReplyGrowthTests.find(NSScrollView.self, in: host))
        scroll.contentView.scroll(to: NSPoint(x: 0, y: SheetGrid.rowHeight * 20))
        scroll.reflectScrolledClipView(scroll.contentView)
        try await Task.sleep(for: .milliseconds(150))
        try await click(0, 0)
        let pinned = try await copied()
        XCTAssertEqual(pinned, "Day", "a click on the pinned header chooses the header, not the row under it")
    }
}
