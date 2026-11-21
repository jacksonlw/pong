import SwiftUI

/// The website's Docs, where an ⓘ's "Learn more" goes.
enum Docs {
    static let base = URL(string: "https://pong.fly/docs/")!

    /// A page of the Docs and the section on it, e.g. `page("settings", "voice")`.
    static func page(_ page: String, _ section: String) -> URL {
        URL(string: "\(page)#\(section)", relativeTo: base)!.absoluteURL
    }
}

/// An ⓘ beside a Settings heading or row: a click shows what it explains in a popover that ends
/// with "Learn more", which opens that part of the Docs in the browser.
struct InfoButton: View {
    var about: String
    var text: String
    var learnMore: URL
    @State private var shown = false

    /// The ⓘ's size, the same beside a heading and beside a row label.
    static let size: CGFloat = 14

    var body: some View {
        HoverButton { shown.toggle() } label: { hover in
            Image(systemName: "info.circle")
                .resizable()
                .fontWeight(Brand.iconWeight)
                .scaledToFit()
                .frame(width: Self.size, height: Self.size)
                .foregroundStyle(hover ? Brand.text : Brand.secondary)
                .contentShape(Rectangle())
        }
        .accessibilityLabel("About \(about)")
        .popover(isPresented: $shown, arrowEdge: .bottom) {
            InfoPopover(text: text, learnMore: learnMore)
        }
    }
}

/// What an ⓘ's popover holds: the explanation and "Learn more".
struct InfoPopover: View {
    var text: String
    var learnMore: URL
    @Environment(\.openURL) private var openURL

    static let width: CGFloat = 280

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(text)
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
                .fixedSize(horizontal: false, vertical: true)
            HoverButton { openURL(learnMore) } label: { hover in
                Text("Learn more")
                    .brandStyle(.label)
                    .foregroundStyle(hover ? Brand.secondary : Brand.link)
                    .underline()
            }
            .accessibilityHint("Opens the Docs in your browser")
        }
        .padding(14)
        .frame(width: Self.width, alignment: .leading)
        .background(Brand.surface)
    }
}

extension VerticalAlignment {
    private enum CapMiddle: AlignmentID {
        static func defaultValue(in d: ViewDimensions) -> CGFloat { d[VerticalAlignment.center] }
    }
    /// The middle of a line's capital letters, so an ⓘ beside a label sits level with its words
    /// rather than with the line's box, which line height pads below and above.
    static let capMiddle = VerticalAlignment(CapMiddle.self)
}

extension View {
    /// Puts this text's `.capMiddle` halfway up its first line's capitals, for a label in `style`.
    func capCentred(_ style: Brand.Style) -> some View {
        alignmentGuide(.capMiddle) { d in d[.firstTextBaseline] - style.size * Self.interCapHeight / 2 }
    }

    /// Inter's capital height, as a fraction of its size.
    private static var interCapHeight: CGFloat { 0.727 }
}
