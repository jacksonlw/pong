import Foundation

/// What a context view explains: the question, one answer, or the context ring.
public enum ContextTopic: Equatable, Sendable {
    case question
    case answer(String)
    case ring
}

/// One part of a context view: a label over text, answers with what they lead to, or figures.
public struct ContextSection {
    public enum Body {
        case text(String)
        /// Answers with what they lead to.
        case answers([PingContext.Answer])
        /// Name and value pairs, e.g. Tokens · 420k of 1M.
        case figures([(String, String)])
    }
    public var label: String
    public var body: Body
    /// Shown in the hard-to-undo colour under the body.
    public var warning: String?

    public init(label: String, body: Body, warning: String? = nil) {
        self.label = label
        self.body = body
        self.warning = warning
    }
}

extension ContextFigures {
    /// From 80% full the ring turns red, as a prompt to start follow-ups in a new Session.
    public static func isFull(_ used: Double) -> Bool { used >= 0.8 }

    /// A token count as people read it: 420k, 1M, 1.5M.
    public static func tokens(_ n: Int) -> String {
        n >= 1_000_000 ? (n % 1_000_000 == 0 ? "\(n / 1_000_000)M" : String(format: "%.1fM", Double(n) / 1e6))
            : "\(Int((Double(n) / 1000).rounded()))k"
    }
}

extension Directory.Card {
    /// What to show for a topic; empty when there is nothing to explain.
    public func sections(_ topic: ContextTopic) -> [ContextSection] {
        switch topic {
        case .question:
            var s: [ContextSection] = []
            if let c = ping?.context, !c.isEmpty { s.append(.init(label: "Context", body: .text(c))) }
            if let w = ping?.why, !w.isEmpty { s.append(.init(label: "Why it is asking", body: .text(w))) }
            let explained = (ping?.answers ?? []).filter { !($0.leadsTo ?? "").isEmpty }
            if !explained.isEmpty { s.append(.init(label: "What each answer leads to", body: .answers(explained))) }
            return s
        case .answer(let label):
            guard let a = ping?.answers?.first(where: { $0.label == label }) else { return [] }
            let leads = a.leadsTo ?? ""
            if leads.isEmpty && a.hardToUndo != true { return [] }
            return [.init(label: a.label, body: .text(leads), warning: a.hardToUndo == true ? "Hard to undo" : nil)]
        case .ring:
            guard let f = session?.context, let used = f.usedFraction else { return [] }
            var rows: [(String, String)] = []
            let tokens = ContextFigures.tokens
            if let t = f.tokens { rows.append(("Tokens", f.window.map { "\(tokens(t)) of \(tokens($0))" } ?? tokens(t))) }
            if let c = f.costUsd { rows.append(("Cost", String(format: "$%.2f so far", c))) }
            if let m = f.model, !m.isEmpty { rows.append(("Model", m)) }
            let pct = "\(Int((used * 100).rounded()))% full"
            return [.init(label: "Memory", body: .text(pct),
                          warning: ContextFigures.isFull(used) ? "Start follow-ups in a new Session" : nil)]
                + (rows.isEmpty ? [] : [.init(label: "", body: .figures(rows))])
        }
    }
}
