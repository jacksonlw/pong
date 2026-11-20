import AppKit
import SwiftUI

/// The reply box, on the card and in a Thread: a row-coloured block that grows with its text up
/// to five lines, then scrolls inside. Return sends; Shift-Return starts a new line, and the
/// next item in a list. The pencil is part of the placeholder: the caret starts on it, and it
/// goes once there is text.
struct ReplyField: View {
    /// The text as shown, a list with its bullets.
    @Binding var text: String
    var placeholder: String
    var enabled: Bool
    var typing: FocusState<Bool>.Binding
    var send: () -> Void
    @State private var selection: TextSelection?
    @State private var hover = false

    var body: some View {
        TextField("", text: $text, selection: $selection, axis: .vertical)
            .lineLimit(1...5)
            .brandFieldLines(text, 1...5)
            .brandPlaceholder(placeholder, symbol: "pencil.line", shown: text.isEmpty)
            .textFieldStyle(.plain)
            .disabled(!enabled)
            .brandStyle(.body)
            .foregroundStyle(Brand.text)
            .focused(typing)
            .padding(.vertical, 10)
            .padding(.horizontal, 12)
            .frame(minHeight: 40)
            // The whole box takes a click, not only its text, and shows the text cursor.
            .background { FieldClickArea(enabled: enabled) { typing.wrappedValue = true } }
            .background(hover && enabled && !typing.wrappedValue ? Brand.rowHover : Brand.row, in: Rectangle())
            .hoverHighlight($hover)
            .brandFocus(typing.wrappedValue)
            .onKeyPress(.return, phases: .down) { press in
                if press.modifiers.contains(.option) { return .ignored }
                if press.modifiers.contains(.shift) {
                    if enabled { newLine() }
                    return .handled
                }
                send()
                return .handled
            }
    }

    private func newLine() {
        var cursor = text.endIndex..<text.endIndex
        if case .selection(let r)? = selection?.indices, r.upperBound <= text.endIndex { cursor = r }
        let (new, at) = ListText.newLine(in: text, at: cursor)
        text = new
        selection = TextSelection(insertionPoint: at)
    }
}

/// The reply box's whole area: a click makes its window key and puts the caret in the field, and
/// the pointer shows the text cursor over it, even while Pong is inactive.
private struct FieldClickArea: NSViewRepresentable {
    var enabled: Bool
    var click: () -> Void

    func makeNSView(context: Context) -> Area { Area() }

    func updateNSView(_ v: Area, context: Context) {
        v.enabled = enabled
        v.click = click
    }

    final class Area: NSView {
        var enabled = true
        var click: () -> Void = {}

        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
        override var needsPanelToBecomeKey: Bool { true }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            trackingAreas.forEach(removeTrackingArea)
            addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .mouseMoved, .activeAlways, .inVisibleRect],
                                           owner: self))
        }

        override func mouseEntered(with e: NSEvent) { if enabled { NSCursor.iBeam.set() } }
        override func mouseMoved(with e: NSEvent) { if enabled { NSCursor.iBeam.set() } }
        override func mouseExited(with e: NSEvent) { NSCursor.arrow.set() }

        override func mouseDown(with e: NSEvent) {
            guard enabled else { return }
            window?.makeKey()
            DispatchQueue.main.async { [click] in click() }
        }
    }
}
