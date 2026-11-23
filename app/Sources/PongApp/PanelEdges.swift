import AppKit
import PongKit

/// Where on a panel's border the pointer is: one side, or a corner where two meet.
struct PanelHandle: Equatable {
    var left = false, right = false, top = false, bottom = false

    static let all: [PanelHandle] = [
        .init(left: true), .init(right: true), .init(top: true), .init(bottom: true),
        .init(left: true, top: true), .init(right: true, top: true),
        .init(left: true, bottom: true), .init(right: true, bottom: true),
    ]

    var position: NSCursor.FrameResizePosition {
        switch (left, right, top, bottom) {
        case (true, _, true, _): .topLeft
        case (_, true, true, _): .topRight
        case (true, _, _, true): .bottomLeft
        case (_, true, _, true): .bottomRight
        case (true, _, _, _): .left
        case (_, true, _, _): .right
        case (_, _, true, _): .top
        default: .bottom
        }
    }

    /// The window's frame once the pointer has moved `by` since it was pressed on `from`, never
    /// smaller than `minimum`: the edges this handle holds move, the others stay put.
    func frame(_ from: NSRect, by d: CGSize, minimum: CGSize) -> NSRect {
        var f = from
        if right { f.size.width = max(minimum.width, from.width + d.width) }
        if left {
            f.size.width = max(minimum.width, from.width - d.width)
            f.origin.x = from.maxX - f.width
        }
        if top { f.size.height = max(minimum.height, from.height + d.height) }
        if bottom {
            f.size.height = max(minimum.height, from.height - d.height)
            f.origin.y = from.maxY - f.height
        }
        return f.integral
    }
}

/// The strips along every side and corner of a panel beside the card, as on the card: the
/// system's resize cursor and a line along the edge while the pointer rests on it or drags it,
/// and a drag resizes the panel from there. Tracking areas set the cursor, since cursor rects
/// only work in the key window.
final class PanelEdges: NSView, PointerCursorOwner {
    static let thickness: CGFloat = 6
    static let corner: CGFloat = 14

    /// Told when a drag starts (true) and ends (false).
    var resizing: (Bool) -> Void = { _ in }
    var minimum = CGSize(width: 320, height: 240)

    private var lines: [NSView] = []
    private var hovered: PanelHandle?
    private var pressed: (handle: PanelHandle, mouse: NSPoint, frame: NSRect)?

    override init(frame: NSRect) {
        super.init(frame: frame)
        autoresizingMask = [.width, .height]
        for _ in 0..<4 {
            let l = NSView()
            l.wantsLayer = true
            l.alphaValue = 0
            addSubview(l)
            lines.append(l)
        }
    }

    required init?(coder: NSCoder) { fatalError("not used") }

    override var isFlipped: Bool { false }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
    override var mouseDownCanMoveWindow: Bool { false }

    override func layout() {
        super.layout()
        let b = bounds
        lines[0].frame = NSRect(x: 0, y: 0, width: 2, height: b.height)
        lines[1].frame = NSRect(x: b.maxX - 2, y: 0, width: 2, height: b.height)
        lines[2].frame = NSRect(x: 0, y: b.maxY - 2, width: b.width, height: 2)
        lines[3].frame = NSRect(x: 0, y: 0, width: b.width, height: 2)
    }

    /// The handle under a point in this view, if it is on the border.
    private func handle(at p: NSPoint) -> PanelHandle? {
        let b = bounds, t = Self.thickness, c = Self.corner
        guard b.contains(p) else { return nil }
        let nearLeft = p.x < t, nearRight = p.x > b.maxX - t, nearBottom = p.y < t, nearTop = p.y > b.maxY - t
        guard nearLeft || nearRight || nearTop || nearBottom else { return nil }
        // Near a corner along either edge, the corner takes it.
        let left = p.x < c, right = p.x > b.maxX - c, bottom = p.y < c, top = p.y > b.maxY - c
        if (nearLeft || nearRight) && (top || bottom) || (nearTop || nearBottom) && (left || right) {
            return PanelHandle(left: left, right: right, top: top, bottom: bottom)
        }
        return PanelHandle(left: nearLeft, right: nearRight, top: nearTop, bottom: nearBottom)
    }

    /// Only the border takes the mouse; everything inside goes to the panel's content.
    override func hitTest(_ point: NSPoint) -> NSView? {
        guard let superview else { return nil }
        return handle(at: convert(point, from: superview)) == nil ? nil : self
    }

    private func show(_ h: PanelHandle?) {
        effectiveAppearance.performAsCurrentDrawingAppearance {
            for l in lines { l.layer?.backgroundColor = NSColor(Brand.tagSkyOutline).cgColor }
        }
        let on = [h?.left, h?.right, h?.top, h?.bottom].map { $0 == true }
        NSAnimationContext.runAnimationGroup { c in
            c.duration = NSWorkspace.shared.accessibilityDisplayShouldReduceMotion ? 0 : 0.12
            for (l, on) in zip(lines, on) { l.animator().alphaValue = on ? 1 : 0 }
        }
    }

    private func cursor(_ h: PanelHandle) -> NSCursor { NSCursor.frameResize(position: h.position, directions: .all) }
    private func point(_ h: PanelHandle) { cursor(h).set() }

    func pointerCursor(at point: NSPoint) -> NSCursor? {
        handle(at: convert(point, from: nil)).map(cursor)
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .mouseMoved, .cursorUpdate, .activeAlways, .inVisibleRect],
                                       owner: self))
    }

    /// Lights the edge the pointer is on, and shows the window's cursor for where it is.
    /// Only the cursor of a pointer that was on an edge is set here: elsewhere, what is under the
    /// pointer shows its own.
    private func track(_ e: NSEvent) {
        guard pressed == nil else { return }
        let h = handle(at: convert(e.locationInWindow, from: nil))
        if h != nil || hovered != nil { window?.showPointerCursor() }
        guard h != hovered else { return }
        hovered = h
        show(h)
    }

    override func mouseEntered(with e: NSEvent) { track(e) }
    override func mouseMoved(with e: NSEvent) { track(e) }
    override func mouseExited(with e: NSEvent) { track(e) }
    /// The tracking area covers the whole panel; inside the border, the content's own cursor stands.
    override func cursorUpdate(with e: NSEvent) {
        guard pressed == nil else { return }
        if handle(at: convert(e.locationInWindow, from: nil)) != nil { window?.showPointerCursor() } else { super.cursorUpdate(with: e) }
    }

    override func mouseDown(with e: NSEvent) {
        guard let w = window, let h = handle(at: convert(e.locationInWindow, from: nil)) else { return }
        pressed = (h, NSEvent.mouseLocation, w.frame)
        point(h)
        resizing(true)
    }

    override func mouseDragged(with e: NSEvent) {
        guard let p = pressed, let w = window else { return }
        point(p.handle)
        let m = NSEvent.mouseLocation
        let d = CGSize(width: m.x - p.mouse.x, height: m.y - p.mouse.y)
        w.setFrame(p.handle.frame(p.frame, by: d, minimum: minimum), display: true)
    }

    override func mouseUp(with e: NSEvent) {
        guard pressed != nil else { return }
        pressed = nil
        hovered = nil
        track(e)
        window?.showPointerCursor()
        resizing(false)
    }
}
