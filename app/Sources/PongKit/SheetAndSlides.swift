import AppKit
import PDFKit
import SwiftUI

/// A sheet Resource as a table: the first row is the header, every cell is text, and
/// the table scrolls both ways. Several sheets get one tab each, above the table.
struct SheetView: View {
    var content: SheetContent
    @State private var chosen = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if content.sheets.count > 1 {
                ScrollView(.horizontal) {
                    HStack(spacing: 6) {
                        ForEach(content.sheets.indices, id: \.self) { i in
                            SheetTab(name: content.sheets[i].name, chosen: i == chosen) { chosen = i }
                        }
                    }
                }
                .scrollIndicators(.never)
            }
            if content.sheets.indices.contains(chosen) {
                ScrollView([.horizontal, .vertical]) {
                    SheetTable(sheet: content.sheets[chosen])
                }
            }
        }
    }
}

/// One sheet's tab: the chosen one is ink, as anything chosen is.
private struct SheetTab: View {
    var name: String
    var chosen: Bool
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            Text(name)
                .brandStyle(.label)
                .foregroundStyle(chosen ? Brand.onSelected : Brand.text)
                .lineLimit(1)
                .fixedSize()
                .padding(.horizontal, 10)
                .frame(minHeight: 26)
                .background(chosen ? Brand.selected : hover ? Brand.rowHover : Brand.row, in: Rectangle())
                .contentShape(Rectangle())
        }
        .buttonStyle(FocusedPlain())
        .hoverHighlight($hover)
        .accessibilityAddTraits(chosen ? [.isSelected] : [])
    }
}

/// The cells in a grid whose columns line up; the header row is a step of colour above the rest.
private struct SheetTable: View {
    var sheet: SheetContent.Sheet

    var body: some View {
        Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
            ForEach(sheet.rows.indices, id: \.self) { i in
                GridRow {
                    ForEach(Array(sheet.row(i).enumerated()), id: \.offset) { _, cell in
                        Text(cell)
                            .brandStyle(i == 0 ? .label : .secondary)
                            .foregroundStyle(Brand.text)
                            .lineLimit(1)
                            .fixedSize()
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
                .background(i == 0 ? Brand.rowHover : .clear)
            }
        }
        .textSelection(.enabled)
        .fixedSize()
        .background(Brand.row, in: Rectangle())
    }
}

/// Slides as PDF pages drawn with PDFKit, one under the other.
struct SlidesView: View {
    var document: PDFDocument
    /// How wide each page is drawn, with its backing.
    var width: CGFloat
    static let inset: CGFloat = 10

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(0..<document.pageCount, id: \.self) { i in
                if let page = document.page(at: i) {
                    let bounds = page.bounds(for: .mediaBox)
                    let inner = width - 2 * Self.inset
                    let size = CGSize(width: width, height: (bounds.width > 0 ? inner * bounds.height / bounds.width : inner) + 2 * Self.inset)
                    VStack(alignment: .leading, spacing: 4) {
                        // On a step of colour, so a white slide stands apart from a white surface.
                        Image(nsImage: page.thumbnail(of: CGSize(width: size.width * 2, height: size.height * 2), for: .mediaBox))
                            .resizable()
                            .frame(width: size.width - 2 * Self.inset, height: size.height - 2 * Self.inset)
                            .padding(Self.inset)
                            .background(Brand.row, in: Rectangle())
                            .accessibilityLabel("Slide \(i + 1) of \(document.pageCount)")
                        Text("\(i + 1) of \(document.pageCount)").brandStyle(.secondary).foregroundStyle(Brand.secondary)
                            .accessibilityHidden(true)
                    }
                }
            }
        }
    }
}
