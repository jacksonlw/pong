import Foundation

/// What opening a link Resource can do outside Pong; each app gives the real one, tests a fake.
@MainActor
public protocol LinkSystem {
    /// Opens `url` in the app that handles it, which comes forward.
    func open(_ url: URL)
    /// The name of the app that opens `url` on this Device, or nil when none does.
    func appName(opening url: URL) -> String?
    /// Asks the human, returning whether they allowed it.
    func confirm(_ question: String, detail: String) async -> Bool
    func tell(_ text: String)
}

/// Opening a link Resource: a web link opens, and a link to an app opens once the human
/// has allowed links to that app. Tags are never clicked. Nothing here can send a Pong or change
/// the Ping.
@MainActor
public final class LinkOpener {
    public enum Outcome: Equatable, Sendable {
        case opened, nothing
    }

    /// What a click on a link does.
    public enum Decision: Equatable, Sendable {
        /// Open it now.
        case open
        /// Ask the human first, naming the app that opens it.
        case ask(app: String)
        /// Never open it, whatever the human allowed.
        case refuse
        /// No app on this Device opens it.
        case noApp
    }

    let system: LinkSystem
    /// The schemes the human allowed are kept on this Device, outside the Engine. The key keeps
    /// its old name, from when Tags opened links, so what the human allowed then still holds.
    let defaults: UserDefaults
    static let allowedKey = "tagLinkSchemesAllowed"

    public init(system: LinkSystem, defaults: UserDefaults = .standard) {
        self.system = system
        self.defaults = defaults
    }

    /// Web links open at once. The schemes the Engine never accepts (file:, javascript:, data:)
    /// are refused here again, as a second guard in case such a link reaches the card anyway;
    /// the Engine's other link checks are not repeated. Any other scheme opens once allowed, if
    /// an app still opens it.
    public static func decide(_ url: URL, allowed: Set<String>, app: String?) -> Decision {
        guard let scheme = url.scheme?.lowercased(), !["file", "javascript", "data"].contains(scheme) else { return .refuse }
        if scheme == "http" || scheme == "https" { return .open }
        guard let app else { return .noApp }
        return allowed.contains(scheme) ? .open : .ask(app: app)
    }

    /// What the human is told when nothing on this Device opens a link.
    public static func noApp(_ scheme: String) -> String { "No app on this Device opens \(scheme) links." }

    /// Opens a link Resource's link. `place` names where it was clicked when asking.
    @discardableResult
    public func open(_ link: String, from place: String) async -> Outcome {
        guard let url = URL(string: link) else { return .nothing }
        let scheme = url.scheme?.lowercased() ?? ""
        var allowed = Set(defaults.stringArray(forKey: Self.allowedKey) ?? [])
        switch Self.decide(url, allowed: allowed, app: system.appName(opening: url)) {
        case .open:
            break
        case .ask(let app):
            guard await system.confirm("Open links to \(app) from \(place)?",
                                       detail: "Once allowed, \(scheme) links in \(place) open \(app) without asking on this Device.")
            else { return .nothing }
            allowed.insert(scheme)
            defaults.set(allowed.sorted(), forKey: Self.allowedKey)
        case .noApp:
            system.tell(Self.noApp(scheme))
            return .nothing
        case .refuse:
            return .nothing
        }
        system.open(url)
        return .opened
    }
}

/// The row of Tags, as far as it is a rule and not drawing.
public enum TagRowRule {
    /// How many Tags show in a row `available` wide: all of them when they fit, else as many as
    /// fit, in order, beside the "+N" for the rest. `more[i]` is the width of "+\(i + 1)".
    public static func fit(_ widths: [CGFloat], more: [CGFloat], in available: CGFloat, spacing: CGFloat) -> Int {
        func span(_ ws: some Collection<CGFloat>) -> CGFloat {
            ws.reduce(0, +) + spacing * CGFloat(max(ws.count - 1, 0))
        }
        if span(widths) <= available { return widths.count }
        for shown in stride(from: widths.count - 1, through: 1, by: -1)
        where span(widths.prefix(shown)) + spacing + more[widths.count - shown - 1] <= available {
            return shown
        }
        return 0
    }
}
