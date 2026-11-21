import Foundation
import Markdown

/// One block of a document, ready for either app to draw. Inline text is an AttributedString whose
/// runs carry Foundation's presentation intents (bold, slanted, code, struck through) and links,
/// which SwiftUI's Text draws as they are.
public enum MarkdownBlock: Equatable, Sendable {
    case heading(Int, AttributedString)
    case paragraph(AttributedString)
    /// A list; `start` is the first number of an ordered list, nil for a bulleted one.
    case list(start: Int?, items: [MarkdownItem])
    case quote([MarkdownBlock])
    case code(language: String?, text: String)
    case mermaid(String)
    case table(MarkdownTable)
    case rule
}

public struct MarkdownItem: Equatable, Sendable {
    /// A task item's box: true ticked, false empty, nil for an item with no box.
    public var checked: Bool?
    public var blocks: [MarkdownBlock]
}

public struct MarkdownTable: Equatable, Sendable {
    public enum Alignment: Equatable, Sendable { case leading, center, trailing }
    /// Each column's alignment as the separator row gives it; nil where it gives none.
    public var alignments: [Alignment?]
    public var head: [AttributedString]
    public var rows: [[AttributedString]]
}

/// GitHub-flavoured markdown (tables, task lists, strikethrough) read by swift-markdown.
public enum MarkdownReader {
    public static func blocks(_ source: String) -> [MarkdownBlock] {
        blocks(of: Document(parsing: source))
    }

    static func blocks(of parent: Markup) -> [MarkdownBlock] {
        parent.children.compactMap(block)
    }

    static func block(_ m: Markup) -> MarkdownBlock? {
        switch m {
        case let h as Heading: return .heading(h.level, inline(h))
        case let p as Paragraph: return .paragraph(inline(p))
        case let l as OrderedList: return .list(start: Int(l.startIndex), items: items(l))
        case let l as UnorderedList: return .list(start: nil, items: items(l))
        case let q as BlockQuote: return .quote(blocks(of: q))
        case let c as CodeBlock:
            let text = c.code.hasSuffix("\n") ? String(c.code.dropLast()) : c.code
            let lang = c.language?.lowercased()
            return lang == "mermaid" ? .mermaid(text) : .code(language: lang, text: text)
        case is ThematicBreak: return .rule
        case let t as Table:
            return .table(MarkdownTable(
                alignments: t.columnAlignments.map {
                    switch $0 { case .left: .leading; case .center: .center; case .right: .trailing; case nil: nil }
                },
                head: t.head.children.map(inline),
                rows: t.body.children.map { $0.children.map(inline) }))
        // Raw HTML is shown as the source it is, since neither app draws HTML.
        case let h as HTMLBlock: return .code(language: "html", text: h.rawHTML.trimmingCharacters(in: .newlines))
        default: return nil
        }
    }

    static func items(_ list: ListItemContainer) -> [MarkdownItem] {
        list.listItems.map { item in
            MarkdownItem(checked: item.checkbox.map { $0 == .checked }, blocks: blocks(of: item))
        }
    }

    /// A block's inline children as one AttributedString.
    static func inline(_ m: Markup) -> AttributedString {
        var out = AttributedString()
        append(m.children, to: &out, intent: [], link: nil)
        return Autolink.apply(out)
    }

    static func append(_ children: MarkupChildren, to out: inout AttributedString, intent: InlinePresentationIntent, link: URL?) {
        for child in children {
            func put(_ s: String, _ extra: InlinePresentationIntent = []) {
                var a = AttributedString(s)
                let i = intent.union(extra)
                if !i.isEmpty { a.inlinePresentationIntent = i }
                if let link { a.link = link }
                out += a
            }
            switch child {
            case let t as Text: put(t.string)
            case let c as InlineCode: put(c.code, .code)
            case is SoftBreak: put(" ")
            case is LineBreak: put("\n")
            case let h as InlineHTML: put(h.rawHTML)
            case let e as Emphasis: append(e.children, to: &out, intent: intent.union(.emphasized), link: link)
            case let s as Strong: append(s.children, to: &out, intent: intent.union(.stronglyEmphasized), link: link)
            case let s as Strikethrough: append(s.children, to: &out, intent: intent.union(.strikethrough), link: link)
            case let l as Link: append(l.children, to: &out, intent: intent, link: l.destination.flatMap(URL.init(string:)) ?? link)
            // An image in a document is shown as its words, linked to the picture.
            case let i as Image: append(i.children, to: &out, intent: intent, link: i.source.flatMap(URL.init(string:)) ?? link)
            default: append(child.children, to: &out, intent: intent, link: link)
            }
        }
    }
}
