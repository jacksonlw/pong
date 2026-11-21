import AppKit
import PDFKit
import SwiftUI

/// The size a preview window beside the card opens at.
public let previewSize = CGSize(width: 520, height: 560)

/// One Resource previewed: its title and kind, then its content as befits the kind.
public struct ResourcePreview: View {
    var resource: Resource
    var data: Data?
    /// The human dragged the preview's window by its title; nil where the preview has no window of its own.
    var moved: (() -> Void)?
    /// The human double-clicked the title to put the preview back beside the card.
    var putBack: () -> Void
    var onClose: () -> Void

    public init(resource: Resource, data: Data?, moved: (() -> Void)? = nil, putBack: @escaping () -> Void = {},
                onClose: @escaping () -> Void) {
        self.resource = resource
        self.data = data
        self.moved = moved
        self.putBack = putBack
        self.onClose = onClose
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            PanelTitle(symbol: symbol(forKind: resource.kind), title: resource.title, closeLabel: "Close preview",
                       moved: moved, putBack: putBack, onClose: onClose)
            if resource.kind == "sheet", let data, let sheet = SheetContent.decode(data) {
                // A table scrolls both ways inside its own frame, so it is not put in the page's scroll.
                SheetView(content: sheet)
                    .padding([.horizontal, .bottom], 16)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            } else if let lines = longLines() {
                // A long change or file is a page of its own that draws only the lines on screen.
                Group {
                    if resource.kind == "change" { DiffView.lazy(lines) } else { Mono.lazy(lines) }
                }
                .padding([.horizontal, .bottom], 16)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            } else {
                ScrollView {
                    content()
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(16)
                }
            }
        }
        .frame(idealWidth: previewSize.width, maxWidth: .infinity, idealHeight: previewSize.height, maxHeight: .infinity)
        .background(Brand.surface, in: Rectangle())
    }

    /// The lines of a change, file or output too long to draw at once; nil for anything else.
    private func longLines() -> [Substring]? {
        guard let data, ["change", "output", "file", "error"].contains(resource.kind) else { return nil }
        let lines = splitLines(String(decoding: data, as: UTF8.self))
        return lines.count > lazyLineCount ? lines : nil
    }

    @ViewBuilder private func content() -> some View {
        let text = data.map { String(decoding: $0, as: UTF8.self) } ?? ""
        if data == nil {
            Text("Loading…").brandStyle(.body).foregroundStyle(Brand.secondary)
        } else {
            switch resource.kind {
            case "image":
                if let img = NSImage(data: data!) {
                    Image(nsImage: img).resizable().aspectRatio(contentMode: .fit)
                        .clipShape(Rectangle())
                        .contentShape(Rectangle())
                        .onTapGesture { ImageViewer.show(img, title: resource.title) }
                        .help("Click to see it full screen")
                        .accessibilityAddTraits(.isButton)
                        .accessibilityHint("Shows the image full screen")
                } else {
                    Text("This image cannot be shown.").brandStyle(.body).foregroundStyle(Brand.secondary)
                }
            case "document": MarkdownView(source: text)
            case "change": DiffView(source: text)
            case "error": Mono(text: text, error: true)
            case "output", "file": Mono(text: text)
            case "slides":
                if let pdf = PDFDocument(data: data!) {
                    SlidesView(document: pdf, width: previewSize.width - 32)
                } else {
                    Text("These slides cannot be shown.").brandStyle(.body).foregroundStyle(Brand.secondary)
                }
            case "sheet": Text("This sheet cannot be shown.").brandStyle(.body).foregroundStyle(Brand.secondary)
            default: Text(inline(text)).brandStyle(.body).foregroundStyle(Brand.text).textSelection(.enabled)
            }
        }
    }
}

private let monoFont = Brand.Style.monospace.font

/// Inline markdown (bold, code, links) via Foundation; the text as is when it does not parse.
/// Links take the link role and are underlined, not the system accent colour.
func inline(_ s: String) -> AttributedString {
    var out = (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(s)
    for run in out.runs where run.link != nil {
        out[run.range].foregroundColor = Brand.link
        out[run.range].underlineStyle = .single
    }
    return out
}

/// Monospaced text in a row-coloured block; an error's block carries a warning sign before it.
struct Mono: View {
    var text: String
    var error = false

    var body: some View {
        let lines = HStack(alignment: .firstTextBaseline, spacing: 8) {
            if error {
                Image(systemName: "exclamationmark.triangle.fill").imageScale(.large)
                    .font(Brand.Style.monospace.font)
                    .brandIcon()
                    .foregroundStyle(Brand.warning)
                    .accessibilityLabel("Error")
            }
            Text(text.trimmingCharacters(in: .newlines))
                .brandStyle(.monospace)
                .foregroundStyle(Brand.text)
                .textSelection(.enabled)
                .fixedSize()
        }
        .padding(10)
        // One line per line: too wide a block scrolls sideways instead of wrapping.
        ViewThatFits(in: .horizontal) {
            lines.frame(maxWidth: .infinity, alignment: .leading)
            ScrollView(.horizontal) { lines }
        }
            .background(Brand.row)
            .clipShape(Rectangle())
    }
}

extension Mono {
    /// A long text's lines, drawn as they come on screen.
    static func lazy(_ lines: [Substring]) -> some View {
        LazyLines(lines: lines, gutter: 20) { line in
            Text(line).brandStyle(.monospace).foregroundStyle(Brand.text).fixedSize().padding(.horizontal, 10)
        }
    }
}

/// A unified diff, one row per line. Added and removed lines keep their + and − in a gutter and
/// are tinted, so they differ by more than colour alone.
struct DiffView: View {
    var source: String

    var body: some View {
        let lines = splitLines(source)
        let rows = VStack(alignment: .leading, spacing: 0) {
            ForEach(lines.indices, id: \.self) { i in Self.row(lines[i]) }
        }
        .textSelection(.enabled)
        .padding(.vertical, 6)
        // One line per line: too wide a diff scrolls sideways instead of wrapping.
        ViewThatFits(in: .horizontal) {
            rows
            ScrollView(.horizontal) { rows }
        }
        .background(Brand.row, in: Rectangle())
        .clipShape(Rectangle())
    }

    /// A long diff's lines, drawn as they come on screen.
    static func lazy(_ lines: [Substring]) -> some View {
        LazyLines(lines: lines, gutter: 44) { row($0) }
    }

    @ViewBuilder static func row(_ line: Substring) -> some View {
        let header = line.hasPrefix("+++") || line.hasPrefix("---") || line.hasPrefix("diff ") || line.hasPrefix("index ")
        let hunk = line.hasPrefix("@@")
        let added = !header && line.hasPrefix("+")
        let removed = !header && line.hasPrefix("-")
        let body = added || removed || line.hasPrefix(" ") ? line.dropFirst() : line
        HStack(alignment: .firstTextBaseline, spacing: 0) {
            Text(added ? "+" : removed ? "−" : " ")
                .font(monoFont.weight(.bold))
                .foregroundStyle(added ? Brand.added : removed ? Brand.removed : Brand.secondary)
                .frame(width: 22)
            Text(header || hunk ? line : body)
                .font(header ? monoFont.weight(.semibold) : monoFont)
                .foregroundStyle(header || hunk ? Brand.secondary : Brand.text)
                .fixedSize()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 1)
        // As much room after the longest line as the sign gutter leaves before it.
        .padding(.trailing, 22)
        .background(added ? Brand.addedWash : removed ? Brand.removedWash : .clear)
        .accessibilityLabel(added ? "Added: \(body)" : removed ? "Removed: \(body)" : String(line))
    }
}
