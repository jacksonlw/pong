import AppKit
import XCTest
@testable import PongKit

/// An image shown full screen stays up until put away.
@MainActor
final class ImageViewerTests: XCTestCase {
    func testAnImageShowsFullScreen() async throws {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Snapshots/bar-threads-held-light.png")
        let img = try XCTUnwrap(NSImage(contentsOf: url))
        ImageViewer.show(img, title: "Large image")
        try await Task.sleep(for: .milliseconds(1500))
        let zoom = try XCTUnwrap(ImageZoomKeys.shownFullScreen, "the full-screen image takes the zoom keys")
        // Pinched all the way in, the image is far larger than the screen and is still drawn.
        for _ in 0..<12 { zoom.pinch(0.5); try await Task.sleep(for: .milliseconds(50)) }
        XCTAssertEqual(zoom.scale, ImageZoom.range.upperBound)
        try await Task.sleep(for: .milliseconds(1000))
        NSApp.windows.compactMap { $0 as? ImageViewer.Viewer }.forEach { $0.dismiss() }
        try await Task.sleep(for: .milliseconds(300))
    }
}
