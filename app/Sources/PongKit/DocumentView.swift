import PongDocument
import SwiftUI

// A document Resource drawn natively from the blocks PongDocument reads. Other apps may draw the same
// blocks with the same tokens.

/// A markdown document: GitHub-flavoured blocks, highlighted code and Mermaid diagrams.
struct MarkdownView: View {
    var source: String

    var body: some View {
        BlocksView(blocks: MarkdownReader.blocks(source), spacing: 10)
            .foregroundStyle(Brand.text)
            .textSelection(.enabled)
    }
}

/// Inline text as SwiftUI draws it: links take the link role and an underline, not the accent
/// colour, and inline code sits on a step of colour from what is behind it (`code`).
func styled(_ s: AttributedString, code: Color = Brand.row) -> AttributedString {
    var out = s
    for run in out.runs {
        if run.link != nil {
            out[run.range].foregroundColor = Brand.link
            out[run.range].underlineStyle = .single
        }
        if run.inlinePresentationIntent?.contains(.code) == true {
            out[run.range].backgroundColor = code
        }
    }
    return out
}

struct BlocksView: View {
    var blocks: [MarkdownBlock]
    var spacing: CGFloat = 6

    var body: some View {
        VStack(alignment: .leading, spacing: spacing) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { i, b in block(b, first: i == 0) }
        }
    }

    @ViewBuilder private func block(_ b: MarkdownBlock, first: Bool) -> some View {
        switch b {
        case let .heading(level, s):
            Text(styled(s))
                .brandStyle(level == 1 ? .question : level == 2 ? .answer : .label)
                .padding(.top, first ? 0 : 8)
                .accessibilityAddTraits(.isHeader)
        case let .paragraph(s):
            Text(styled(s)).brandStyle(.body).fixedSize(horizontal: false, vertical: true)
        case let .list(start, items):
            ListBlock(start: start, items: items)
        case let .quote(inner):
            // A quote is set in and receding, with no rule beside it and no block of colour, so
            // code and tables inside it keep their own step of colour.
            BlocksView(blocks: inner)
                .foregroundStyle(Brand.secondary)
                .padding(.leading, 16)
        case let .code(language, text):
            if language == "diff" { DiffView(source: text) } else { CodeBlock(language: language, text: text) }
        case let .mermaid(source):
            MermaidView(source: source) { CodeBlock(language: nil, text: source) }
        case let .table(t):
            TableBlock(table: t)
        case .rule:
            // Nothing is a divider: a break between sections is more space.
            Color.clear.frame(height: 8).accessibilityHidden(true)
        }
    }
}

private struct ListBlock: View {
    var start: Int?
    var items: [MarkdownItem]

    var body: some View {
        // Every marker is as wide as the widest, so the items line up past 9.
        let widest = start.map { "\($0 + items.count - 1)." } ?? "•"
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(items.enumerated()), id: \.offset) { n, item in
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Group {
                        if let checked = item.checked {
                            Image(systemName: checked ? "checkmark.square.fill" : "square").imageScale(.large)
                                .brandIcon()
                                // Drawn larger than the text, in a box no taller than it.
                                .frame(height: Brand.Style.body.size)
                                .accessibilityLabel(checked ? "Done" : "Not done")
                        } else {
                            Text(start.map { "\($0 + n)." } ?? "•")
                        }
                    }
                    .brandStyle(.body)
                    .foregroundStyle(Brand.secondary)
                    .frame(minWidth: 16, alignment: .trailing)
                    .background(alignment: .trailing) { Text(widest).brandStyle(.body).hidden() }
                    BlocksView(blocks: item.blocks)
                }
            }
        }
    }
}

/// Fenced code, highlighted: keywords are heavier and comments recede, so the shape of the code
/// shows without colour meaning anything.
struct CodeBlock: View {
    var language: String?
    var text: String

    var body: some View {
        var a = AttributedString()
        for run in Highlight.runs(text, language: language) {
            var piece = AttributedString(run.text)
            switch run.kind {
            case .keyword: piece.font = Brand.Style.monospace.font.weight(.semibold)
            case .comment: piece.foregroundColor = Brand.secondary
            case .string, .number, nil: break
            }
            a += piece
        }
        let lines = Text(a)
            .brandStyle(.monospace)
            .foregroundStyle(Brand.text)
            .textSelection(.enabled)
            .fixedSize()
            .padding(10)
        // One line per line: too wide a block scrolls sideways instead of wrapping.
        return ViewThatFits(in: .horizontal) {
            lines.frame(maxWidth: .infinity, alignment: .leading)
            ScrollView(.horizontal) { lines }
        }
        .background(Brand.row)
        .clipShape(Rectangle())
    }
}

/// A markdown table: the header row a step of colour above the rest and columns aligned as the
/// separator row says. A table too wide for the page wraps its longest cells to fit.
private struct TableBlock: View {
    var table: MarkdownTable
    /// How tall each header cell is, by layout (wrapping or not) and column, so the header's step
    /// of colour can run the full width behind the row, as tall as its tallest cell.
    @State private var headerCells: [Bool: [Int: CGFloat]] = [:]

    var body: some View {
        ViewThatFits(in: .horizontal) {
            grid(wrapping: false)
            grid(wrapping: true)
        }
    }

    private func grid(wrapping: Bool) -> some View {
        Grid(alignment: .topLeading, horizontalSpacing: 0, verticalSpacing: 0) {
            row(table.head, header: true, wrapping: wrapping)
            ForEach(Array(table.rows.enumerated()), id: \.offset) { _, cells in row(cells, header: false, wrapping: wrapping) }
        }
        .fixedSize(horizontal: !wrapping, vertical: true)
        .background(alignment: .top) { Brand.rowHover.frame(height: headerCells[wrapping]?.values.max() ?? 0) }
        .background(Brand.row, in: Rectangle())
    }

    private func alignment(_ i: Int) -> (HorizontalAlignment, TextAlignment) {
        switch table.alignments.indices.contains(i) ? table.alignments[i] : nil {
        case .trailing: (.trailing, .trailing)
        case .center: (.center, .center)
        default: (.leading, .leading)
        }
    }

    private func row(_ cells: [AttributedString], header: Bool, wrapping: Bool) -> some View {
        GridRow {
            ForEach(Array(cells.enumerated()), id: \.offset) { i, cell in
                let (column, text) = alignment(i)
                Text(styled(cell, code: Brand.surface))
                    .brandStyle(header ? .label : .secondary)
                    .multilineTextAlignment(text)
                    .fixedSize(horizontal: !wrapping, vertical: true)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { h in
                        if header { headerCells[wrapping, default: [:]][i] = h }
                    }
                    .gridColumnAlignment(column)
            }
        }
    }
}
