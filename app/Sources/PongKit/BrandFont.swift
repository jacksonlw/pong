import CoreText
import Foundation

/// Registers Inter, which ships in PongKit's resource bundle (copied there from the brand kit).
/// When it cannot be loaded, SwiftUI falls back to the system font for `Font.custom("Inter")`,
/// and this says so in the log. Returns whether Inter is available.
@discardableResult
public func registerBrandFont() -> Bool {
    struct Once { nonisolated(unsafe) static var result: Bool? }
    if let r = Once.result { return r }
    let name = "Pong_PongKit.bundle"
    let font = brandResource("Inter-opsz-wght", "ttf")
    var error: Unmanaged<CFError>?
    let ok = font.map { CTFontManagerRegisterFontsForURL($0 as CFURL, .process, &error) } ?? false
    if !ok {
        let why = font == nil ? "Inter-opsz-wght.ttf not found in \(name)" : String(describing: error?.takeRetainedValue())
        print("Inter could not be loaded (\(why)); using the system font")
    }
    Once.result = ok
    return ok
}

/// A file in PongKit's resource bundle. In Pong.app the bundle is in Contents/Resources; under
/// `swift run` and `swift test` it sits beside the executable or the test bundle.
func brandResource(_ name: String, _ ext: String) -> URL? {
    let places = [Bundle.main.resourceURL, Bundle.main.bundleURL, Bundle(for: FontAnchor.self).bundleURL.deletingLastPathComponent()]
    return places.lazy.compactMap { $0.flatMap { Bundle(url: $0.appendingPathComponent("Pong_PongKit.bundle")) } }
        .compactMap { $0.url(forResource: name, withExtension: ext) }.first
}

private final class FontAnchor {}
