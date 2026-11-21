import AppKit
@testable import PongKit
import XCTest

final class PanelOpeningTests: XCTestCase {
    /// A plain click opens a panel in place of the docked one; Command, Control or Shift opens it beside.
    func testModifiersOpenBeside() {
        XCTAssertFalse(opensBeside([]))
        XCTAssertFalse(opensBeside([.option]))
        XCTAssertFalse(opensBeside([.capsLock]))
        XCTAssertTrue(opensBeside([.command]))
        XCTAssertTrue(opensBeside([.control]))
        XCTAssertTrue(opensBeside([.shift]))
        XCTAssertTrue(opensBeside([.command, .shift]))
    }
}
