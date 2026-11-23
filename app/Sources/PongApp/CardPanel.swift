import AppKit
import SwiftUI
import PongKit

/// Every window setting for the card, in one place; change it here.
enum CardPanelConfiguration {
    static let style: NSWindow.StyleMask = [.nonactivatingPanel, .borderless]
    static let level: NSWindow.Level = .floating // raise to .statusBar if .floating stays below full screen
    static let behavior: NSWindow.CollectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
    /// Distance from the screen's corner the card sits in (chosen in Settings).
    static let margin: CGFloat = 12

    /// The card's frame for its size in the chosen corner of the screen's visible frame.
    static func frame(_ s: CGSize, in corner: CardCorner, of screen: NSRect) -> NSRect {
        NSRect(x: corner.isRight ? screen.maxX - s.width - margin : screen.minX + margin,
               y: corner.isTop ? screen.maxY - s.height - margin : screen.minY + margin,
               width: s.width, height: s.height)
    }

    static func apply(to p: NSPanel) {
        p.isFloatingPanel = true
        p.becomesKeyOnlyIfNeeded = true
        p.hidesOnDeactivate = false
        p.level = level
        p.collectionBehavior = behavior
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = true
        p.isReleasedWhenClosed = false
    }
}

/// Which edge of the card's window stays put as the card changes height, and the width the
/// human has dragged the card to.
@MainActor @Observable
final class CardAnchor {
    var bottom = false
    /// The window's right edge is the fixed one (a right corner), so content keeps to it while
    /// the window is wider than the content, as it is for a moment after it shrinks.
    var trailing = false
    var width = cardWidth
}

enum CardEdge: CaseIterable {
    case left, right, top, bottom

    var isSide: Bool { self == .left || self == .right }

    var alignment: Alignment {
        switch self {
        case .left: .leading
        case .right: .trailing
        case .top: .top
        case .bottom: .bottom
        }
    }
}

/// A thin strip along each edge of the card, or along the Bar's sides. Dragging a side changes
/// the window's width, and a double-click on any of them puts the standard size back.
struct CardEdges: View {
    /// A side and how far outwards the pointer has gone since it was pressed.
    var drag: (CardEdge, CGFloat) -> Void
    var ended: () -> Void
    var reset: () -> Void
    /// False on the Bar, the Threads list and the start panel: only the sides are strips there.
    var onCard = true
    static let thickness: CGFloat = 8

    var body: some View {
        ZStack {
            ForEach(onCard ? CardEdge.allCases : [.left, .right], id: \.self) { edge in
                EdgeArea(edge: edge, drag: { drag(edge, $0) }, ended: ended, reset: reset)
                    .frame(width: edge.isSide ? Self.thickness : nil, height: edge.isSide ? nil : Self.thickness)
                    .help(edge.isSide ? "Drag to resize. Double-click for the standard size." : "Double-click for the standard size.")
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: edge.alignment)
            }
        }
        .accessibilityHidden(true)
    }
}

/// One edge's strip. Like the grip, it sets its cursor from a tracking area, since cursor
/// rects only work in the key window, which the card rarely is. A side shows the system's
/// window-resize cursor and a line along it while the pointer rests on it or drags it. The line
/// is drawn here and not in SwiftUI, so showing it redraws nothing else and the cursor stays.
struct EdgeArea: NSViewRepresentable {
    var edge: CardEdge
    var drag: (CGFloat) -> Void
    var ended: () -> Void
    var reset: () -> Void

    func makeNSView(context: Context) -> Area { Area() }

    func updateNSView(_ v: Area, context: Context) {
        v.edge = edge
        v.drag = drag
        v.ended = ended
        v.reset = reset
    }

    final class Area: NSView, PointerCursorOwner {
        var edge = CardEdge.left
        var drag: (CGFloat) -> Void = { _ in }
        var ended: () -> Void = {}
        var reset: () -> Void = {}
        /// Where the pointer was pressed, across the screen.
        private var start: CGFloat?
        private var inside = false
        /// The line along a side, shown while the pointer rests on it or drags it.
        private let line = NSView()

