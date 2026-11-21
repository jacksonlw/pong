import AppKit
import SwiftUI

/// Past this many lines a block draws only the lines on screen, so a long diff or file opens at once.
let lazyLineCount = 200

/// The lines of a long block in a page of their own that scrolls both ways and draws only the
/// rows on screen. Every row is as wide as the longest line, measured from the monospace face, so
/// the page does not change width as rows come and go.
struct LazyLines<Row: View>: View {
    var lines: [Substring]
    /// Room each row takes beside its text, such as a sign gutter.
    var gutter: CGFloat = 0
    @ViewBuilder var row: (Substring) -> Row
    @State private var viewport: CGFloat = 0

    var body: some View {
        let width = max(viewport, Self.width(of: lines) + gutter)
        ScrollView([.vertical, .horizontal]) {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(lines.indices, id: \.self) { i in
                    row(lines[i]).frame(width: width, alignment: .leading)
                }
            }
            .padding(.vertical, 6)
            .textSelection(.enabled)
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewport = $0 }
        .background(Brand.row, in: Rectangle())
    }

    /// The widest line in the monospace face, a tab counted as four characters.
    static func width(of lines: [Substring]) -> CGFloat {
        let longest = lines.lazy.map { $0.count + $0.filter { $0 == "\t" }.count * 3 }.max() ?? 0
        let font = Brand.Style.monospaceNSFont
        return ceil(CGFloat(longest) * ("M" as NSString).size(withAttributes: [.font: font]).width)
    }
}

/// The lines of a text, without its leading and trailing blank lines.
func splitLines(_ text: String) -> [Substring] {
    text.trimmingCharacters(in: .newlines).split(separator: "\n", omittingEmptySubsequences: false)
}
