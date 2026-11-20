import Foundation

// The Ping sound. The Engine says whether a Ping is its Session's first
// (`first`); the app decides here whether and which sound it makes.

/// When a Ping makes a sound, as the human chose in Settings and the Engine keeps.
public enum PingSoundMode: String, Codable, CaseIterable, Sendable {
    case off
    case first
    case firstAndFollowUp = "first-and-follow-up"

    public var title: String {
        switch self {
        case .off: "Off"
        case .first: "First Ping only"
        case .firstAndFollowUp: "First and follow-up"
        }
    }
}

/// The two sounds, from the brand kit's sound folder (each app bundles the files).
public enum PingSound: String, Sendable {
    case first = "ping-first"
    case followUp = "ping-follow-up"

    /// The sound a newly arrived Ping makes, or nil for none. Notes and Thoughts are always silent.
    public static func `for`(_ ping: Ping, mode: PingSoundMode) -> PingSound? {
        guard ping.isNote != true, !ping.isThought else { return nil }
        switch (mode, ping.first == true) {
        case (.off, _): return nil
        case (_, true): return .first
        case (.first, false): return nil
        case (.firstAndFollowUp, false): return .followUp
        }
    }

    /// The one sound for several Pings arriving together: the first-Ping sound wins.
    public static func one(of sounds: some Sequence<PingSound>) -> PingSound? {
        var out: PingSound?
        for s in sounds {
            if s == .first { return s }
            out = s
        }
        return out
    }

    /// The file name, for a system notification's sound.
    public var fileName: String { rawValue + ".wav" }
}

/// Tells the Pings that just arrived from those already waiting, with the sound each makes.
/// A Ping created before the app started is not new, so it makes none.
@MainActor
public final class PingArrivals {
    private let start: Date
    private var seen: Set<String> = []

    public init(start: Date = Date()) {
        self.start = start
    }

    /// The sound of each Ping in `waiting` not seen before, by Ping id.
    public func sounds(_ waiting: [Ping], mode: PingSoundMode) -> [String: PingSound] {
        var out: [String: PingSound] = [:]
        for p in waiting where !seen.contains(p.id) {
            seen.insert(p.id)
            guard let at = p.createdAt.flatMap(Self.date), at >= start else { continue }
            out[p.id] = PingSound.for(p, mode: mode)
        }
        return out
    }

    private static func date(_ s: String) -> Date? {
        (try? Date(s, strategy: .iso8601.year().month().day().time(includingFractionalSeconds: true)))
            ?? (try? Date(s, strategy: .iso8601))
    }
}