        override init(frame: NSRect) {
            super.init(frame: frame)
            line.wantsLayer = true
            line.alphaValue = 0
            addSubview(line)
        }

        required init?(coder: NSCoder) { fatalError("not used") }

        override func layout() {
            super.layout()
            line.frame = NSRect(x: edge == .right ? bounds.maxX - 2 : 0, y: 0, width: 2, height: bounds.height)
        }

        override func hitTest(_ point: NSPoint) -> NSView? { super.hitTest(point) == nil ? nil : self }

        private func active(_ on: Bool) {
            guard edge.isSide else { return }
            effectiveAppearance.performAsCurrentDrawingAppearance {
                line.layer?.backgroundColor = NSColor(Brand.tagSkyOutline).cgColor
            }
            NSAnimationContext.runAnimationGroup { c in
                c.duration = NSWorkspace.shared.accessibilityDisplayShouldReduceMotion ? 0 : 0.12
                line.animator().alphaValue = on ? 1 : 0
            }
        }

        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
        override var mouseDownCanMoveWindow: Bool { false }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            trackingAreas.forEach(removeTrackingArea)
            addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .mouseMoved, .cursorUpdate, .activeAlways, .inVisibleRect],
                                           owner: self))
        }

        /// The cursor a resizable window shows on that side.
        private var resizeCursor: NSCursor? {
            edge.isSide ? NSCursor.frameResize(position: edge == .left ? .left : .right, directions: .all) : nil
        }

        func pointerCursor(at point: NSPoint) -> NSCursor? { resizeCursor }

        /// Shows the window's cursor for the pointer, unless a drag holds it.
        private func point() {
            if start == nil { window?.showPointerCursor() } else { resizeCursor?.set() }
        }

        override func mouseEntered(with e: NSEvent) {
            inside = true
            point()
            active(true)
        }
        override func mouseMoved(with e: NSEvent) { point() }
        override func cursorUpdate(with e: NSEvent) { point() }
        override func mouseExited(with e: NSEvent) {
            inside = false
            guard start == nil else { return }
            point()
            active(false)
        }

        override func mouseDown(with e: NSEvent) {
            if e.clickCount == 2 { start = nil; reset(); return }
            if edge.isSide { start = NSEvent.mouseLocation.x }
        }

        override func mouseDragged(with e: NSEvent) {
            guard let s = start else { return }
            point()
            let across = NSEvent.mouseLocation.x - s
            drag(edge == .right ? across : -across)
        }

        override func mouseUp(with e: NSEvent) {
            guard start != nil else { return }
            start = nil
            inside = bounds.contains(convert(e.locationInWindow, from: nil))
            point()
            if !inside { active(false) }
            ended()
        }
    }
}

/// The card, the Bar, the Threads list or the start panel in the window, pinned to its fixed
/// edges (`CardFaceStack`), with the edges that resize the card.
struct CardRoot: View {
    var inbox: Inbox
    var stage: CardStage
    var faces: CardFaces
    var directory: Directory
    var anchor: CardAnchor
    var voice: VoiceSession
    var moved: () -> Void
    var putBack: () -> Void
    var resize: (CGSize) -> Void
    var edges: CardEdges
    var openThread: (SessionThread.SessionRef) -> Void
    var pointing: (Bool) -> Void
    var settled: () -> Void
    var start: BarStart
    var settings: () -> Void

    var body: some View {
        CardFaceStack(inbox: inbox, stage: stage, faces: faces, directory: directory, voice: voice,
                      bottom: anchor.bottom, trailing: anchor.trailing, width: anchor.width,
                      moved: moved, putBack: putBack, openThread: openThread, pointing: pointing, settled: settled,
                      start: start.view, newSession: { start.toggle() }, settings: settings,
                      edges: { onCard in
                          var e = edges
                          e.onCard = onCard
                          return e
                      })
            .onGeometryChange(for: CGSize.self, of: { $0.size }) { resize($0) }
            .frame(maxWidth: .infinity, maxHeight: .infinity,
                   alignment: anchor.bottom ? (anchor.trailing ? .bottomTrailing : .bottomLeading)
                                            : (anchor.trailing ? .topTrailing : .topLeading))
            .environment(\.linkOpener, appLinkOpener)
    }
}

