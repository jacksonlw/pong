import AppKit
import PongKit

/// Every window setting for a panel beside the card: the card's own floating panel, which never
/// activates Pong, but one that a click makes key, so ⌘W and Escape reach it.
enum DockPanelConfiguration {
    static func apply(to p: NSPanel) {
        CardPanelConfiguration.apply(to: p)
        p.becomesKeyOnlyIfNeeded = false
    }
}

/// A Thread, Settings or Resource preview's window. Not a CardPanel, so the card is the only one
/// of those. ⌘W, ⌃W and Escape close it, as a browser closes a tab.
final class DockPanel: NSPanel {
    var onClose: () -> Void = {}
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
    override func cancelOperation(_ sender: Any?) { onClose() }

    override func performKeyEquivalent(with e: NSEvent) -> Bool {
        let keys = e.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if e.charactersIgnoringModifiers?.lowercased() == "w", keys == .command || keys == .control {
            onClose()
            return true
        }
        return super.performKeyEquivalent(with: e)
    }

    static func make(size: CGSize) -> DockPanel {
        let p = DockPanel(contentRect: NSRect(origin: .zero, size: size), styleMask: CardPanelConfiguration.style,
                          backing: .buffered, defer: false)
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
        for g in gone { close(g) }
        if apart { placeApart(panel) }
        layout(eased: true, arriving: apart ? nil : key)
        print("dock shows \(key)")
    }

    /// The owner closed its panel.
    func remove(_ key: String) {
        guard let p = panels.removeValue(forKey: key) else { return }
        let hadKey = p.isKeyWindow
        row.close(key)
        closers[key] = nil
        detaching[key] = nil
        laidOut[key] = nil
        p.orderOut(nil)
        // Typing goes back to the app the human was in.
        if hadKey, NSApp.isActive { NSApp.deactivate() }
        layout(eased: true)
        print("dock closed \(key)")
    }

    /// The human closed the panel by key, or the row replaced it: its owner closes it.
    private func close(_ key: String) {
        closers[key]?()
        remove(key)
    }

    /// Lays the row out again for the card moving or changing size, or a docked panel changing size.
    private func windowChanged(_ w: NSWindow?, resized: Bool) {
        if w is CardPanel { return layout() }
        guard resized, let key = panels.first(where: { $0.value === w })?.key, row.docked.contains(key),
              laidOut[key] != w?.frame.size else { return }
        layout(eased: true)
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
    private func layout(eased: Bool = false, arriving: String? = nil) {
        let keys = row.docked
        guard let card else {
            for k in keys { panels[k]?.orderOut(nil) }
            return
        }
        for (k, f) in zip(keys, frames(keys, beside: card)) {
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
            if abs(p.frame.minX - home.minX) <= reach, abs(p.frame.minY - home.minY) <= reach { return layout(eased: true) }
        }
        // The other docked panels close up the row, and this one rests against what is near.
        let rest = row.docked.filter { $0 != key }
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
            detaching[key]?(false)
            print("dock docked \(key)")
        } else {
            row.detach(key)
            detaching[key]?(true)
            move(p, to: at, eased: true)
            print("dock detached \(key)")
        }
        layout(eased: true)
    }

    /// The human double-clicked the panel's title: it docks at the end of the row.
    func putBack(_ key: String) {
        guard row.contains(key) else { return }
        row.dockAtEnd(key)
        detaching[key]?(false)
        layout(eased: true)
    }
}
