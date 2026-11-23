import AppKit
import SwiftUI

/// The reply box, on the card and in a Thread: a row-coloured block that grows with its text up
/// to five lines, then scrolls inside. Return sends; Shift-Return starts a new line, and the
/// next item in a list. The pencil is part of the placeholder: the caret starts on it, and it
/// goes once there is text.
///
/// It is a text view of its own, not a field: a field's text is only scrollable while it is
/// being edited, so a long reply jumped back to its top and stopped taking the wheel whenever
/// the box lost focus. A text view keeps its place and scrolls either way.
struct ReplyField: View {
    /// The text as shown, a list with its bullets.
    @Binding var text: String
    var placeholder: String
    var enabled: Bool
    var typing: FocusState<Bool>.Binding
    /// Escape in the box; true when the caller used it.
    var escape: () -> Bool = { false }
    var send: () -> Void
    @State private var hover = false
    @State private var editing = false

    static let lines = 1...5
    static let inset = NSSize(width: 12, height: 10)

    var body: some View {
        ReplyText(text: $text, enabled: enabled, focused: typing.wrappedValue,
                  focus: { now in
                      editing = now
                      typing.wrappedValue = now
                  },
                  send: send, escape: escape)
            .focused(typing)
            .overlay(alignment: .topLeading) {
                Color.clear
                    .brandPlaceholder(placeholder, symbol: "pencil.line", shown: text.isEmpty)
                    .brandStyle(.body)
                    .padding(.vertical, Self.inset.height)
                    .padding(.horizontal, Self.inset.width)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
            .accessibilityLabel(placeholder)
            .frame(minHeight: 40)
            .background(hover && enabled && !editing ? Brand.rowHover : Brand.row, in: Rectangle())
            .hoverHighlight($hover)
            .brandFocus(editing)
    }
}

/// The text view inside its scroll view. The box's padding is the text view's own inset, so a
/// click anywhere in the box puts the caret in the text.
private struct ReplyText: NSViewRepresentable {
    @Binding var text: String
    var enabled: Bool
    var focused: Bool
    var focus: (Bool) -> Void
    var send: () -> Void
    var escape: () -> Bool

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = NSScrollView()
        scroll.drawsBackground = false
        scroll.borderType = .noBorder
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.hasHorizontalScroller = false
        let tv = Editor()
        tv.drawsBackground = false
        tv.isRichText = false
        tv.importsGraphics = false
        tv.allowsUndo = true
        tv.font = Brand.Style.bodyNSFont
        tv.textColor = NSColor(Brand.text)
        tv.insertionPointColor = NSColor(Brand.text)
        tv.typingAttributes = [.font: Brand.Style.bodyNSFont, .foregroundColor: NSColor(Brand.text)]
        tv.textContainerInset = ReplyField.inset
        tv.textContainer?.lineFragmentPadding = 0
        tv.textContainer?.widthTracksTextView = true
        tv.isVerticallyResizable = true
        tv.isHorizontallyResizable = false
        tv.autoresizingMask = [.width]
        tv.minSize = .zero
        tv.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: .greatestFiniteMagnitude)
        tv.string = text
        tv.delegate = context.coordinator
        tv.focusChanged = { [weak coordinator = context.coordinator] now in
            DispatchQueue.main.async { coordinator?.parent.focus(now) }
        }
        scroll.documentView = tv
        return scroll
    }

    func updateNSView(_ scroll: NSScrollView, context: Context) {
        context.coordinator.parent = self
        guard let tv = scroll.documentView as? Editor else { return }
        tv.isEditable = enabled
        tv.isSelectable = enabled
        tv.textColor = NSColor(enabled ? Brand.text : Brand.secondary)
        if tv.string != text, !tv.hasMarkedText() {
            // Changed from outside, such as a dash turned into a bullet or a draft from another
            // Device: the caret stays where it was, as far as the new text reaches.
            let at = tv.selectedRange()
            tv.string = text
            let n = (text as NSString).length
            tv.setSelectedRange(NSRange(location: min(at.location, n), length: min(at.length, n - min(at.location, n))))
            tv.scrollRangeToVisible(tv.selectedRange())
        }
        if focused, enabled, let w = tv.window, w.firstResponder !== tv {
            DispatchQueue.main.async { w.makeFirstResponder(tv) }
        }
    }

    /// As wide as offered, and as tall as the lines the text takes, from one to five.
    func sizeThatFits(_ proposal: ProposedViewSize, nsView scroll: NSScrollView, context: Context) -> CGSize? {
        guard let tv = scroll.documentView as? NSTextView, let layout = tv.layoutManager, let container = tv.textContainer,
              let font = tv.font else { return nil }
        let width = proposal.width ?? ThreadView.width
        guard width.isFinite, width > 0 else { return nil }
        container.size = NSSize(width: max(1, width - 2 * ReplyField.inset.width), height: .greatestFiniteMagnitude)
        layout.ensureLayout(for: container)
        let line = layout.defaultLineHeight(for: font)
        let used = layout.usedRect(for: container).height
        let lines = min(ReplyField.lines.upperBound, max(ReplyField.lines.lowerBound, Int((used / line).rounded())))
        return CGSize(width: width, height: CGFloat(lines) * line + 2 * ReplyField.inset.height)
    }

    @MainActor
    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: ReplyText
        init(_ parent: ReplyText) { self.parent = parent }

        func textDidChange(_ n: Notification) {
            guard let tv = n.object as? NSTextView else { return }
            parent.text = tv.string
            tv.scrollRangeToVisible(tv.selectedRange())
        }

        func textView(_ tv: NSTextView, doCommandBy sel: Selector) -> Bool {
            switch sel {
            case #selector(NSResponder.insertNewline(_:)):
                if (tv as? Editor)?.keyFlags.contains(.shift) == true {
                    if parent.enabled { newLine(tv) }
                } else {
                    parent.send()
                }
                return true
            case #selector(NSResponder.cancelOperation(_:)):
                // Escape is the caller's, then the window's; never the text view's completion list.
                if !parent.escape() { _ = tv.nextResponder?.tryToPerform(#selector(NSResponder.cancelOperation(_:)), with: nil) }
                return true
            case #selector(NSResponder.insertTab(_:)):
                tv.window?.selectNextKeyView(nil)
                return true
            case #selector(NSResponder.insertBacktab(_:)):
                tv.window?.selectPreviousKeyView(nil)
                return true
            default:
                return false
            }
        }

        /// Shift-Return: a new line, or the next item in a list.
        private func newLine(_ tv: NSTextView) {
            let s = tv.string
            let cursor = Range(tv.selectedRange(), in: s) ?? s.endIndex..<s.endIndex
            let (new, at) = ListText.newLine(in: s, at: cursor)
            tv.string = new
            tv.setSelectedRange(NSRange(at..<at, in: new))
            parent.text = new
            tv.scrollRangeToVisible(tv.selectedRange())
        }
    }
}

