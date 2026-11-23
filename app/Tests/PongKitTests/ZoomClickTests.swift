import AppKit
import PongKit
import SwiftUI
import XCTest

/// At any zoom, a click lands on the control drawn under it: SwiftUI's own and AppKit's.
@MainActor
final class ZoomClickTests: XCTestCase {
    final class AppKitControl: NSView {
        var clicked: () -> Void = {}
        override func mouseDown(with event: NSEvent) { clicked() }
    }

    struct AppKitRow: NSViewRepresentable {
        let clicked: () -> Void
        func makeNSView(context: Context) -> AppKitControl {
            let v = AppKitControl()
            v.clicked = clicked
            return v
        }
        func updateNSView(_ v: AppKitControl, context: Context) {}
    }

    /// Four rows 50 points high, two SwiftUI buttons then two AppKit views, each clicked at its
    /// middle as drawn, at the zoom the window was opened at and at one it changed to.
    func testClicksLandOnTheControlDrawnUnderThem() throws {
        for (opened, zoom) in [(1.0, 1.0), (1.5, 1.5), (2.0, 2.0), (1.0, 1.75), (2.0, 0.75)] as [(CGFloat, CGFloat)] {
            var clicked: [Int] = []
            let rows = VStack(spacing: 0) {
                ForEach(0..<2) { i in
                    Button("\(i)") { clicked.append(i) }.buttonStyle(.plain)
                        .frame(width: 200, height: 50).contentShape(Rectangle())
                }
                ForEach(2..<4) { i in AppKitRow { clicked.append(i) }.frame(width: 200, height: 50) }
            }.frame(width: 200, height: 200, alignment: .top)
            let host = ZoomedHostingView(rows, scale: opened)
            host.scale = zoom
            let size = NSSize(width: 200 * zoom, height: 200 * zoom)
            let w = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless], backing: .buffered, defer: false)
            w.contentView = host
            w.orderFrontRegardless()
            defer { w.orderOut(nil) }
            host.layoutSubtreeIfNeeded()
            RunLoop.current.run(until: Date().addingTimeInterval(0.1))
            for i in 0..<4 {
                let at = NSPoint(x: 100 * zoom, y: size.height - (CGFloat(i) * 50 + 25) * zoom)
                for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                    w.sendEvent(try XCTUnwrap(NSEvent.mouseEvent(with: type, location: at, modifierFlags: [], timestamp: 0,
                                                                 windowNumber: w.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1)))
                    RunLoop.current.run(until: Date().addingTimeInterval(0.05))
                }
            }
            XCTAssertEqual(clicked, [0, 1, 2, 3], "opened at \(opened), zoomed to \(zoom)")
        }
    }
}
