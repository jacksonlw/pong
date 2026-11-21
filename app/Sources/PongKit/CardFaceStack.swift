import SwiftUI

/// What the card's window shows: the card, the Bar, the Threads list or the start panel (`CardFaces`).
///
/// Between the card and the Bar (or the list) the window's face changes as the card's content
/// does: the outgoing content fades out over a surface held at its height, the surface eases to
/// the incoming content's height, the incoming content fades in from a moment into that ease, and
/// then the held surface fades away under the content's own. On the Bar, the surface eases to the
/// content's height at every change, as the card's box does between questions: the Bar opening
/// into its list or the start panel, one changing into the other, the start panel growing.
public struct CardFaceStack<Edges: View>: View {
    var inbox: Inbox
    var stage: CardStage
    var faces: CardFaces
    var directory: Directory
    var voice: VoiceSession?
    /// The window keeps its bottom edge fixed (a bottom corner), and its right one (a right corner).
    var bottom: Bool
    var trailing: Bool
    var width: CGFloat
    var moved: () -> Void
    var putBack: () -> Void
    var openThread: (SessionThread.SessionRef) -> Void
    var pointing: (Bool) -> Void
    var settled: () -> Void
    /// The start panel, while the Bar is open into it.
    var start: StartPanelView?
    var newSession: () -> Void
    var settings: () -> Void
    var edges: Edges

    public init(inbox: Inbox, stage: CardStage, faces: CardFaces, directory: Directory, voice: VoiceSession? = nil,
                bottom: Bool = false, trailing: Bool = false, width: CGFloat = cardWidth,
                moved: @escaping () -> Void = {}, putBack: @escaping () -> Void = {},
                openThread: @escaping (SessionThread.SessionRef) -> Void = { _ in }, pointing: @escaping (Bool) -> Void = { _ in },
                settled: @escaping () -> Void = {}, start: StartPanelView? = nil,
                newSession: @escaping () -> Void = {}, settings: @escaping () -> Void = {}, edges: Edges) {
        self.inbox = inbox
        self.stage = stage
        self.faces = faces
        self.directory = directory
        self.voice = voice
        self.bottom = bottom
        self.trailing = trailing
        self.width = width
        self.moved = moved
        self.putBack = putBack
        self.openThread = openThread
        self.pointing = pointing
        self.settled = settled
        self.start = start
        self.newSession = newSession
        self.settings = settings
        self.edges = edges
    }

    @State private var shown: CardFace = .none
    /// How many of the newest Threads the list shows; Show more adds a page.
    @State private var listed = ThreadRow.page
    @State private var visible = true
    /// The surface held under the faces while one becomes another; nil at rest.
    @State private var box: CGFloat?
    @State private var boxOpacity = 1.0
    /// In a bottom corner, how far above the window's bottom the held surface starts: the card's
    /// question sits above its Resources, and the surface eases down from there.
    @State private var boxInset: CGFloat = 0
    @State private var faceHeight: CGFloat = 0
    /// The question card's height and the whole card's, kept as its content leaves.
    @State private var cardHeight: CGFloat = 0
    @State private var cardFull: CGFloat = 0
    @State private var morphing: Task<Void, Never>?
    /// The morph has set its box to the incoming content's height: a later change of that
    /// height (the start panel's Projects loading) eases the box on to it.
    @State private var easing = false
    /// The Bar's surface height as drawn: it eases to the Bar's content at every change, as the
    /// card's box does between questions. Nil on the card, which eases its own.
    @State private var follow: CGFloat?
    /// The held Ping's button is on the Bar. When the card is asked for, it stays there and fades
    /// out with the rest of the Bar, until the card takes the Bar's place.
    @State private var keepsHeld = false
    /// The whole face's scale and its tip about its middle, top edge away: as the human sticks
    /// the Threads list open it lands like a sheet of paper on a desk, dipping and rocking to
    /// rest, and it rocks less as they unstick it.
    @State private var press: CGFloat = 1
    @State private var tip = 0.0
    /// How strongly the stuck Bar's edge is drawn: it comes in as the Bar lands and goes as it lifts.
    @State private var edge = 0.0
    private static var stuckEdge: Double { 0.6 }
    @State private var pressing: Task<Void, Never>?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private func isBar(_ f: CardFace) -> Bool { f == .bar || f == .threads || f == .start }

