import AppKit
import PongKit

/// What opening a link Resource does on this Mac: Launch Services opens the link, which brings
/// the opened app forward.
@MainActor
struct SystemLinkSystem: LinkSystem {
    func open(_ url: URL) {
        NSWorkspace.shared.open(url)
    }

    func appName(opening url: URL) -> String? {
        guard let app = NSWorkspace.shared.urlForApplication(toOpen: url) else { return nil }
        let bundle = Bundle(url: app)
        return bundle?.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String
            ?? bundle?.object(forInfoDictionaryKey: "CFBundleName") as? String
            ?? app.deletingPathExtension().lastPathComponent
    }

    /// An alert asks, so Pong comes forward for it; a link only asks once per scheme.
    func confirm(_ question: String, detail: String) -> Bool {
        let a = NSAlert()
        a.messageText = question
        a.informativeText = detail
        a.addButton(withTitle: "Allow")
        a.addButton(withTitle: "Don't Allow")
        NSApp.activate()
        return a.runModal() == .alertFirstButtonReturn
    }

    func tell(_ text: String) {
        let a = NSAlert()
        a.messageText = text
        NSApp.activate()
        a.runModal()
    }
}

/// The one LinkOpener the card and every Thread window share.
@MainActor let appLinkOpener = LinkOpener(system: SystemLinkSystem())
