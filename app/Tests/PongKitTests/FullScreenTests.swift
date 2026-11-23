import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// A view over the whole screen draws its ink there, but keeps its content and controls clear of
/// the menu bar and the Dock.
@MainActor
final class FullScreenTests: XCTestCase {
    /// The menu bar and the Dock take the top and bottom of the screen: the whole screen less what
    /// windows may use.
    func testFullScreenKeepsClearOfTheMenuBarAndTheDock() {
        let c = FullScreen.clear(frame: NSRect(x: 0, y: 0, width: 1512, height: 982),
                                 visible: NSRect(x: 0, y: 70, width: 1512, height: 888))
        XCTAssertEqual([c.top, c.leading, c.bottom, c.trailing], [24, 0, 70, 0])
        let side = FullScreen.clear(frame: NSRect(x: 0, y: 0, width: 1512, height: 982),
                                    visible: NSRect(x: 64, y: 0, width: 1448, height: 958))
        XCTAssertEqual([side.top, side.leading, side.bottom, side.trailing], [24, 64, 0, 0], "a Dock on the left")
    }

    /// Nothing but the ink is drawn under the menu bar or the Dock: not the close control at the
    /// top, nor the zoom controls at the bottom.
    func testTheImageAndItsControlsStayOutOfTheMenuBarAndTheDock() throws {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Snapshots/bar-threads-held-light.png")
        let img = try XCTUnwrap(NSImage(contentsOf: url))
        let size = CGSize(width: 900, height: 600)
        let clear = EdgeInsets(top: 24, leading: 0, bottom: 70, trailing: 0)
        let host = NSHostingView(rootView: ImageViewerView(image: img, title: "Large image", clear: clear) {}.frame(width: size.width, height: size.height))
        host.frame = NSRect(origin: .zero, size: size)
        let w = NSWindow(contentRect: host.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        w.contentView = host
        host.layoutSubtreeIfNeeded()
        RunLoop.current.run(until: Date().addingTimeInterval(0.2))
        let rep = try XCTUnwrap(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: rep)
        let scale = CGFloat(rep.pixelsHigh) / size.height
        func drawn(rows: Range<CGFloat>) -> Bool {
            let ink = rep.colorAt(x: 1, y: Int(rows.lowerBound * scale))
            for y in stride(from: rows.lowerBound * scale, to: rows.upperBound * scale, by: 1) {
                for x in stride(from: 0, to: CGFloat(rep.pixelsWide), by: 1) where rep.colorAt(x: Int(x), y: Int(y)) != ink {
                    return true
                }
            }
            return false
        }
        XCTAssertFalse(drawn(rows: 0..<clear.top), "nothing under the menu bar")
        XCTAssertFalse(drawn(rows: (size.height - clear.bottom)..<size.height), "nothing over the Dock")
        XCTAssertTrue(drawn(rows: clear.top..<(size.height - clear.bottom)), "the image and controls are drawn between them")
    }
}
