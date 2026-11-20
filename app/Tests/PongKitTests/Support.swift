import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// Helpers every test class shares: snapshot comparison against the reference PNGs in
/// Snapshots/ (run with `PONG_RECORD_SNAPSHOTS=1` to write new ones, then look at them), and
/// waiting for a condition.
@MainActor
extension XCTestCase {
    static var snapshots: URL {
        URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Snapshots")
    }

    /// Compares `view`, rendered in light and dark, with `<name>-light.png` and `<name>-dark.png`.
    func assertSnapshot<V: View>(_ view: V, named name: String, file: StaticString = #filePath, line: UInt = #line) throws {
        for (theme, appearance) in [("light", NSAppearance.Name.aqua), ("dark", .darkAqua)] {
            let png = render(view, appearance: appearance)
            let ref = Self.snapshots.appendingPathComponent("\(name)-\(theme).png")
            if ProcessInfo.processInfo.environment["PONG_RECORD_SNAPSHOTS"] != nil || !FileManager.default.fileExists(atPath: ref.path) {
                try png.write(to: ref)
                XCTFail("recorded \(ref.lastPathComponent); look at it, then run again", file: file, line: line)
                continue
            }
            let diff = try Self.difference(png, Data(contentsOf: ref))
            if diff > 0.002 {
                let failed = FileManager.default.temporaryDirectory.appendingPathComponent("\(name)-\(theme)-actual.png")
                try png.write(to: failed)
                XCTFail("\(name)-\(theme): \(String(format: "%.2f", diff * 100))% of pixels differ; actual in \(failed.path)", file: file, line: line)
            }
        }
    }

    /// Renders a view through NSHostingView at 2x into a PNG.
    func render<V: View>(_ view: V, appearance: NSAppearance.Name) -> Data {
        XCTAssertTrue(registerBrandFont(), "snapshots render in Inter")
        let host = NSHostingView(rootView: view)
        host.appearance = NSAppearance(named: appearance)
        let size = host.fittingSize
        host.frame = NSRect(origin: .zero, size: size)
        host.layoutSubtreeIfNeeded()
        let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size.width * 2), pixelsHigh: Int(size.height * 2),
                                   bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                   colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        rep.size = size
        host.cacheDisplay(in: host.bounds, to: rep)
        return rep.representation(using: .png, properties: [:])!
    }

    /// The share of pixels whose colour differs noticeably; 1 when the sizes differ.
    static func difference(_ a: Data, _ b: Data) throws -> Double {
        let pa = try pixels(a), pb = try pixels(b)
        guard pa.width == pb.width, pa.height == pb.height else { return 1 }
        var differing = 0
        for i in stride(from: 0, to: pa.bytes.count, by: 4) {
            for c in 0..<4 where abs(Int(pa.bytes[i + c]) - Int(pb.bytes[i + c])) > 24 {
                differing += 1
                break
            }
        }
        return Double(differing) / Double(pa.width * pa.height)
    }

    static func pixels(_ png: Data) throws -> (width: Int, height: Int, bytes: [UInt8]) {
        let rep = try XCTUnwrap(NSBitmapImageRep(data: png)?.cgImage)
        let w = rep.width, h = rep.height
        var bytes = [UInt8](repeating: 0, count: w * h * 4)
        let ctx = CGContext(data: &bytes, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                            space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        ctx.draw(rep, in: CGRect(x: 0, y: 0, width: w, height: h))
        return (w, h, bytes)
    }

    /// Waits up to two seconds for `done`, failing the test if it never holds.
    func until(_ done: @escaping () -> Bool) async throws {
        for _ in 0..<200 {
            if done() { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        let ok = done()
        XCTAssertTrue(ok, "timed out")
    }
}
