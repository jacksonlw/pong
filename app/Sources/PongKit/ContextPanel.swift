import PongDocument
import SwiftUI

/// The context shown beside the card for what the pointer rests on: the question's background,
/// why the agent asks and what each answer leads to; one answer's meaning; or the ring's figures.
public struct ContextPanel: View {
    var sections: [Section]
    /// The tallest the context may be; taller content scrolls inside it. Nil takes the content's height.
    public var maxHeight: CGFloat?

    public static let width: CGFloat = 350
    /// The tallest the context grows beside the card before its content scrolls.
    public static let tallest: CGFloat = 480

    typealias Section = ContextSection

    /// Nil when there is nothing to explain, so nothing is shown.
    public init?(topic: ContextHover.Topic, card: Directory.Card) {
        let s = Self.sections(topic, card)
        if s.isEmpty { return nil }
        sections = s
    }

    static func sections(_ topic: ContextHover.Topic, _ card: Directory.Card) -> [Section] { card.sections(topic) }

    static func tokens(_ n: Int) -> String { ContextFigures.tokens(n) }

    public var body: some View {
        if let maxHeight {
            ScrollView(.vertical) { list }
                .scrollIndicators(.automatic)
                .frame(width: Self.width, height: maxHeight)
                .background(Brand.surface, in: Rectangle())
        } else {
            list.background(Brand.surface, in: Rectangle())
        }
    }

    private var list: some View {
        ContextSectionList(sections: sections)
            .padding(.horizontal, 14)
            .padding(.top, 2)
            .padding(.bottom, 14)
            .frame(width: Self.width, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// The labelled sections of a context view, drawn the same beside the card and in the Thread.
struct ContextSectionList: View {
    var sections: [ContextSection]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(sections.enumerated()), id: \.offset) { _, s in
                if !s.label.isEmpty {
                    Text(s.label)
                        .brandStyle(.label)
                        .foregroundStyle(Brand.secondary)
                        .padding(.top, 12)
                        .padding(.bottom, 4)
                }
                content(s)
            }
        }
        .brandStyle(.body)
        .foregroundStyle(Brand.text)
    }

    @ViewBuilder private func content(_ s: ContextSection) -> some View {
        switch s.body {
        case .text(let t):
            VStack(alignment: .leading, spacing: 6) {
                if !t.isEmpty { BlocksView(blocks: MarkdownReader.blocks(t)) }
                if let w = s.warning { hardText(w) }
            }
        case .answers(let answers):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(answers, id: \.label) { a in
                    (Text("\(a.label): ").font(Brand.Style.answer.font) + Text(Self.inline(a.leadsTo ?? ""))
                        + (a.hardToUndo == true ? Text("  Hard to undo").font(Brand.Style.answer.font).foregroundColor(Brand.warning) : Text("")))
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        case .figures(let rows):
            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 4) {
                ForEach(rows, id: \.0) { k, v in
                    GridRow {
                        Text(k).foregroundStyle(Brand.secondary)
                        Text(v)
                    }
                }
            }
            .padding(.top, 8)
        }
    }

    /// One line of Markdown, such as what an answer leads to: emphasis, code and links, no blocks.
    static func inline(_ s: String) -> AttributedString {
        let a = (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            ?? AttributedString(s)
        return styled(Autolink.apply(a))
    }

    private func hardText(_ t: String) -> some View {
        Text(t).brandStyle(.answer).foregroundStyle(Brand.warning)
    }
}
