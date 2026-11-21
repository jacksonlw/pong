import AppKit
import SwiftUI

/// One of the actions in a panel's ⋮ menu.
public struct PanelAction {
    public var title: String
    public var symbol: String?
    /// An image of its own, such as a Harness's mark, in place of the symbol.
    public var image: NSImage?
    public var enabled: Bool
    public var action: () -> Void

    public init(_ title: String, symbol: String? = nil, image: NSImage? = nil, enabled: Bool = true,
                action: @escaping () -> Void = {}) {
        self.title = title
        self.symbol = symbol
        self.image = image
        self.enabled = enabled
        self.action = action
    }
}

/// The strip along the top of a panel beside the card: what it is, its title, the ⋮ menu of the
/// panel's actions when it has any, and the close control. Dragging the strip moves the panel,
/// and a double-click puts it back in the row.
public struct PanelTitle<Mark: View>: View {
    var mark: Mark
    var title: String
    var closeLabel: String
    var menu: [PanelAction]
    /// The human let go of the panel after dragging it; nil where the panel has no window of its own.
    var moved: (() -> Void)?
    var putBack: () -> Void
    var onClose: () -> Void

    public init(title: String, closeLabel: String, menu: [PanelAction] = [], moved: (() -> Void)?,
                putBack: @escaping () -> Void, onClose: @escaping () -> Void, @ViewBuilder mark: () -> Mark) {
        self.mark = mark()
        self.title = title
        self.closeLabel = closeLabel
        self.menu = menu
        self.moved = moved
        self.putBack = putBack
        self.onClose = onClose
    }

    public var body: some View {
        HStack(spacing: 8) {
            // The mouse goes through the title to the area behind it, which moves the window.
            Group {
                mark
                Text(title).brandStyle(.answer).foregroundStyle(Brand.text).lineLimit(1).truncationMode(.tail)
            }
            .allowsHitTesting(false)
            Spacer(minLength: 0)
            if !menu.isEmpty { PanelMenu(actions: menu) }
            HoverButton(action: onClose) { hover in
                Image(systemName: "xmark").imageScale(.large).font(Brand.Style.label.font).foregroundStyle(hover ? Brand.text : Brand.secondary)
                    .frame(width: 24, height: 24)
                    .background(hover ? Brand.row : .clear, in: Rectangle())
                    .contentShape(Rectangle())
            }
            .accessibilityLabel(closeLabel)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background { if let moved { WindowDragArea(moved: moved, putBack: putBack) } }
        .help(moved == nil ? title : "\(title)\nDrag to move. Double-click to put it back.")
    }
}

extension PanelTitle where Mark == PanelSymbol {
    public init(symbol: String, title: String, closeLabel: String, menu: [PanelAction] = [], moved: (() -> Void)?,
                putBack: @escaping () -> Void, onClose: @escaping () -> Void) {
        self.init(title: title, closeLabel: closeLabel, menu: menu, moved: moved, putBack: putBack, onClose: onClose) {
            PanelSymbol(name: symbol)
        }
    }
}

/// The symbol at the start of a panel's title strip.
public struct PanelSymbol: View {
    var name: String
    public var body: some View { Image(systemName: name).imageScale(.large).foregroundStyle(Brand.secondary) }
}

/// The ⋮ button left of a panel's close control, the same size, opening its actions.
struct PanelMenu: View {
    var actions: [PanelAction]
    @State private var hover = false

    var body: some View {
        Menu {
            ForEach(Array(actions.enumerated()), id: \.offset) { _, a in
                Button(action: a.action) {
                    if let image = a.image {
                        Label { Text(a.title) } icon: { Image(nsImage: image) }
                    } else if let symbol = a.symbol {
                        Label(a.title, systemImage: symbol)
                    } else {
                        Text(a.title)
                    }
                }
                .disabled(!a.enabled)
            }
        } label: {
            Image(systemName: "ellipsis").imageScale(.large).font(Brand.Style.label.font)
                .rotationEffect(.degrees(90))
                .foregroundStyle(hover ? Brand.text : Brand.secondary)
                .frame(width: 24, height: 24)
                .background(hover ? Brand.row : .clear, in: Rectangle())
                .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .hoverHighlight($hover)
        .help("More")
        .accessibilityLabel("More")
    }
}

/// Whether a click opens a panel beside the docked ones instead of in place of them: Command,
/// Control or Shift held.
public func opensBeside(_ keys: NSEvent.ModifierFlags) -> Bool {
    !keys.intersection([.command, .control, .shift]).isEmpty
}
