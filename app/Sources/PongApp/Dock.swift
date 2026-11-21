import AppKit
import PongKit

/// Every window setting for a panel beside the card: the card's own floating panel, which never
/// activates Pong, but one that a click makes key, so ⌘W reaches it.
enum DockPanelConfiguration {
    static func apply(to p: NSPanel) {
        CardPanelConfiguration.apply(to: p)
        p.becomesKeyOnlyIfNeeded = false
    }
}

/// A Thread, Settings or Resource preview's window. Not a CardPanel, so the card is the only one
/// of those. ⌘W, ⌃W and its close control close it, as a browser closes a tab; Escape does not.
/// Its edges and corners resize it.
final class DockPanel: NSPanel {
    var onClose: () -> Void = {}
    /// Told when the human starts (true) and stops (false) resizing it by an edge.
    var onResizing: (Bool) -> Void = { _ in }
    private(set) var isResizing = false
    private let edges = PanelEdges()

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
    /// Escape belongs to what has focus inside the panel, never to closing it.
    override func cancelOperation(_ sender: Any?) {}

    override func performKeyEquivalent(with e: NSEvent) -> Bool {
        let keys = e.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if e.charactersIgnoringModifiers?.lowercased() == "w", keys == .command || keys == .control {
            onClose()
            return true
        }
        return super.performKeyEquivalent(with: e)
    }

    /// Shows `content` filling the panel, under the edges that resize it, no smaller than `minimum`.
    func host(_ content: NSView, minimum: CGSize) {
        let box = NSView(frame: NSRect(origin: .zero, size: frame.size))
        let zoomed = ZoomBox(content)
        zoomed.frame = box.bounds
        zoomed.autoresizingMask = [.width, .height]
        box.addSubview(zoomed)
        edges.frame = box.bounds
        edges.minimum = minimum
        edges.resizing = { [weak self] on in
            guard let self else { return }
            self.isResizing = on
            self.onResizing(on)
        }
        box.addSubview(edges)
        contentView = box
    }

    static func make(size: CGSize) -> DockPanel {
        let z = Zoom.shared.scale
        let p = DockPanel(contentRect: NSRect(origin: .zero, size: CGSize(width: size.width * z, height: size.height * z)),
                          styleMask: CardPanelConfiguration.style, backing: .buffered, defer: false)
        DockPanelConfiguration.apply(to: p)
        return p
    }
}

/// The row of panels beside the card's window: Threads, Settings and Resource previews, the
/// newest nearest the card. A new one eases in from the card's side while the others ease
/// outward. A panel the human drags away is detached: it comes to rest beside whichever of
/// Pong's windows is near and stays until closed. Let go at the end of the row, or double-clicked
/// by its title, it is docked again at the end. With no card on screen the docked ones hide.
@MainActor
final class Dock {
    static let gap: CGFloat = 10
    /// How long a panel takes to ease to where it rests.
    private static let ease: TimeInterval = 0.18
    /// How far from its place a new panel starts, towards the card.
    private static let arrival: CGFloat = 24

    private var row = DockRow()
    private var panels: [String: DockPanel] = [:]
    /// What each owner does when the row closes its panel: replaced, or closed by key.
    private var closers: [String: () -> Void] = [:]
    /// Tells each owner when its panel is detached (true) or docked again (false).
    private var detaching: [String: (Bool) -> Void] = [:]
    /// The size each docked panel was laid out at, so only a real change of size lays out again.
    private var laidOut: [String: CGSize] = [:]