/// The reply's text view: a click on it makes its window key, even on the card, which is never
/// key by itself, and the pointer shows the text cursor over it even while Pong is inactive.
private final class Editor: NSTextView, PointerCursorOwner {
    var focusChanged: (Bool) -> Void = { _ in }
    /// The modifiers held for the key being handled, which tells Return from Shift-Return.
    private(set) var keyFlags: NSEvent.ModifierFlags = []

    override func keyDown(with e: NSEvent) {
        keyFlags = e.modifierFlags
        defer { keyFlags = [] }
        super.keyDown(with: e)
    }

    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
    override var needsPanelToBecomeKey: Bool { true }

    override func becomeFirstResponder() -> Bool {
        let ok = super.becomeFirstResponder()
        if ok { focusChanged(true) }
        return ok
    }

    override func resignFirstResponder() -> Bool {
        let ok = super.resignFirstResponder()
        if ok { focusChanged(false) }
        return ok
    }

    override func mouseDown(with e: NSEvent) {
        if isEditable { window?.makeKey() }
        super.mouseDown(with: e)
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        for a in trackingAreas where a.owner === self && a.userInfo?["pointer"] != nil { removeTrackingArea(a) }
        addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .mouseMoved, .activeAlways, .inVisibleRect],
                                       owner: self, userInfo: ["pointer": true]))
    }

    /// Text that can be edited or selected shows the text cursor.
    func pointerCursor(at point: NSPoint) -> NSCursor? { isSelectable ? .iBeam : nil }

    override func mouseEntered(with e: NSEvent) { window?.showPointerCursor() }
    override func mouseMoved(with e: NSEvent) {
        super.mouseMoved(with: e)
        window?.showPointerCursor()
    }
    override func mouseExited(with e: NSEvent) { window?.showPointerCursor() }
}
