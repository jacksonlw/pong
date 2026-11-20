import AppKit

/// Makes a chosen image file into the logo the Engine keeps: a PNG no more than `side` pixels on
/// its longer side. A tile shows a logo at 16 to 18 points, so this is sharp on every screen and
/// far under the Engine's 1 MB limit, whatever the size of the file it came from.
enum ProjectLogo {
    static let side = 256

    /// Nil when the data is not an image the Mac can read.
    static func prepared(from data: Data) -> Data? {
        guard let image = NSImage(data: data), image.size.width > 0, image.size.height > 0 else { return nil }
        let scale = CGFloat(side) / max(image.size.width, image.size.height)
        let width = max(1, Int((image.size.width * scale).rounded())), height = max(1, Int((image.size.height * scale).rounded()))
        guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height, bitsPerSample: 8,
                                         samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                                         bytesPerRow: 0, bitsPerPixel: 0),
              let context = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        context.imageInterpolation = .high
        image.draw(in: NSRect(x: 0, y: 0, width: width, height: height))
        NSGraphicsContext.restoreGraphicsState()
        return rep.representation(using: .png, properties: [:])
    }
}
