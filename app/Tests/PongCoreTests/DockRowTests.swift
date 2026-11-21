import CoreGraphics
import PongCore
import XCTest

final class DockRowTests: XCTestCase {
    /// A new panel goes in nearest the card; one already open keeps its place.
    func testTheNewestIsNearestTheCard() {
        var row = DockRow()
        row.open("thread:a")
        row.open("settings")
        row.open("resource:1")
        XCTAssertEqual(row.docked, ["resource:1", "settings", "thread:a"])
        row.open("thread:a")
        XCTAssertEqual(row.docked, ["resource:1", "settings", "thread:a"], "reopening keeps its place")
    }

    /// Replacing takes out only the docked panels it picks, and says which, so they can close.
    func testReplacingTakesOutOnlyDockedPanelsOfThatKind() {
        var row = DockRow()
        row.open("thread:a")
        row.open("thread:b", apart: true)
        row.open("settings")
        let gone = row.open("thread:c", replacing: { $0.hasPrefix("thread:") })
        XCTAssertEqual(gone, ["thread:a"])
        XCTAssertEqual(row.docked, ["thread:c", "settings"])
        XCTAssertEqual(row.detached, ["thread:b"], "a detached Thread is never replaced")
    }

    /// Closing a panel names the one that takes focus, so ⌘W after ⌘W closes the rest: the one
    /// that slides into its place, else the one before it, else a detached one, else none.
    func testClosingNamesTheNextPanel() {
        var row = DockRow()
        row.open("c")
        row.open("b")
        row.open("a")
        row.open("x", apart: true)
        XCTAssertEqual(row.close("b"), "c", "the one further out slides into its place")
        XCTAssertEqual(row.close("c"), "a", "the last in the row hands focus back along it")
        XCTAssertEqual(row.close("x"), "a", "a detached one hands focus to the row")
        row.open("y", apart: true)
        XCTAssertEqual(row.close("a"), "y", "with the row empty, a detached one is next")
        XCTAssertNil(row.close("y"), "none left")
    }

    func testDetachDockAtTheEndAndClose() {
        var row = DockRow()
        row.open("a")
        row.open("b")
        row.detach("b")
        XCTAssertEqual(row.docked, ["a"])
        XCTAssertEqual(row.detached, ["b"])
        row.open("c")
        row.dockAtEnd("b")
        XCTAssertEqual(row.docked, ["c", "a", "b"])
        XCTAssertTrue(row.detached.isEmpty)
        row.close("a")
        XCTAssertEqual(row.docked, ["c", "b"])
        XCTAssertFalse(row.contains("a"))
    }

    /// Mixed widths run left of the card with the gap, tops level with the card's top.
    func testFramesRunLeftOfTheCard() {
        let screen = CGRect(x: 0, y: 0, width: 2000, height: 1200)
        let card = CGRect(x: 1560, y: 1000, width: 420, height: 60)
        let frames = DockRow.frames([CGSize(width: 520, height: 600), CGSize(width: 560, height: 640)],
                                    beside: card, on: screen, gap: 10)
        XCTAssertEqual(frames, [CGRect(x: 1030, y: 460, width: 520, height: 600),
                                CGRect(x: 460, y: 420, width: 560, height: 640)])
    }

    /// With no room on the left, the row runs right of the card; past the screen's edge, panels stay on it.
    func testFramesRunRightWhenTheLeftHasNoRoom() {
        let screen = CGRect(x: 0, y: 0, width: 1600, height: 1000)
        let card = CGRect(x: 12, y: 900, width: 420, height: 88)
        let frames = DockRow.frames([CGSize(width: 520, height: 560), CGSize(width: 520, height: 1200)],
                                    beside: card, on: screen, gap: 10)
        XCTAssertEqual(frames, [CGRect(x: 442, y: 428, width: 520, height: 560),
                                CGRect(x: 972, y: 0, width: 520, height: 1200)])
    }
}