    private var vertical: Alignment { bottom ? .bottom : .top }
    /// While the card and the Bar change places in a top corner, the mark is drawn here, still.
    /// In a bottom corner the card's top bar and the Bar are at opposite ends, so it fades with them.
    private var holdsMark: Bool { !bottom && (box != nil || cardLeft) }
    /// The card's last content has faded out and the Bar is about to take its place: its surface
    /// stays, so the box the card leaves is the one that eases into the Bar.
    private var cardLeft: Bool { shown == .card && stage.shown == nil }

    private var surface: (height: CGFloat, inset: CGFloat)? {
        if let box { return (box, boxInset) }
        if cardLeft, cardHeight > 0 { return (cardHeight, bottom ? max(cardFull - cardHeight, 0) : 0) }
        if let follow { return (follow, 0) }
        return nil
    }

    public var body: some View {
        ZStack(alignment: vertical) {
            if let s = surface {
                Rectangle().fill(Brand.surface)
                    .frame(width: width, height: s.height)
                    .padding(.bottom, s.inset)
                    .opacity(boxOpacity)
            }
            face
                .opacity(visible ? 1 : 0)
                .environment(\.cardMarkHeld, holdsMark)
                .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { h in
                    faceHeight = h
                    if shown == .card, stage.shown != nil, h > 0 { cardFull = h }
                    measured(h)
                }
            if isBar(shown), let s = surface {
                // Stuck open, the Bar wears Pong's one outline around its surface, softened. The mask
                // cuts the outline's halo off at the window's edge, which leaves its edge.
                Color.clear
                    .frame(width: width, height: s.height)
                    .brandFocus(true)
                    .padding(.bottom, s.inset)
                    .opacity(boxOpacity * edge)
                    .allowsHitTesting(false)
            }
            if holdsMark {
                // The card's top bar and the Bar start in the same place, so the mark stays put
                // while everything around it fades.
                DragGrip()
                    .environment(\.cardMarkHeld, false)
                    .padding(DragGrip.inset)
                    .frame(width: width, alignment: .topLeading)
            }
        }
        .mask(alignment: vertical) {
            if let s = surface { Rectangle().frame(height: s.height).padding(.bottom, s.inset) } else { Rectangle() }
        }
        .environment(\.chosenCardWidth, width)
        .fixedSize()
        .rotation3DEffect(.degrees(tip), axis: (x: 1, y: 0, z: 0), perspective: 0.3)
        .scaleEffect(press)
        .overlay { if shown == .card { edges } }
        .onChange(of: faces.stuck) { _, now in pressed(deep: now) }
        .onAppear { edge = faces.stuck ? Self.stuckEdge : 0 }
        .onChange(of: stage.height) { _, h in if let h, h > 0 { cardHeight = h } }
        .onChange(of: faces.held, initial: true) { _, now in if now { keepsHeld = true } else if faces.face != .card { keepsHeld = false } }
        .onChange(of: faces.face, initial: true) { _, f in
            if f == .bar { listed = ThreadRow.page } // the list opens again at its first page
            change(to: f)
        }
    }

    @ViewBuilder private var face: some View {
        switch shown {
        case .card:
            InboxCard(inbox: inbox, stage: stage, voice: voice, anchorsBottom: bottom, moved: moved, putBack: putBack)
        case .bar, .threads, .start:
            let rows = ThreadRow.recent(directory, waiting: inbox.waiting, count: listed)
            BarView(figures: BarFigures(directory: directory, asking: inbox.waiting, waiting: inbox.hidden ? inbox.waitingCount : 0),
                    threads: rows, openThreads: faces.openThreads,
                    more: directory.sessions.count > rows.count ? { listed += ThreadRow.page } : nil,
                    open: shown == .threads ? .threads : shown == .start ? .start : nil, stuck: faces.stuck, start: start,
                    held: held, anchorsBottom: bottom,
                    actions: .init(openThread: openThread, openHeld: { faces.openHeld() },
                                   unhide: { if inbox.unhide() { faces.openCard() } }, pointing: pointing, moved: moved, putBack: putBack,
                                   stick: { faces.set(stuck: !faces.stuck) },
                                   newSession: newSession, settings: settings))
        case .none:
            EmptyView()
        }
    }