final class CardPanel: NSPanel, GrabbedWindow {
    /// Escape, while the window is key (the start panel is open).
    var cancel: () -> Void = {}
    /// The human is dragging the card by its bar (top-left corner), or let go (nil).
    var onGrab: (NSPoint?) -> Void = { _ in }
    func grabbed(at topLeft: NSPoint?) { onGrab(topLeft) }
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
    override func cancelOperation(_ sender: Any?) { cancel() }
}

/// Shows the Inbox's card in a screen corner while any Ping waits, and the Bar while none does,
/// keeping the panel the size of what it shows as that changes (answers, a growing reply box,
/// the typing badge below it, the Threads list, the start panel).
/// Changes go through a CardStage; the panel grows at once and shrinks once the card has
/// eased to its new height, so the easing is never cut off.
@MainActor
final class CardController {
    private let inbox: Inbox
    private let directory: Directory
    private let stage = CardStage()
    /// Whether the window shows the card, the Bar, the Threads list or the start panel.
    private let faces = CardFaces()
    /// The start panel the Bar opens into.
    private var start: BarStart!
    /// Settles the pointer's comings and goings over the Bar before they open or close the list.
    private var pointer: DispatchWorkItem?
    /// Voice mode: on-device hearing and the system voice.
    let voice: VoiceSession
    private let anchor = CardAnchor()
    private let panel: CardPanel
    private var size = CGSize(width: cardWidth, height: 100)
    private var shrink: DispatchWorkItem?
    /// Where the human dragged the card's top-left corner to; nil keeps it in its corner.
    private var dragged: NSPoint?
    /// The card's width and right edge when the human began dragging one of its sides.
    private var widening: (width: CGFloat, maxX: CGFloat)?
    private static let widthKey = "cardWidth"
    private static let stuckKey = "barStuck"

