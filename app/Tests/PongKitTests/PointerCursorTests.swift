import XCTest
@testable import PongKit

@MainActor
final class PointerCursorTests: XCTestCase {
    final class Owner: NSView, PointerCursorOwner {
        var cursor: NSCursor?
        func pointerCursor(at point: NSPoint) -> NSCursor? { cursor }
    }

    /// Takes no hits, as the hover trackers behind the card's views do.
    final class Transparent: NSView {
        override func hitTest(_ point: NSPoint) -> NSView? { nil }
    }

    private func window() -> NSWindow {
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 200, height: 100), styleMask: .borderless, backing: .buffered, defer: true)
        w.contentView = NSView(frame: NSRect(x: 0, y: 0, width: 200, height: 100))
        return w
    }

    func testWhereTwoOwnersOverlapOnlyTheTopOneAnswers() {
        let w = window()
        let bar = Owner(frame: NSRect(x: 0, y: 60, width: 200, height: 40))
        bar.cursor = .openHand
        let side = Owner(frame: NSRect(x: 0, y: 0, width: 8, height: 100))
        side.cursor = .resizeLeftRight
        w.contentView!.addSubview(bar)
        w.contentView!.addSubview(side)

        XCTAssertTrue(w.pointerCursor(at: NSPoint(x: 4, y: 80)) === NSCursor.resizeLeftRight)
        XCTAssertTrue(w.pointerCursor(at: NSPoint(x: 100, y: 80)) === NSCursor.openHand)
    }

    func testTheArrowEverywhereNoOwnerAnswers() {
        let w = window()
        let quiet = Owner(frame: NSRect(x: 0, y: 0, width: 50, height: 50))
        w.contentView!.addSubview(quiet)

        XCTAssertTrue(w.pointerCursor(at: NSPoint(x: 10, y: 10)) === NSCursor.arrow)
        XCTAssertTrue(w.pointerCursor(at: NSPoint(x: 150, y: 80)) === NSCursor.arrow)
    }

    func testAViewInsideAnOwnerShowsTheOwnersCursor() {
        let w = window()
        let text = Owner(frame: NSRect(x: 0, y: 0, width: 100, height: 100))
        text.cursor = .iBeam
        text.addSubview(NSView(frame: NSRect(x: 10, y: 10, width: 20, height: 20)))
        w.contentView!.addSubview(text)

        XCTAssertTrue(w.pointerCursor(at: NSPoint(x: 15, y: 15)) === NSCursor.iBeam)
    }

    func testAViewThatTakesNoHitsLeavesTheCursorToWhatIsUnder() {
        let w = window()
        let bar = Owner(frame: NSRect(x: 0, y: 0, width: 200, height: 100))
        bar.cursor = .openHand
        w.contentView!.addSubview(bar)
        w.contentView!.addSubview(Transparent(frame: NSRect(x: 0, y: 0, width: 200, height: 100)))

        XCTAssertTrue(w.pointerCursor(at: NSPoint(x: 50, y: 50)) === NSCursor.openHand)
    }
}
