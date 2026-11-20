import AppKit
import SwiftUI
import WebKit
import XCTest
@testable import PongDocument

/// The bundled page draws a diagram and says how tall it is, and says so when it cannot.
/// `PONG_MERMAID_SHOT=<dir>` also saves what it drew, to look at.
@MainActor
final class MermaidTests: XCTestCase {
    static let diagram = "graph TD\n  subgraph Engine\n    B[The Engine keeps a stop]\n  end\n  A[Record how a hook halts a turn] --> C[Claude Code ends the turn]\n  B --> C"
    final class Box { var height: CGFloat = 0; var failed = false }

    func draw(_ source: String, dark: Bool = false) async throws -> (Box, WKWebView) {
        let box = Box()
        let c = MermaidCoordinator(height: Binding(get: { box.height }, set: { box.height = $0 }),
                                   failed: Binding(get: { box.failed }, set: { box.failed = $0 }))
        let web = c.makeWebView()
        web.frame = CGRect(x: 0, y: 0, width: 488, height: 600)
        web.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
        c.source = source
        let window = NSWindow(contentRect: web.frame, styleMask: .borderless, backing: .buffered, defer: false)
        window.contentView = web
        objc_setAssociatedObject(web, "c", c, .OBJC_ASSOCIATION_RETAIN)
        for _ in 0..<200 where box.height == 0 && !box.failed {
            c.show(in: web)
            try await Task.sleep(for: .milliseconds(50))
        }
        return (box, web)
    }

    func testDrawsADiagram() async throws {
        let (box, web) = try await draw(MermaidTests.diagram)
        XCTAssertFalse(box.failed)
        XCTAssertGreaterThan(box.height, 100)
        if let dir = ProcessInfo.processInfo.environment["PONG_MERMAID_SHOT"] {
            for dark in [false, true] {
                let (_, w) = dark ? try await draw(MermaidTests.diagram, dark: true) : (box, web)
                let image = try await w.takeSnapshot(configuration: nil)
                let rep = NSBitmapImageRep(data: image.tiffRepresentation!)!
                try rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: dir).appendingPathComponent("mermaid-\(dark ? "dark" : "light").png"))
            }
        }
    }

    func testABadDiagramFails() async throws {
        let (box, _) = try await draw("graph TD\n  A -->")
        XCTAssertTrue(box.failed)
    }
}
