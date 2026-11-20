import AppKit
import SwiftUI
import PongKit

/// Every window setting for a Resource preview, next to the card's: the same non-activating
/// floating panel, so opening a preview never activates Pong or takes keyboard focus; clicking
/// into it (to select text) makes it key.
enum PreviewPanelConfiguration {
    /// Gap between the preview and the card it sits beside.
    static let gap: CGFloat = 10

    static func apply(to p: NSPanel) {
        CardPanelConfiguration.apply(to: p)
    }
}

/// A preview's window. Not a CardPanel, so the card is the only one of those.
final class PreviewPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

/// Shows each Resource opened on the card in a preview window. Docked previews sit in a row
/// beside the card, follow it as it moves or changes size, and close when the card moves to
/// another Ping. A preview the human drags away is detached: it comes to rest beside whichever
/// of Pong's windows is near and stays until closed. Let go at the end of the row, it is docked again.
/// When the human closes the card, every preview closes with it.
@MainActor
final class PreviewController {
    private let inbox: Inbox
    private let store = ResourceStore.shared
    private var panels: [String: PreviewPanel] = [:]
    private var shownPing: String?
    /// The detached previews that have a place of their own on the screen already.
    private var apart: Set<String> = []
    /// How long a preview takes to ease to where it rests.
    private static let ease: TimeInterval = 0.18

    init(inbox: Inbox, client: @escaping @Sendable () async -> EngineClient?) {
        self.inbox = inbox
        store.fetch = { resourceID, pingID in
            guard let c = await client() else { throw CancellationError() }
            return try await c.resource(resourceID, of: pingID)
        }
        store.onOpen = { [weak self] _, r in
            self?.update()
            self?.panels[r.id]?.orderFrontRegardless() // already open: bring it forward
        }
        for name in [NSWindow.didResizeNotification, NSWindow.didMoveNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] n in
                MainActor.assumeIsolated {
                    if n.object is CardPanel { self?.layout() }
                }
            }
        }
        track()
    }

    private var card: NSWindow? { NSApp.windows.first { $0 is CardPanel && $0.isVisible } }

    private func track() {
        withObservationTracking {
            _ = inbox.current?.id
            _ = inbox.hidden
            _ = store.opened
            _ = store.detached
            _ = store.opened.map { store.contents[$0.id] }
        } onChange: { [weak self] in
            DispatchQueue.main.async {
                self?.update()
                self?.track()
            }
        }
    }

    private func update() {
        if inbox.hidden { store.closeAll() } // the human closed the card: its previews go with it
        if inbox.current?.id != shownPing {
            shownPing = inbox.current?.id
            store.closeDocked()
        }
        for (id, p) in panels where !store.opened.contains(where: { $0.id == id }) {
            p.orderOut(nil)
            panels[id] = nil
        }
        for r in store.opened {
            let p = panels[r.id] ?? {
                let p = PreviewPanel(contentRect: NSRect(origin: .zero, size: previewSize),
                                     styleMask: CardPanelConfiguration.style, backing: .buffered, defer: false)
                PreviewPanelConfiguration.apply(to: p)
                p.contentView = FirstMouseHostingView<ResourcePreview?>(rootView: nil)
                panels[r.id] = p
                print("preview shows \(r.id)")
                return p
            }()
            (p.contentView as? FirstMouseHostingView<ResourcePreview?>)?.rootView = ResourcePreview(
                resource: r, data: store.contents[r.id],
                moved: { [weak self] in self?.dropped(r) },
                putBack: { [weak self] in self?.store.dock(r); self?.layout(eased: true) },
                onClose: { [store] in store.close(r) })
        }
        // A preview opened in its own window starts a step away from the end of the row.
        for (i, id) in store.detached.subtracting(apart).enumerated() {
            guard let p = panels[id] else { continue }
            guard let card else { p.center(); continue }
            let step = CGFloat(i + 1) * 24
            move(p, to: slot(store.docked.count, beside: card).offsetBy(dx: -step, dy: -step), eased: p.isVisible)
        }
        apart = store.detached
        layout(eased: true)
    }

    /// Where the docked preview at `i` sits: in a row away from the card (left of it, or right
    /// when it sits at the screen's left edge), top edges level, kept on the card's screen. Past
    /// the screen's edge, later previews lie over earlier ones.
    private func slot(_ i: Int, beside card: NSWindow) -> NSRect {
        let screen = (card.screen ?? NSScreen.main ?? NSScreen.screens[0]).visibleFrame
        let step = previewSize.width + PreviewPanelConfiguration.gap
        let left = card.frame.minX - step >= screen.minX
        var x = left ? card.frame.minX - CGFloat(i + 1) * step : card.frame.maxX + PreviewPanelConfiguration.gap + CGFloat(i) * step
        x = max(screen.minX, min(x, screen.maxX - previewSize.width))
        let y = max(screen.minY, card.frame.maxY - previewSize.height)
        return NSRect(origin: CGPoint(x: x, y: y), size: previewSize)
    }

    /// Puts the docked previews in their row beside the card and shows every preview. Without a
    /// card the docked ones hide. Only CardController places the card itself.
    private func layout(eased: Bool = false) {
        let card = card
        for (i, r) in store.docked.enumerated() {
            guard let p = panels[r.id] else { continue }
            guard let card else { p.orderOut(nil); continue }
            if p.isVisible { move(p, to: slot(i, beside: card), eased: eased) } else { p.setFrame(slot(i, beside: card), display: true) }
            p.orderFrontRegardless()
        }
        for id in store.detached { panels[id]?.orderFrontRegardless() }
    }

    private func move(_ p: NSPanel, to frame: NSRect, eased: Bool) {
        guard eased, p.frame != frame, !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion else {
            return p.setFrame(frame, display: true)
        }
        NSAnimationContext.runAnimationGroup { c in
            c.duration = Self.ease
            c.timingFunction = CAMediaTimingFunction(name: .easeOut)
            p.animator().setFrame(frame, display: true)
        }
    }

    /// The human let go of a preview they dragged by its title.
    private func dropped(_ r: Resource) {
        guard let p = panels[r.id] else { return }
        let reach = PreviewSnap.reach
        // Barely moved from its place in the row: it goes back.
        if let card, let i = store.docked.firstIndex(where: { $0.id == r.id }) {
            let home = slot(i, beside: card)
            if abs(p.frame.minX - home.minX) <= reach, abs(p.frame.minY - home.minY) <= reach { return layout(eased: true) }
        }
        // The other docked previews close up the row, and this one rests against what is near.
        let row = store.docked.filter { $0.id != r.id }
        var others = store.detached.filter { $0 != r.id }.compactMap { panels[$0]?.frame }
        var end: NSRect?
        if let card {
            others += [card.frame] + row.indices.map { slot($0, beside: card) }
            end = slot(row.count, beside: card)
        }
        let rest = PreviewSnap.rest(p.frame, beside: others, gap: PreviewPanelConfiguration.gap)
        if let end, abs(rest.minX - end.minX) < 1, abs(rest.minY - end.minY) < 1 {
            store.dock(r)
            print("preview docked \(r.id)")
        } else {
            apart.insert(r.id)
            store.detach(r)
            move(p, to: rest, eased: true)
            print("preview detached \(r.id)")
        }
        layout(eased: true)
    }
}