    private var held: (question: String, from: String?)? {
        guard faces.held || (keepsHeld && faces.face == .card && isBar(shown)), let ping = stage.shown?.ping else { return nil }
        return (ping.question, directory.card(for: ping).project?.name)
    }

    /// The face's content changed height. On the Bar, the surface eases to it; in a morph whose
    /// box already eases to the incoming content, the box eases on to the new height.
    private func measured(_ h: CGFloat) {
        guard h > 0 else { return }
        if morphing != nil {
            if easing, box != h { withAnimation(CardMotion.easeOut(stage.timing.box)) { box = h } }
            return
        }
        guard isBar(shown) else { return }
        if follow == nil || reduceMotion { follow = h; return }
        if follow != h { withAnimation(CardMotion.easeOut(stage.timing.box)) { follow = h } }
    }

    /// The landing: a quick dip and tip, then the dip settles while the tip rocks to rest.
    private func pressed(deep: Bool) {
        pressing?.cancel()
        guard !reduceMotion else { edge = deep ? Self.stuckEdge : 0; return }
        withAnimation(.easeInOut(duration: deep ? 0.7 : 0.25)) { edge = deep ? Self.stuckEdge : 0 }
        withAnimation(.easeOut(duration: 0.12)) {
            press = deep ? 0.955 : 0.985
            tip = deep ? 12 : -4
        }
        pressing = Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(120))
            guard !Task.isCancelled else { return }
            withAnimation(.spring(response: 0.4, dampingFraction: 0.85)) { press = 1 }
            withAnimation(.spring(response: 0.55, dampingFraction: 0.4)) { tip = 0 }
            try? await Task.sleep(for: .milliseconds(1100))
            guard !Task.isCancelled else { return }
            settled() // the window's shadow is drawn again around the face at rest
        }
    }

    private func change(to target: CardFace) {
        morphing?.cancel()
        morphing = nil
        easing = false
        let from = shown
        let isCard = { (f: CardFace) in f == .card }
        if from == .none || target == .none || reduceMotion || isCard(from) == isCard(target) {
            // Between the Bar's faces the surface keeps its height and eases to the new content's.
            if !isBar(target) || reduceMotion { follow = nil }
            (shown, visible, box, boxOpacity, boxInset, keepsHeld) = (target, true, nil, 1, 0, false)
            settled()
            return
        }
        let leftCard = cardLeft
        // The surface starts where the outgoing content's is: the card's question box, or the Bar.
        box = box ?? follow ?? (leftCard ? cardHeight : faceHeight)
        follow = nil
        boxInset = leftCard && bottom ? max(cardFull - cardHeight, 0) : boxInset
        boxOpacity = 1
        let t = stage.timing
        morphing = Task { @MainActor in
            if visible && !leftCard {
                withAnimation(CardMotion.ease(t.fadeOut)) { visible = false }
                try? await Task.sleep(for: t.fadeOut)
            } else {
                visible = false
            }
            guard !Task.isCancelled else { return }
            shown = target
            keepsHeld = false
            try? await Task.sleep(for: .milliseconds(16)) // the incoming content is laid out
            guard !Task.isCancelled else { return }
            withAnimation(CardMotion.easeOut(t.box)) {
                box = faceHeight
                boxInset = 0
            }
            easing = true
            try? await Task.sleep(for: t.settle)
            guard !Task.isCancelled else { return }
            withAnimation(CardMotion.ease(t.fadeIn)) { visible = true }
            try? await Task.sleep(for: max(t.box - t.settle, t.fadeIn))
            guard !Task.isCancelled else { return }
            // The content's own surfaces now cover the held one, which fades from the gaps
            // between a card's parts.
            withAnimation(CardMotion.ease(t.fadeIn)) { boxOpacity = 0 }
            try? await Task.sleep(for: t.fadeIn)
            guard !Task.isCancelled else { return }
            // On the Bar the surface stays, at the content's height, to ease its next change.
            if isBar(shown) { follow = faceHeight }
            (box, boxOpacity, boxInset, easing) = (nil, 1, 0, false)
            morphing = nil
            settled()
        }
    }
}

