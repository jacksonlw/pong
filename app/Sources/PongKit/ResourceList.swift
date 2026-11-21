import AppKit
import SwiftUI

/// A Ping's Resources as rows in its Thread, each with its title and an icon for its kind (an
/// image shows its thumbnail). Opening one shows it in the preview window.
struct ResourceList: View {
    var ping: Ping
    @Environment(\.resourceStore) private var store
    @Environment(\.linkOpener) private var clicks

    var body: some View {
        VStack(spacing: 8) {
            ForEach(ping.resources ?? []) { r in
                ResourceRow(resource: r, thumbnail: thumbnail(r), host: store.linkHost(r), opened: store.isOpen(r)) {
                    if r.isLink { Task { await store.openLink(r, of: ping, clicks: clicks) } } else { store.open(r, of: ping) }
                }
                .task(id: r.id) { if r.kind == "image" || r.isLink { await store.load(r, of: ping) } }
            }
        }
    }

    private func thumbnail(_ r: Resource) -> NSImage? {
        guard r.kind == "image", let d = store.contents[r.id] else { return nil }
        return NSImage(data: d)
    }
}

extension ResourceStore {
    /// The host shown beneath a link Resource's title, once its content is here.
    func linkHost(_ r: Resource) -> String? {
        guard r.isLink, let d = contents[r.id] else { return nil }
        return Resource.linkHost(d)
    }

    /// Opens a link Resource: a web link at once, an app link once the human allows its scheme
    /// (LinkOpener); it has no preview.
    func openLink(_ r: Resource, of ping: Ping, clicks: LinkOpener?) async {
        await load(r, of: ping)
        guard let d = contents[r.id], let address = Resource.linkAddress(d) else { return }
        await clicks?.open(address, from: "Resources")
    }
}

/// The SF Symbol for a Resource's kind.
func symbol(forKind kind: String) -> String {
    switch kind {
    case "image": "photo"
    case "document": "doc.text"
    case "sheet": "tablecells"
    case "link": "link"
    case "change": "plus.forwardslash.minus"
    case "error": "exclamationmark.triangle"
    case "output": "apple.terminal"
    case "file": "chevron.left.forwardslash.chevron.right"
    case "slides": "rectangle.on.rectangle"
    default: "text.alignleft"
    }
}

struct ResourceRow: View {
    var resource: Resource
    var thumbnail: NSImage?
    /// A link's host, shown beneath its title.
    var host: String? = nil
    var opened: Bool
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Group {
                    if let thumbnail {
                        Image(nsImage: thumbnail).resizable().aspectRatio(contentMode: .fill)
                    } else {
                        Image(systemName: symbol(forKind: resource.kind)).fontWeight(Brand.iconWeight).imageScale(.large)
                            .font(Brand.Style.body.font)
                            .foregroundStyle(Brand.secondary)
                    }
                }
                .frame(width: 30, height: 30)
                .background(opened ? Brand.onSelected : Brand.surface)
                .clipShape(Rectangle())
                VStack(alignment: .leading, spacing: 0) {
                    Text(resource.title)
                        .brandStyle(.answer)
                        .foregroundStyle(opened ? Brand.onSelected : Brand.text)
                        .lineLimit(1)
                    if let host {
                        Text(host).brandStyle(.secondary).foregroundStyle(opened ? Brand.onSelected : Brand.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
                if resource.isLink {
                    Image(systemName: "arrow.up.right").fontWeight(Brand.iconWeight).imageScale(.medium).foregroundStyle(Brand.secondary)
                }
            }
            .padding(.vertical, 7)
            .padding(.leading, 7)
            .padding(.trailing, 10)
            .background(opened ? Brand.selected : hover ? Brand.rowHover : Brand.row, in: Rectangle())
            .contentShape(Rectangle())
        }
        .buttonStyle(FocusedPlain())
        .hoverHighlight($hover)
        .accessibilityLabel("\(resource.title), \(resource.kind)")
        .accessibilityHint(host.map { "Opens \($0)" } ?? "")
    }
}
