import AppKit
import SwiftUI

/// The strip along the top of a panel beside the card: what it is, its title, and the close
/// control. Dragging the strip moves the panel, and a double-click puts it back in the row.
public struct PanelTitle: View {
    var symbol: String
    var title: String
    var closeLabel: String
    /// The human let go of the panel after dragging it; nil where the panel has no window of its own.
    var moved: (() -> Void)?
    var putBack: () -> Void
    var onClose: () -> Void

    public init(symbol: String, title: String, closeLabel: String, moved: (() -> Void)?, putBack: @escaping () -> Void,
                onClose: @escaping () -> Void) {
        self.symbol = symbol
        self.title = title
        self.closeLabel = closeLabel
        self.moved = moved
        self.putBack = putBack
        self.onClose = onClose
    }

    public var body: some View {
        HStack(spacing: 8) {
            // The mouse goes through the title to the area behind it, which moves the window.
            Group {
                Image(systemName: symbol).imageScale(.large).foregroundStyle(Brand.secondary)
                Text(title).brandStyle(.answer).foregroundStyle(Brand.text).lineLimit(1)
            }
            .allowsHitTesting(false)
            Spacer(minLength: 0)
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
        .help(moved == nil ? "" : "Drag to move. Double-click to put it back.")
    }
}

/// Whether a click opens a panel beside the docked ones instead of in place of them: Command,
/// Control or Shift held.
public func opensBeside(_ keys: NSEvent.ModifierFlags) -> Bool {
    !keys.intersection([.command, .control, .shift]).isEmpty
}