    init() {
        for name in [NSWindow.didResizeNotification, NSWindow.didMoveNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] n in
                MainActor.assumeIsolated { self?.windowChanged(n.object as? NSWindow, resized: name == NSWindow.didResizeNotification) }
            }
        }
        NotificationCenter.default.addObserver(forName: Zoom.changed, object: nil, queue: .main) { [weak self] n in
            let was = n.userInfo?["was"] as? CGFloat ?? 1
            MainActor.assumeIsolated { self?.zoomed(by: Zoom.shared.scale / was) }
        }
    }

    /// Pong zoomed in or out: each panel grows or shrinks with it, keeping its top-left corner.
    private func zoomed(by ratio: CGFloat) {
        for p in panels.values {
            let f = p.frame
            let s = CGSize(width: (f.width * ratio).rounded(), height: (f.height * ratio).rounded())
            p.setFrame(NSRect(x: f.minX, y: f.maxY - s.height, width: s.width, height: s.height), display: true)
        }
        layout(eased: true)
    }

    private var card: NSWindow? { NSApp.windows.first { $0 is CardPanel && $0.isVisible } }

    func isOpen(_ key: String) -> Bool { row.contains(key) }

    /// Shows a panel in the row, nearest the card, or a step away from the row's end when
    /// `apart`. One already open comes forward and keeps its place. The docked panels `replacing`
    /// picks close first.
    func show(_ key: String, _ panel: DockPanel, apart: Bool = false, replacing: (String) -> Bool = { _ in false },
              closed: @escaping () -> Void, detached: @escaping (Bool) -> Void = { _ in }) {
        if row.contains(key) {
            panels[key]?.orderFrontRegardless()
            return
        }
        let gone = row.open(key, apart: apart, replacing: replacing)
        panels[key] = panel
        closers[key] = closed
        detaching[key] = detached
        panel.onClose = { [weak self] in self?.close(key) }
        // Let go of an edge, a docked panel takes its place in the row again at its new size.
        panel.onResizing = { [weak self] on in if !on, self?.row.docked.contains(key) == true { self?.layout(eased: true) } }
        for g in gone { close(g) }
        if apart { placeApart(panel) }
        layout(eased: true, arriving: apart ? nil : key)
        print("dock shows \(key)")
    }

    /// The owner closed its panel.
    func remove(_ key: String) {
        guard let p = panels.removeValue(forKey: key) else { return }
        let hadKey = p.isKeyWindow
        let next = row.close(key)
        unattach(key)
        closers[key] = nil
        detaching[key] = nil
        laidOut[key] = nil
        p.orderOut(nil)
        // Focus stays in Pong, so ⌘W after ⌘W closes the next panel and never reaches the app
        // behind: the next panel takes it, else the card, where ⌘W does nothing. With no card on
        // screen, typing goes back to the app the human was in.
        if hadKey, NSApp.isActive {
            if let next, let n = panels[next] { n.makeKey() } else if let card { card.makeKey() } else { NSApp.deactivate() }
        }
        layout(eased: true)
        print("dock closed \(key)")
    }

    /// The human closed the panel by key, or the row replaced it: its owner closes it.
    private func close(_ key: String) {
        closers[key]?()
        remove(key)
    }

    /// The card's frame when it last moved or changed size.
    private var cardFrame: CGRect?

    /// Lays the row out again for the card moving or changing size, or a docked panel changing
    /// size. While the human drags a docked panel's edge, the others make room for it at once.
    private func windowChanged(_ w: NSWindow?, resized: Bool) {
        if let w, w is CardPanel {
            carryAttached(card: w.frame)
            return layout()
        }
        guard resized, let key = panels.first(where: { $0.value === w })?.key, row.docked.contains(key),
              laidOut[key] != w?.frame.size else { return }
        if (w as? DockPanel)?.isResizing == true { return layout(except: key) }
        layout(eased: true)
    }

    /// The detached panels joined to the card: each snapped into place against it, the docked
    /// panels or another joined panel. They move with the card as one body.
    private var attached: Set<String> = []

    /// The card moved without changing size, as when the human drags it: the panels joined to it
    /// move with it. Panels it passes on the way are left where they are.
    private func carryAttached(card now: CGRect) {
        defer { cardFrame = now }
        guard let was = cardFrame, was.size == now.size, was.origin != now.origin else { return }
        let dx = now.minX - was.minX, dy = now.minY - was.minY
        for k in attached {
            guard let p = panels[k] else { continue }
            p.setFrameOrigin(NSPoint(x: p.frame.minX + dx, y: p.frame.minY + dy))
        }
    }

    /// The card's body: the card and its docked row.
    private func body(_ card: NSWindow) -> [CGRect] { [card.frame] + row.docked.compactMap { panels[$0]?.frame } }

    /// Joins `key` to the card when it rests against the body, and keeps only the joined panels
    /// still reached from the body, so one moved away no longer holds others on.
    private func rejoin(_ key: String?, at f: CGRect? = nil) {
        guard let card else { return attached = [] }
        var frames = Dictionary(uniqueKeysWithValues: attached.compactMap { k in panels[k].map { (k, $0.frame) } })
        if let key { frames[key] = f ?? panels[key]?.frame }
        let now = PreviewSnap.attached(frames.compactMapValues { $0 }, to: body(card), gap: Self.gap)
        if now != attached { print("dock attached \(now.sorted())") }
        attached = now
    }

    private func unattach(_ key: String) {
        guard attached.remove(key) != nil else { return }
        rejoin(nil)
    }

    /// The human let go of the card after dragging it. Near a detached panel, it eases into place
    /// against it, and the panels it then rests against join it.
    func cardDropped(_ card: NSWindow, settled: @escaping () -> Void) {
        let loose = row.detached.subtracting(attached).compactMap { panels[$0]?.frame }
        let at = PreviewSnap.rest(card.frame, beside: loose, gap: Self.gap)
        let join = { [weak self] in
            guard let self else { return settled() }
            for k in self.row.detached where !self.attached.contains(k) {
                if let f = self.panels[k]?.frame, PreviewSnap.touching(f, card.frame, gap: Self.gap) { self.rejoin(k) }
            }
            settled()
        }
        guard at != card.frame else { return join() }
        guard !reduceMotion else {
            card.setFrame(at, display: true)
            return join()
        }
        NSAnimationContext.runAnimationGroup({ c in
            c.duration = Self.ease
            c.timingFunction = CAMediaTimingFunction(name: .easeOut)
            card.animator().setFrame(at, display: true)
        }, completionHandler: { MainActor.assumeIsolated { join() } })
    }

    private func frames(_ keys: [String], beside card: NSWindow) -> [CGRect] {
        let screen = (card.screen ?? NSScreen.main ?? NSScreen.screens[0]).visibleFrame
        return DockRow.frames(keys.map { panels[$0]?.frame.size ?? .zero }, beside: card.frame, on: screen, gap: Self.gap)
    }

    /// A panel opened apart starts a step away from the end of the row.
    private func placeApart(_ p: DockPanel) {
        guard let card else { return p.center() }
        let others = row.docked.filter { panels[$0] !== p }
        let end = DockRow.frames(others.map { panels[$0]?.frame.size ?? .zero } + [p.frame.size], beside: card.frame,
                                 on: (card.screen ?? NSScreen.main ?? NSScreen.screens[0]).visibleFrame, gap: Self.gap).last!
        p.setFrame(end.offsetBy(dx: -Self.arrival, dy: -Self.arrival), display: true)
    }

    /// Puts the docked panels in their row beside the card and shows every panel.
    private func layout(eased: Bool = false, arriving: String? = nil, except: String? = nil) {
        let keys = row.docked
        guard let card else {
            for k in keys { panels[k]?.orderOut(nil) }
            return
        }
        for (k, f) in zip(keys, frames(keys, beside: card)) where k != except {
            guard let p = panels[k] else { continue }
            laidOut[k] = f.size
            if k == arriving {
                arrive(p, at: f, cardOnLeft: f.midX > card.frame.midX)
            } else if p.isVisible {
                move(p, to: f, eased: eased)
            } else {
                p.setFrame(f, display: true)
            }
            p.orderFrontRegardless()
        }
        for k in row.detached { panels[k]?.orderFrontRegardless() }
    }

    private var reduceMotion: Bool { NSWorkspace.shared.accessibilityDisplayShouldReduceMotion }

    private func move(_ p: NSPanel, to frame: NSRect, eased: Bool) {
        guard eased, p.frame != frame, !reduceMotion else { return p.setFrame(frame, display: true) }
        NSAnimationContext.runAnimationGroup { c in
            c.duration = Self.ease
            c.timingFunction = CAMediaTimingFunction(name: .easeOut)
            p.animator().setFrame(frame, display: true)
        }
    }

    /// A new panel eases in from the card's side as it fades in.
    private func arrive(_ p: NSPanel, at frame: NSRect, cardOnLeft: Bool) {
        guard !reduceMotion else {
            p.alphaValue = 1
            return p.setFrame(frame, display: true)
        }
        p.alphaValue = 0
        p.setFrame(frame.offsetBy(dx: cardOnLeft ? -Self.arrival : Self.arrival, dy: 0), display: true)
        p.orderFrontRegardless()
        NSAnimationContext.runAnimationGroup { c in
            c.duration = Self.ease
            c.timingFunction = CAMediaTimingFunction(name: .easeOut)
            p.animator().setFrame(frame, display: true)
            p.animator().alphaValue = 1
        }
    }

    /// The human let go of a panel they dragged by its title.
    func dropped(_ key: String) {
        guard let p = panels[key] else { return }
        let reach = PreviewSnap.reach
        // Barely moved from its place in the row: it goes back.
        if let card, let i = row.docked.firstIndex(of: key) {
            let home = frames(row.docked, beside: card)[i]
            if abs(p.frame.minX - home.minX) <= reach, abs(p.frame.minY - home.minY) <= reach {
                unattach(key)
                return layout(eased: true)
            }
        }
        // The other docked panels close up the row, and this one rests against what is near.
        let rest = row.docked.filter { $0 != key }
        attached.remove(key)
        var others = row.detached.filter { $0 != key }.compactMap { panels[$0]?.frame }
        var end: CGRect?
        if let card {
            let fs = frames(rest + [key], beside: card)
            others += [card.frame] + fs.dropLast()
            end = fs.last
        }
        let at = PreviewSnap.rest(p.frame, beside: others, gap: Self.gap)
        if let end, abs(at.minX - end.minX) < 1, abs(at.minY - end.minY) < 1 {
            row.dockAtEnd(key)
            rejoin(nil)
            detaching[key]?(false)
            print("dock docked \(key)")
        } else {
            row.detach(key)
            detaching[key]?(true)
            move(p, to: at, eased: true)
            // Snapped into place against the card's body, it joins it; let go apart, it stands alone.
            rejoin(key, at: at)
            print("dock detached \(key)")
        }
        layout(eased: true)
    }

    /// The human double-clicked the panel's title: it docks at the end of the row.
    func putBack(_ key: String) {
        guard row.contains(key) else { return }
        row.dockAtEnd(key)
        unattach(key)
        detaching[key]?(false)
        layout(eased: true)
    }
}
