import Foundation

/// What a Thread's details say about its Session, shown on hover over a Thread row on the Mac and
/// on press and hold on the phone: the agent's latest summary, when it started and how long it
/// has run, and how full its context is.
public struct ThreadDetails: Equatable, Sendable {
    /// The agent's latest line or two on where the Session stands, from its last Ping or Note.
    public var summary: String?
    public var createdAt: Date?
    /// When the turn it is in, or was last in, started.
    public var turnStartedAt: Date?
    public var working: Bool
    public var context: ContextFigures?

    public init(summary: String?, createdAt: String?, turnStartedAt: String?, state: String?, context: ContextFigures?) {
        self.summary = summary.flatMap { $0.isEmpty ? nil : $0 }
        self.createdAt = createdAt.flatMap(Self.date)
        self.turnStartedAt = turnStartedAt.flatMap(Self.date)
        working = state == "working"
        self.context = context
    }

    /// The labelled parts to show at `now`; empty when there is nothing to say.
    public func sections(now: Date, calendar: Calendar = .current, locale: Locale = .current) -> [ContextSection] {
        var out: [ContextSection] = []
        if let summary { out.append(.init(label: "", body: .text(summary))) }
        var rows: [(String, String)] = []
        if let createdAt {
            rows.append(("Started", Self.started(createdAt, now: now, calendar: calendar, locale: locale)))
            rows.append(("Running for", Self.duration(from: createdAt, to: now, calendar: calendar, locale: locale)))
        }
        if working, let turnStartedAt {
            rows.append(("This turn", Self.duration(from: turnStartedAt, to: now, calendar: calendar, locale: locale)))
        }
        if let f = context {
            if let used = f.usedFraction { rows.append(("Memory", "\(Int((used * 100).rounded()))% full")) }
            let tokens = ContextFigures.tokens
            if let t = f.tokens { rows.append(("Tokens", f.window.map { "\(tokens(t)) of \(tokens($0))" } ?? tokens(t))) }
            if let c = f.costUsd { rows.append(("Cost", String(format: "$%.2f so far", c))) }
            if let m = f.model, !m.isEmpty { rows.append(("Model", m)) }
        }
        if !rows.isEmpty { out.append(.init(label: "", body: .figures(rows))) }
        return out
    }

    /// "Today, 2:04 PM", "Yesterday, 9:30 AM", or "18 Nov, 2:04 PM".
    static func started(_ d: Date, now: Date, calendar: Calendar, locale: Locale) -> String {
        var time = Date.FormatStyle(date: .omitted, time: .shortened, locale: locale)
        time.timeZone = calendar.timeZone
        let t = d.formatted(time)
        if calendar.isDate(d, inSameDayAs: now) { return "Today, \(t)" }
        if let y = calendar.date(byAdding: .day, value: -1, to: now), calendar.isDate(d, inSameDayAs: y) { return "Yesterday, \(t)" }
        var day = Date.FormatStyle(locale: locale).day().month(.abbreviated)
        day.timeZone = calendar.timeZone
        return "\(d.formatted(day)), \(t)"
    }

    /// Counted as a clock counts, to the second: "0:42", "6:03", "2:14:07", "2d 2:00:00".
    static func duration(from: Date, to: Date, calendar: Calendar, locale: Locale) -> String {
        let s = max(0, Int(to.timeIntervalSince(from)))
        let (d, h, m, sec) = (s / 86_400, s % 86_400 / 3600, s % 3600 / 60, s % 60)
        let clock = d > 0 || h > 0 ? String(format: "%d:%02d:%02d", h, m, sec) : String(format: "%d:%02d", m, sec)
        return d > 0 ? "\(d)d \(clock)" : clock
    }

    /// The details count time, so a view of them redraws every second.
    public var counts: Bool { createdAt != nil }

    /// The Engine writes times in RFC 3339, with or without fractions of a second.
    static func date(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }
}
