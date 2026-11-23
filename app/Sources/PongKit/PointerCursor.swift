import AppKit

/// A view that names the cursor shown while the pointer is over it.
@MainActor public protocol PointerCursorOwner: NSView {
    /// The cursor at `point`, in window coordinates; nil for the arrow.
    func pointerCursor(at point: NSPoint) -> NSCursor?
}

extension NSWindow {
    /// The cursor for `point`, in window coordinates: the one named by the topmost view there, or
    /// by the nearest view around it that names one, else the arrow. Only one view answers for a
    /// spot, so two views that overlap never show different cursors there.
    public func pointerCursor(at point: NSPoint) -> NSCursor {
        var v = contentView?.hitTest(point)
        while let view = v {
            if let owner = view as? PointerCursorOwner { return owner.pointerCursor(at: point) ?? .arrow }
            v = view.superview
        }
        return .arrow
    }

    /// Shows the cursor for where the pointer is now. Views call this from every pointer event,
    /// exits included, and from `cursorUpdate`, so an exit reported while the pointer is still
    /// inside (as rebuilding tracking areas can) keeps the cursor, and AppKit's own cursor
    /// updates once the window is key give the same answer as the views' own.
    /// A view holding a drag sets its drag cursor itself and does not call this until it lets go.
    public func showPointerCursor() {
        pointerCursor(at: mouseLocationOutsideOfEventStream).set()
    }
}
