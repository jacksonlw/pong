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
                SheetTable(sheet: content.sheets[chosen]).id(chosen)
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

/// One cell of a sheet: its row, where 0 is the header, and its column.
struct SheetCell: Equatable {
    var row: Int
    var column: Int
}

/// The cells chosen in a sheet: the rectangle from where the choosing began to where it ends.
struct SheetSelection: Equatable {
    var anchor: SheetCell
    var end: SheetCell

    var rows: ClosedRange<Int> { min(anchor.row, end.row)...max(anchor.row, end.row) }
    var columns: ClosedRange<Int> { min(anchor.column, end.column)...max(anchor.column, end.column) }
    func contains(_ c: SheetCell) -> Bool { rows.contains(c.row) && columns.contains(c.column) }
}

/// A sheet laid out as a grid: each column as wide as its widest cell, up to a limit, and every
/// row one height, so a point maps to a cell.
enum SheetGrid {
    static let rowHeight: CGFloat = 28
    static let padding: CGFloat = 10
    static let widest: CGFloat = 320
    static let narrowest: CGFloat = 44

    static func widths(_ sheet: SheetContent.Sheet) -> [CGFloat] {
        // The brand's face at the sizes the cells draw in, the header's bold so it is never measured short.
        let body = Brand.Style.bodyNSFont.withSize(Brand.Style.secondary.size)
        let head = NSFontManager.shared.convert(Brand.Style.bodyNSFont.withSize(Brand.Style.label.size), toHaveTrait: .boldFontMask)
        return (0..<sheet.columns).map { c in
            var w: CGFloat = 0
            for r in sheet.rows.indices {
                let cell = sheet.row(r)[c]
                guard !cell.isEmpty else { continue }
                w = max(w, (cell as NSString).size(withAttributes: [.font: r == 0 ? head : body]).width)
            }
            return min(widest, max(narrowest, (w + 2 * padding + 2).rounded(.up)))
        }
    }

    /// The cell at a point in the grid, kept inside it.
    static func cell(at p: CGPoint, widths: [CGFloat], rows: Int) -> SheetCell {
        var x: CGFloat = 0, column = max(0, widths.count - 1)
        for (i, w) in widths.enumerated() {
            x += w
            if p.x < x { column = i; break }
        }
        if p.x < 0 { column = 0 }
        let row = min(max(0, Int((p.y / rowHeight).rounded(.down))), max(0, rows - 1))
        return SheetCell(row: row, column: column)
    }

    /// The chosen cells as text that pastes into a spreadsheet: tabs between cells, a line per row.
    /// A cell holding a tab, a line break or a quote is quoted, as spreadsheets write it.
    static func text(_ sheet: SheetContent.Sheet, _ s: SheetSelection) -> String {
        s.rows.filter { $0 < sheet.rows.count }.map { r in
            let row = sheet.row(r)
            return s.columns.filter { $0 < row.count }.map { c in
                let v = row[c]
                return v.contains(where: { $0 == "\t" || $0 == "\n" || $0 == "\r" || $0 == "\"" })
                    ? "\"" + v.replacingOccurrences(of: "\"", with: "\"\"") + "\"" : v
            }.joined(separator: "\t")
        }.joined(separator: "\n")
    }
}

/// The sheet as a light spreadsheet: a grid of cells whose header row stays in view while the
/// rest scrolls. A click chooses a cell, a drag or a Shift-click a block of them, and ⌘C copies
/// them to paste into a spreadsheet.
struct SheetTable: View {
    var sheet: SheetContent.Sheet
    @State private var selection: SheetSelection?

    var body: some View {
        let widths = SheetGrid.widths(sheet)
        let width = widths.reduce(0, +)
        ScrollView([.horizontal, .vertical]) {
            LazyVStack(alignment: .leading, spacing: 0, pinnedViews: [.sectionHeaders]) {
                Section {
                    ForEach(Array(sheet.rows.indices.dropFirst()), id: \.self) { r in row(r, widths) }
                } header: {
                    if !sheet.rows.isEmpty {
                        // The header row is drawn where it is pinned, so it takes its own clicks.
                        row(0, widths).overlay { choosing(widths, header: true) }
                    }
                }
            }
            .frame(width: width, alignment: .leading)
            .overlay { choosing(widths, header: false) }
        }
        // A sheet narrower or shorter than the panel starts at its top-left, as a spreadsheet does.
        .defaultScrollAnchor(.topLeading)
    }