    init(inbox: Inbox, directory: Directory, client: @escaping @Sendable () async -> EngineClient?,
         settings: @escaping () -> Void, openThread: @escaping (SessionThread.SessionRef) -> Void) {
        self.inbox = inbox
        self.directory = directory
        let kept = UserDefaults.standard.double(forKey: Self.widthKey)
        if kept > 0 { anchor.width = min(max(kept, cardWidths.lowerBound), cardWidths.upperBound) }
        faces.set(stuck: UserDefaults.standard.bool(forKey: Self.stuckKey))
        panel = CardPanel(contentRect: NSRect(x: 0, y: 0, width: cardWidth, height: 100),
                          styleMask: CardPanelConfiguration.style, backing: .buffered, defer: false)
        CardPanelConfiguration.apply(to: panel)
        let voice = VoiceSession(io: Hearing.io)
        Hearing.speak.voice = voice
        self.voice = voice
        let faces = faces
        let start = BarStart(client: client, window: panel, changed: { faces.set(starting: $0) })
        self.start = start
        panel.cancel = { start.close() }
        panel.onGrab = { [weak self] in self?.grab($0) }
        let root = CardRoot(inbox: inbox, stage: stage, faces: faces, directory: directory, anchor: anchor, voice: voice,
                            moved: { [weak self] in self?.moved() }, putBack: { [weak self] in self?.putBack() },
                            resize: { [weak self] in self?.resize($0) },
                            edges: CardEdges(drag: { [weak self] in self?.widen($0, by: $1) },
                                             ended: { [weak self] in self?.keepWidth() },
                                             reset: { [weak self] in self?.standardSize() }),
                            openThread: openThread,
                            pointing: { [weak self] in self?.pointing($0) },
                            settled: { [weak self] in self?.panel.invalidateShadow() },
                            start: start, settings: settings)
        let host = Zoom.shared.host(root.environment(Hearing.speak).environment(SharedSkills.names))
        host.sizingOptions = [] // the panel's size is set here, from the card's
        panel.contentView = host
        track()
        trackStage()
        trackCorner()
        trackFace()
        update()
        zooming = NotificationCenter.default.addObserver(forName: Zoom.changed, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, self.panel.isVisible else { return }
                self.shrink?.cancel()
                self.shrink = nil
                self.place(self.size)
            }
        }
    }

    private var zooming: NSObjectProtocol?

    /// The window's size for content of size `s`, drawn at Pong's zoom.
    private func zoomed(_ s: CGSize) -> CGSize {
        let z = Zoom.shared.scale
        return CGSize(width: (s.width * z).rounded(), height: (s.height * z).rounded())
    }

    /// The Bar opens into the start panel; live checks send SIGUSR1 to press it.
    func newSession() { start.toggle() }

    /// The Threads open in panels beside the card: the list stays open while there are any.
    func set(openThreads: Set<SessionThread.SessionRef>) { faces.set(openThreads: openThreads) }

    private func trackFace() {
        let was = faces.face
        withObservationTracking {
            _ = faces.face
            _ = faces.stuck
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                guard let self else { return }
                let face = self.faces.face
                if UserDefaults.standard.bool(forKey: Self.stuckKey) != self.faces.stuck {
                    UserDefaults.standard.set(self.faces.stuck, forKey: Self.stuckKey)
                    print("bar \(self.faces.stuck ? "stuck open" : "unstuck")")
                }
                // The card shows over the start panel the human was writing in: the panel waits
                // aside and takes typing again when the card goes. Elsewhere the panel closes.
                if face == .start, was == .card { self.start.takeKey() }
                if face != .start, face != .card, self.start.isOpen { self.start.close() }
                if face == .threads, was == .card { self.closeThreadsAwayFromPointer() }
                self.update()
                self.trackFace()
            }
        }
    }

    /// Leaving the card went back to the Threads list, which closes as the pointer leaves it.
    /// The pointer may be elsewhere already, so once the window has eased to the list's height
    /// the list closes if the pointer is not on it.
    private func closeThreadsAwayFromPointer() {
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) { [weak self] in
            guard let self, self.faces.face == .threads,
                  !NSMouseInRect(NSEvent.mouseLocation, self.panel.frame, false) else { return }
            self.pointing(false)
        }
    }

    /// The pointer came onto the Bar or left it. The list opens once the pointer has rested a
    /// moment, so passing over the Bar opens nothing, and closes a moment after it leaves, so a
    /// brush past its edge does not close it.
    private func pointing(_ inside: Bool) {
        pointer?.cancel()
        let later = DispatchWorkItem { [weak self] in self?.faces.set(pointing: inside) }
        pointer = later
        DispatchQueue.main.asyncAfter(deadline: .now() + (inside ? 0.15 : 0.3), execute: later)
    }

    private func trackCorner() {
        withObservationTracking {
            _ = directory.corner
            _ = directory.followsActiveDisplay
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                guard let self else { return }
                self.dragged = nil // a new corner in Settings puts the card back in it
                if self.panel.isVisible { self.place(self.standing) }
                print("card corner \(self.directory.corner.rawValue), follows active display \(self.directory.followsActiveDisplay)")
                self.trackCorner()
            }
        }
    }

    private func track() {
        withObservationTracking {
            _ = inbox.scene
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                guard let self else { return }
                self.stage.set(self.inbox.scene)
                self.track()
            }
        }
    }

    private func trackStage() {
        withObservationTracking {
            _ = stage.shown?.key
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                guard let self else { return }
                if let key = self.stage.shown?.key { print("card shows \(key)") }
                self.faces.set(scene: self.stage.shown != nil)
                self.trackStage()
            }
        }
    }

    /// The human hid Pong from the menu bar: no window shows until they show it again.
    private(set) var hidden = false

    /// Shows Pong with the start panel open, for the Speak shortcut.
    func openStart() {
        if hidden { set(hidden: false) }
        Task { await start.open() }
    }

    func set(hidden: Bool) {
        self.hidden = hidden
        print("pong \(hidden ? "hidden" : "shown") from the menu bar")
        update()
    }

    private func update() {
        guard faces.face != .none, !hidden else {
            if panel.isVisible { print("card hidden") }
            panel.orderOut(nil)
            return
        }
        place(standing)
        panel.orderFrontRegardless()
        print("card face \(faces.face)")
    }

    /// The size to place the panel at now. While a shrink waits for the box's ease, that is
    /// the size the panel already has: the shorter one would cut the box off as it eases.
    private var standing: CGSize {
        let z = Zoom.shared.scale
        return panel.isVisible && shrink != nil ? CGSize(width: panel.frame.width / z, height: panel.frame.height / z) : size
    }

    private func resize(_ s: CGSize) {
        guard s.height > 0 else { return }
        size = s
        shrink?.cancel()
        shrink = nil
        let window = zoomed(s)
        if window.height >= panel.frame.height || NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
            place(s)
            return
        }
        // A new width shows at once; only the height waits for the box.
        if window.width != panel.frame.width { place(CGSize(width: s.width, height: standing.height)) }
        let later = DispatchWorkItem { [weak self] in
            self?.shrink = nil
            self?.place(s)
            self?.panel.invalidateShadow()
        }
        shrink = later
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3, execute: later) // after the box's 220 ms ease
    }

    /// Where the human let go of the card: Pong's other windows may pull it into place.
    var dropped: (NSWindow, @escaping () -> Void) -> Void = { _, settled in settled() }

    /// While the card is held, it stays where the pointer has it: a resize or an ease that lands
    /// mid-drag keeps its top edge there instead of putting it back in its corner.
    private func grab(_ topLeft: NSPoint?) {
        guard let topLeft else { return }
        if dragged == nil, let later = shrink {
            // Leaving the corner pins the top edge; a shrink still waiting would otherwise land
            // after the grab and pull the bottom up under the pointer.
            later.cancel()
            shrink = nil
            dragged = topLeft
            place(size)
            return
        }
        dragged = topLeft
        if anchor.bottom { anchor.bottom = false }
        if anchor.trailing { anchor.trailing = false }
    }

    private func moved() {
        dropped(panel) { [weak self] in
            guard let self else { return }
            self.dragged = NSPoint(x: self.panel.frame.minX, y: self.panel.frame.maxY)
            print("card moved to \(Int(self.panel.frame.minX)), \(Int(self.panel.frame.maxY))")
        }
    }

    /// The human is dragging a side of the card. In its corner the card stays there; dragged
    /// somewhere else, its other side stays put.
    private func widen(_ edge: CardEdge, by distance: CGFloat) {
        let from = widening ?? (anchor.width, panel.frame.maxX)
        widening = from
        let z = Zoom.shared.scale
        let w = min(max(from.width + distance / z, cardWidths.lowerBound), cardWidths.upperBound).rounded()
        if edge == .left, let d = dragged { dragged = NSPoint(x: from.maxX - w * z, y: d.y) }
        anchor.width = w
    }

    private func keepWidth() {
        widening = nil
        UserDefaults.standard.set(Double(anchor.width), forKey: Self.widthKey)
        print("card width \(Int(anchor.width))")
    }

    private func standardSize() {
        widening = nil
        anchor.width = cardWidth
        UserDefaults.standard.removeObject(forKey: Self.widthKey)
        print("card at its standard size")
    }

    private func putBack() {
        dragged = nil
        place(standing)
        print("card put back in its corner")
    }

    /// Keeps the panel where the human dragged it, top edge fixed as it changes height, or else
    /// in the chosen corner of the main display (the one with the menu bar),
    /// or of the display with the focused window when Settings say the card follows it.
    /// In a bottom corner the bottom edge is the fixed one, and the card is pinned to it.
    private func place(_ content: CGSize) {
        guard content.height > 0 else { return }
        let s = zoomed(content)
        let bottom = dragged == nil && !directory.corner.isTop
        if anchor.bottom != bottom { anchor.bottom = bottom }
        let trailing = dragged == nil && directory.corner.isRight
        if anchor.trailing != trailing { anchor.trailing = trailing }
        if let d = dragged {
            panel.setFrame(NSRect(x: d.x, y: d.y - s.height, width: s.width, height: s.height), display: true)
            return
        }
        let main = NSScreen.screens.first
        let screen = ((directory.followsActiveDisplay ? NSScreen.main : main) ?? main ?? NSScreen.main)?.visibleFrame ?? .zero
        panel.setFrame(CardPanelConfiguration.frame(s, in: directory.corner, of: screen), display: true)
    }
}
