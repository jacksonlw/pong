import AppKit
import Observation
import SwiftUI

/// What the pointer rests on in the card, for the context shown beside it.
@MainActor @Observable
public final class ContextHover {
    public typealias Topic = ContextTopic

    public struct Target: Equatable, Sendable {
        public var ping: Ping
        public var topic: Topic
    }

    /// Set while the pointer rests on the question, an answer or the ring.
    public var target: Target?
    /// The top edge, in screen coordinates, of what the pointer rests on, where the context starts.
    /// Nil starts it at the card's top.
    public var top: CGFloat?
    /// Set while the pointer rests on the context beside the card, which keeps it open.
    public var overPanel = false

    nonisolated public init() {}
}

/// The Directory and hover state the card's views read. The app uses `live`; tests inject their own.
public struct CardContext {
    public var directory: Directory
    public var hover: ContextHover

    public init(directory: Directory, hover: ContextHover) {
        self.directory = directory
        self.hover = hover
    }

    public static let live = CardContext(directory: Directory(), hover: ContextHover())
}

private struct CardContextKey: EnvironmentKey {
    static let defaultValue = CardContext.live
}

extension EnvironmentValues {
    public var cardContext: CardContext {
        get { self[CardContextKey.self] }
        set { self[CardContextKey.self] = newValue }
    }
}

extension View {
    /// Reports the pointer resting on this view as `topic` of `ping`, even while Pong is inactive.
    /// `topInset` is how far the part the human sees starts below this view's top edge.
    public func contextHover(_ topic: ContextHover.Topic, ping: Ping, topInset: CGFloat = 0) -> some View {
        modifier(ContextHoverHook(target: .init(ping: ping, topic: topic), topInset: topInset))
    }
}

private struct ContextHoverHook: ViewModifier {
    var target: ContextHover.Target
    var topInset: CGFloat
    @Environment(\.cardContext) private var ctx

    func body(content: Content) -> some View {
        let hover = ctx.hover
        content.background(HoverTracker(entered: { hover.top = $0.maxY - topInset }) { inside in
            if inside { hover.target = target } else if hover.target == target { hover.target = nil }
        })
    }
}

/// Pointer enter and exit from a tracking area with `.activeAlways`, which fires while the app
/// is inactive and the card is not key. Takes no clicks.
/// With a cursor, it shows that cursor while the pointer is inside; it sets it on every move,
/// since an inactive app gets no `cursorUpdate`.
public struct HoverTracker: NSViewRepresentable {
    var cursor: NSCursor?
    var entered: ((NSRect) -> Void)?
    var onChange: (Bool) -> Void

    /// `entered` gets the view's frame on screen as the pointer comes in, before `onChange`.
    public init(cursor: NSCursor? = nil, entered: ((NSRect) -> Void)? = nil, onChange: @escaping (Bool) -> Void = { _ in }) {
        self.cursor = cursor
        self.entered = entered
        self.onChange = onChange
    }

    public func makeNSView(context: Context) -> TrackingView {
        let v = TrackingView()
        updateNSView(v, context: context)
        return v
    }

    public func updateNSView(_ v: TrackingView, context: Context) {
        v.onChange = onChange
        v.entered = entered
        v.cursor = cursor
    }

    public final class TrackingView: NSView {
        var onChange: (Bool) -> Void = { _ in }
        var entered: ((NSRect) -> Void)?
        var cursor: NSCursor?

        override public func updateTrackingAreas() {
            super.updateTrackingAreas()
            trackingAreas.forEach(removeTrackingArea)
            addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .mouseMoved, .activeAlways, .inVisibleRect],
                                           owner: self))
        }

        override public func mouseEntered(with event: NSEvent) {
            cursor?.set()
            if let entered, let window { entered(window.convertToScreen(convert(bounds, to: nil))) }
            onChange(true)
        }
        override public func mouseMoved(with event: NSEvent) { cursor?.set() }
        override public func mouseExited(with event: NSEvent) {
            if cursor != nil { NSCursor.arrow.set() }
            onChange(false)
        }
        override public func hitTest(_ point: NSPoint) -> NSView? { nil }
        override public var acceptsFirstResponder: Bool { false }
    }
}

extension View {
    /// Tracks the pointer over something clickable, even while Pong is inactive, and eases its
    /// hover look in and out (at once with Reduce Motion). Every clickable control uses it.
    func hoverHighlight(_ hover: Binding<Bool>) -> some View {
        modifier(HoverHighlight(hover: hover))
    }

    /// Shows `cursor` while the pointer is over this view, even while Pong is inactive.
    func hoverCursor(_ cursor: NSCursor) -> some View {
        background(HoverTracker(cursor: cursor))
    }
}

/// How long a hover look takes to ease in or out.
let hoverEase = Animation.easeOut(duration: 0.12)

private struct HoverHighlight: ViewModifier {
    @Binding var hover: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.background(HoverTracker { inside in
            withAnimation(reduceMotion ? nil : hoverEase) { hover = inside }
        })
    }
}
