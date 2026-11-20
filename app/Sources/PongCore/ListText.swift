import Foundation

/// A text box's bullets, wherever the human writes to an agent: the start panel, the card's reply box and the phone's. A line typed as `- ` or
/// `* ` shows a bullet, and goes to the agent as Markdown. Both forms are the same length, so the cursor stays where it is.
public enum ListText {
    private static let typed = try! NSRegularExpression(pattern: #"^([ \t]*)[-*] "#, options: .anchorsMatchLines)
    private static let shown = try! NSRegularExpression(pattern: #"^([ \t]*)• "#, options: .anchorsMatchLines)
    private static let item = try! NSRegularExpression(pattern: #"^([ \t]*• )(.*)$"#)

    public static func bulleted(_ s: String) -> String { replace(typed, in: s, with: "$1• ") }
    public static func markdown(_ s: String) -> String { replace(shown, in: s, with: "$1- ") }

    private static func replace(_ re: NSRegularExpression, in s: String, with template: String) -> String {
        re.stringByReplacingMatches(in: s, range: NSRange(s.startIndex..., in: s), withTemplate: template)
    }

    /// A new line at `cursor`. In a list it starts the next item; on an empty item it ends the
    /// list instead. Returns the new text and where the cursor goes.
    public static func newLine(in s: String, at cursor: Range<String.Index>) -> (String, String.Index) {
        let lineStart = s[..<cursor.lowerBound].lastIndex(of: "\n").map { s.index(after: $0) } ?? s.startIndex
        let line = String(s[lineStart..<cursor.lowerBound])
        var out = s
        if let m = item.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)),
           let marker = Range(m.range(at: 1), in: line), let rest = Range(m.range(at: 2), in: line) {
            if line[rest].isEmpty, cursor.isEmpty {
                out.removeSubrange(lineStart..<cursor.lowerBound)
                return (out, out.index(out.startIndex, offsetBy: s.distance(from: s.startIndex, to: lineStart)))
            }
            return insert("\n" + line[marker], into: s, at: cursor)
        }
        return insert("\n", into: s, at: cursor)
    }

    private static func insert(_ new: String, into s: String, at cursor: Range<String.Index>) -> (String, String.Index) {
        let offset = s.distance(from: s.startIndex, to: cursor.lowerBound) + new.count
        var out = s
        out.replaceSubrange(cursor, with: new)
        return (out, out.index(out.startIndex, offsetBy: offset))
    }
}
