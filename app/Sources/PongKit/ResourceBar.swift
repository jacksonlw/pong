import AppKit
import SwiftUI

extension EnvironmentValues {
    @Entry public var resourceStore: ResourceStore = .shared
}

/// A Ping's Resources as a bar of files on their own card below the question card: each one
/// an icon for its kind (an image shows its thumbnail) with its title beneath. Two rows show,
/// and more scroll. Opening one shows it in a preview window beside the card; several can be open.
struct ResourceBar: View {
    var ping: Ping
    @Environment(\.resourceStore) private var store
    @Environment(\.linkOpener) private var clicks

    static let perRow = 3
    static let spacing: CGFloat = 8
    static let padding: CGFloat = 10
    static let iconHeight: CGFloat = 64
    static let titleGap: CGFloat = 6
    /// Two lines of the title's type role, so every tile, and so the two-row cap, is the same height.
    static var titleHeight: CGFloat { 2 * Brand.Style.secondary.size * Brand.Style.secondary.lineHeight }
    static var tileHeight: CGFloat { iconHeight + titleGap + titleHeight }
    @Environment(\.chosenCardWidth) private var width

    /// A tile's width on a card of the given width: three fill a row.
    static func tileWidth(on card: CGFloat) -> CGFloat {
        (card - 2 * padding - CGFloat(perRow - 1) * spacing) / CGFloat(perRow)
    }

    var body: some View {
        let resources = ping.resources ?? []
        let tile = Self.tileWidth(on: width)
        let columns = Array(repeating: GridItem(.fixed(tile), spacing: Self.spacing, alignment: .top), count: Self.perRow)
        ScrollView(.vertical) {
            LazyVGrid(columns: columns, alignment: .leading, spacing: Self.spacing) {
                ForEach(resources) { r in
                    ResourceTile(resource: r, thumbnail: thumbnail(r), host: store.linkHost(r), width: tile, opened: store.isOpen(r)) {
                        // A link opens through LinkOpener; it has no preview.
                        if r.isLink { Task { await store.openLink(r, of: ping, clicks: clicks) }; return }
                        // Command adds one and Shift adds a run, as in a Finder window.
                        let keys = NSApp.currentEvent?.modifierFlags ?? []
                        store.open(r, of: ping, keys.contains(.command) ? .added : keys.contains(.shift) ? .through : .alone,
                                   among: resources)
                    }
                    .contextMenu {
                        if r.isLink {
                            Button("Open") { Task { await store.openLink(r, of: ping, clicks: clicks) } }
                        } else {
                            Button("Open") { store.open(r, of: ping) }
                            Button("Open in new window") { store.open(r, of: ping, .detached) }
                            if store.isOpen(r) { Button("Close preview") { store.close(r) } }
                        }
                    }
                    .task(id: r.id) { if r.kind == "image" || r.isLink { await store.load(r, of: ping) } }
                }
            }
            .padding(Self.padding)
        }
        .scrollBounceBehavior(.basedOnSize)
        .frame(maxHeight: 2 * Self.tileHeight + Self.spacing + 2 * Self.padding)
        .frame(width: width)
        .background(Brand.surface, in: Rectangle())
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Resources")
    }

    private func thumbnail(_ r: Resource) -> NSImage? {
        guard r.kind == "image", let d = store.contents[r.id] else { return nil }
        return NSImage(data: d)
    }
}

/// One Resource in the bar: its icon or thumbnail, and its title beneath in up to two lines.
struct ResourceTile: View {
    var resource: Resource
    var thumbnail: NSImage?
    /// A link's host, shown beneath its title in place of the title's second line.
    var host: String? = nil
    var width = ResourceBar.tileWidth(on: cardWidth)
    var opened: Bool
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            VStack(spacing: ResourceBar.titleGap) {
                Group {
                    if let thumbnail {
                        Image(nsImage: thumbnail).resizable().aspectRatio(contentMode: .fill)
                    } else {
                        Image(systemName: symbol(forKind: resource.kind)).imageScale(.large)
                            .font(Brand.Style.question.font)
                            .foregroundStyle(opened ? Brand.onSelected : Brand.secondary)
                    }
                }
                .frame(width: width, height: ResourceBar.iconHeight)
                .background(opened ? Brand.selected : hover ? Brand.rowHover : Brand.row)
                .clipShape(Rectangle())
                VStack(spacing: 0) {
                    Text(resource.title)
                        .foregroundStyle(opened || hover ? Brand.text : Brand.secondary)
                        .lineLimit(host == nil ? 2 : 1)
                    if let host { Text(host).foregroundStyle(Brand.secondary).lineLimit(1) }
                }
                .brandStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(width: width, height: ResourceBar.titleHeight, alignment: .top)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(FocusedPlain())
        .hoverHighlight($hover)
        .help(host.map { "\(resource.title), \($0)" } ?? resource.title)
        .accessibilityLabel("\(resource.title), \(resource.kind)")
        .accessibilityAddTraits(opened ? [.isSelected] : [])
    }
}