    private func choosing(_ widths: [CGFloat], header: Bool) -> some View {
        SheetChooser(sheet: sheet, selection: $selection, header: header) { p in
            var at = SheetGrid.cell(at: p, widths: widths, rows: sheet.rows.count)
            if header { at.row = 0 }
            return at
        }
    }

    private func row(_ r: Int, _ widths: [CGFloat]) -> some View {
        let cells = sheet.row(r)
        return HStack(spacing: 0) {
            ForEach(cells.indices, id: \.self) { c in
                let chosen = selection?.contains(SheetCell(row: r, column: c)) == true
                Text(cells[c])
                    .brandStyle(r == 0 ? .label : .secondary)
                    .foregroundStyle(chosen ? Brand.onSelected : Brand.text)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .padding(.horizontal, SheetGrid.padding)
                    .frame(width: widths[c], height: SheetGrid.rowHeight, alignment: .leading)
                    .background(chosen ? Brand.selected : r == 0 ? Brand.rowHover : Brand.row)
                    // Lines between cells, a step of colour, as a spreadsheet draws them.
                    .overlay(alignment: .trailing) { Brand.rowHover.frame(width: 1) }
                    .overlay(alignment: .bottom) { Brand.rowHover.frame(height: 1) }
                    .accessibilityLabel(cells[c].isEmpty ? "Empty" : cells[c])
                    .accessibilityAddTraits(chosen ? [.isSelected] : [])
            }
        }
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

/// Takes the clicks on a sheet's cells and its ⌘C. An AppKit view, so the copy comes to it through
/// the responder chain in whichever panel the sheet is in.
private struct SheetChooser: NSViewRepresentable {
    var sheet: SheetContent.Sheet
    @Binding var selection: SheetSelection?
    var header: Bool
    var cell: (CGPoint) -> SheetCell

    func makeNSView(context: Context) -> Area { Area() }

    func updateNSView(_ v: Area, context: Context) {
        v.choose = { p, extend, start in
            let at = cell(p)
            if start, !extend || selection == nil {
                selection = SheetSelection(anchor: at, end: at)
            } else {
                selection?.end = at
            }
        }
        v.text = { selection.map { SheetGrid.text(sheet, $0) } }
        v.tip = { p in
            let at = cell(p)
            return at.row < sheet.rows.count ? sheet.row(at.row)[at.column] : nil
        }
        v.clear = { selection = nil }
        v.underHeader = !header
    }

    final class Area: NSView, NSViewToolTipOwner {
        /// A point from the top-left, whether it extends the choice, and whether a click starts here.
        var choose: (CGPoint, Bool, Bool) -> Void = { _, _, _ in }
        var text: () -> String? = { nil }
        var clear: () -> Void = {}
        /// A cell's whole text, for one too long for its column.
        var tip: (CGPoint) -> String? = { _ in nil }

        /// The body's area leaves the header row, pinned at the top of what shows, to the header's.
        var underHeader = false

        override var isFlipped: Bool { true }

        override func hitTest(_ p: NSPoint) -> NSView? {
            if underHeader, let superview {
                let local = convert(p, from: superview)
                if local.y < visibleRect.minY + SheetGrid.rowHeight { return nil }
            }
            return super.hitTest(p)
        }

        override func setFrameSize(_ s: NSSize) {
            super.setFrameSize(s)
            removeAllToolTips()
            addToolTip(bounds, owner: self, userData: nil)
        }

        func view(_ view: NSView, stringForToolTip tag: NSView.ToolTipTag, point: NSPoint, userData: UnsafeMutableRawPointer?) -> String {
            tip(point) ?? ""
        }
        override var acceptsFirstResponder: Bool { true }
        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
        override var needsPanelToBecomeKey: Bool { true }

        override func mouseDown(with e: NSEvent) {
            window?.makeKey()
            window?.makeFirstResponder(self)
            choose(convert(e.locationInWindow, from: nil), e.modifierFlags.contains(.shift), true)
        }

        override func mouseDragged(with e: NSEvent) {
            autoscroll(with: e)
            choose(convert(e.locationInWindow, from: nil), true, false)
        }

        @objc func copy(_ sender: Any?) {
            guard let t = text() else { return }
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(t, forType: .string)
        }

        override func cancelOperation(_ sender: Any?) { clear() }

        override func responds(to s: Selector!) -> Bool {
            // ⌘C is offered only while cells are chosen.
            if s == #selector(copy(_:)) { return text() != nil }
            return super.responds(to: s)
        }
    }
}
