import Foundation

/// Offering a Harness's Skills as the human types its trigger, and finding the known ones in a
/// text. Offsets are UTF-16 units, as text views count them. Pong sends what was typed as
/// typed; nothing here rewrites a Skill.
public enum SkillCompletion {
    /// The partial name being typed: its range, trigger included, and what follows the trigger.
    public struct Query: Equatable, Sendable {
        public var range: NSRange
        public var typed: String
    }

    /// How many names the menu shows at once; it scrolls to the rest.
    public static let shown = 8

    /// What calls a Skill in a Harness: `/` in Claude Code, `$` in Codex; nil in others.
    public static func trigger(for harness: String?) -> Character? {
        switch harness {
        case "claude-code": "/"
        case "codex": "$"
        default: nil
        }
    }

    /// The name being typed just before the caret, when it starts with the trigger at the start
    /// of the text or after a space or new line; nil otherwise.
    public static func query(in text: String, caret: Int, trigger: Character) -> Query? {
        let ns = text as NSString
        guard caret >= 1, caret <= ns.length else { return nil }
        var start = caret
        while start > 0 {
            let ch = ns.substring(with: NSRange(location: start - 1, length: 1))
            if ch.first?.isWhitespace == true { break }
            start -= 1
        }
        let word = ns.substring(with: NSRange(location: start, length: caret - start))
        guard word.first == trigger else { return nil }
        let typed = String(word.dropFirst())
        guard !typed.contains(trigger) else { return nil }
        return Query(range: NSRange(location: start, length: caret - start), typed: typed)
    }

    /// The names that match what is typed: those starting with it first, then those holding it,
    /// ignoring case, each group in order.
    public static func matches(_ typed: String, in names: [String]) -> [String] {
        let t = typed.lowercased()
        guard !t.isEmpty else { return names }
        let starts = names.filter { $0.lowercased().hasPrefix(t) }
        let holds = names.filter { !$0.lowercased().hasPrefix(t) && $0.lowercased().contains(t) }
        return starts + holds
    }

    /// The text with the query replaced by the picked name and a space, and where the caret goes.
    public static func pick(_ name: String, for q: Query, in text: String, trigger: Character) -> (text: String, caret: Int) {
        let ns = text as NSString
        let end = q.range.location + q.range.length
        let next = end < ns.length ? ns.substring(with: NSRange(location: end, length: 1)) : ""
        let insert = String(trigger) + name + (next.first?.isWhitespace == true ? "" : " ")
        let out = ns.replacingCharacters(in: q.range, with: insert)
        let caret = q.range.location + (insert as NSString).length + (next.first?.isWhitespace == true ? 1 : 0)
        return (out, min(caret, (out as NSString).length))
    }

    /// Where known Skills stand in a text: the trigger and a name Pong knows, as a whole word.
    public static func known(in text: String, trigger: Character, names: Set<String>) -> [NSRange] {
        guard !names.isEmpty else { return [] }
        let ns = text as NSString
        var out: [NSRange] = []
        var i = 0
        while i < ns.length {
            let atStart = i == 0 || ns.substring(with: NSRange(location: i - 1, length: 1)).first?.isWhitespace == true
            guard atStart, ns.substring(with: NSRange(location: i, length: 1)).first == trigger else { i += 1; continue }
            var end = i + 1
            while end < ns.length, ns.substring(with: NSRange(location: end, length: 1)).first?.isWhitespace != true { end += 1 }
            var word = ns.substring(with: NSRange(location: i + 1, length: end - i - 1))
            // A sentence's own punctuation after the name is not part of it.
            while let last = word.last, ".,;:!?)".contains(last), !names.contains(word) { word.removeLast() }
            if names.contains(word) { out.append(NSRange(location: i, length: (word as NSString).length + 1)) }
            i = end
        }
        return out
    }
}

/// The Skills a box offers and draws, and the trigger that calls them.
public struct SkillOffer: Equatable, Sendable {
    public var trigger: Character?
    public var names: [String]
    public static let none = SkillOffer(trigger: nil, names: [])
    public init(trigger: Character?, names: [String]) { self.trigger = trigger; self.names = names }

    /// What the harness offers with these names: none for a Harness without a trigger.
    public init(harness: String?, names: [String]) {
        self.init(trigger: SkillCompletion.trigger(for: harness), names: SkillCompletion.trigger(for: harness) == nil ? [] : names)
    }
}
