import AppKit
@testable import PongKit
import SwiftUI
import XCTest

/// Pong's zoom control: − and + step to the next zoom, and the middle shows the zoom now.
@MainActor
final class ZoomControlTests: XCTestCase {
    private let steps: [CGFloat] = [0.75, 1, 1.25, 2]

    /// Clicks the control's zoom-out and zoom-in buttons at `scale`, and returns what each chose.
    private func click(at scale: CGFloat) throws -> [CGFloat] {
        var chosen: [CGFloat] = []
        let control = ZoomControl(scale: scale, steps: steps, choose: { chosen.append($0) })
        let host = ZoomedHostingView(control, scale: 1)
        let size = NSHostingView(rootView: control).fittingSize
        let w = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless], backing: .buffered, defer: false)
        w.contentView = host
        w.orderFrontRegardless()
        defer { w.orderOut(nil) }
        host.layoutSubtreeIfNeeded()
        RunLoop.current.run(until: Date().addingTimeInterval(0.1))
        for x in [15, size.width - 15] {
            for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                w.sendEvent(try XCTUnwrap(NSEvent.mouseEvent(with: type, location: NSPoint(x: x, y: size.height / 2), modifierFlags: [],
                                                             timestamp: 0, windowNumber: w.windowNumber, context: nil, eventNumber: 0,
                                                             clickCount: 1, pressure: 1)))
                RunLoop.current.run(until: Date().addingTimeInterval(0.05))
            }
        }
        return chosen
    }

    func testMinusAndPlusStepToTheNextZoom() throws {
        XCTAssertEqual(try click(at: 1.25), [1, 2])
        XCTAssertEqual(try click(at: 0.75), [1], "nothing smaller than the smallest")
        XCTAssertEqual(try click(at: 2), [1.25], "nothing larger than the largest")
    }

    func testShowsTheZoomNow() {
        XCTAssertEqual(ZoomControl.percent(1.25), "125%")
        XCTAssertEqual(ZoomControl.percent(0.85), "85%")
    }
}
