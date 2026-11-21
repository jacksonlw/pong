import Foundation

/// Web addresses written bare in text, such as `https://example.com/a`, made into links, as
/// GitHub draws them. Text that is already a link or is code is left as it is.
public enum Autolink {
    private static let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue)

    public static func apply(_ s: AttributedString) -> AttributedString {
        var out = s
        for run in s.runs where run.link == nil && run.inlinePresentationIntent?.contains(.code) != true {
            let text = String(s[run.range].characters)
            guard let detector else { return out }
            for match in detector.matches(in: text, range: NSRange(text.startIndex..., in: text)) {
                // Only addresses written with their scheme: "example.com" in a sentence stays words.
                guard let url = match.url, let scheme = url.scheme?.lowercased(), ["http", "https"].contains(scheme),
                      let r = Range(match.range, in: text), text[r].lowercased().hasPrefix(scheme) else { continue }
                let start = text.distance(from: text.startIndex, to: r.lowerBound)
                let length = text.distance(from: r.lowerBound, to: r.upperBound)
                let lower = out.characters.index(run.range.lowerBound, offsetBy: start)
                let upper = out.characters.index(lower, offsetBy: length)
                out[lower..<upper].link = url
            }
        }
        return out
    }

    /// Plain text with its bare web addresses linked.
    public static func apply(_ s: String) -> AttributedString { apply(AttributedString(s)) }
}
