import AppKit
import XCTest
@testable import PongKit

/// An image zooms in steps from wherever it is, and ⌘+, ⌘− and ⌘0 act on the image under the
/// pointer, or the one full screen, before Pong's own zoom.
@MainActor
final class ImageZoomTests: XCTestCase {
    func testStepsGoFromWhereTheImageIs() {
        let z = ImageZoom()
        z.fit = 0.6
        XCTAssertNil(z.scale, "an image starts fitted")
        z.zoomIn()
        XCTAssertEqual(z.scale, 0.75, "zooming in from a fit goes to the next step up")
        z.zoomOut(); z.zoomOut()
        XCTAssertEqual(z.scale, 0.25)
        z.set(100)
        XCTAssertEqual(z.scale, 8, "never past the last step")
        z.zoomIn()
        XCTAssertEqual(z.scale, 8)
        z.toFit()
        XCTAssertNil(z.scale)
    }

    func testTheKeysGoToTheImageWhenThereIsOne() throws {
        func key(_ c: String, _ m: NSEvent.ModifierFlags = .command) throws -> NSEvent {
            try XCTUnwrap(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: m, timestamp: 0, windowNumber: 0,
                                           context: nil, characters: c, charactersIgnoringModifiers: c, isARepeat: false, keyCode: 0))
        }
        ImageZoomKeys.target = nil
        ImageZoomKeys.shownFullScreen = nil
        XCTAssertFalse(ImageZoomKeys.handle(try key("=")), "with no image, Pong zooms")

        let hovered = ImageZoom(), full = ImageZoom()
        ImageZoomKeys.target = hovered
        XCTAssertTrue(ImageZoomKeys.handle(try key("=")))
        XCTAssertEqual(hovered.scale, 1.5)
        XCTAssertFalse(ImageZoomKeys.handle(try key("=", [.command, .option])), "⌥⌘= is not a zoom key")
        XCTAssertFalse(ImageZoomKeys.handle(try key("x")))

        ImageZoomKeys.shownFullScreen = full
        XCTAssertTrue(ImageZoomKeys.handle(try key("-")))
        XCTAssertEqual(full.scale, 0.75, "the full-screen image takes the keys")
        XCTAssertEqual(hovered.scale, 1.5)
        XCTAssertTrue(ImageZoomKeys.handle(try key("0", .control)))
        XCTAssertNil(full.scale)
        ImageZoomKeys.target = nil
        ImageZoomKeys.shownFullScreen = nil
    }
}
