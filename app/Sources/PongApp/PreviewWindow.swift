import AppKit
import SwiftUI
import PongKit

/// Shows each Resource opened on the card in a preview panel in the row beside the card. Docked
/// previews close when the card moves to another Ping; a detached one stays until closed. When
/// the human closes the card, every preview closes with it.
@MainActor
final class PreviewController {
    private let inbox: Inbox
    private let store = ResourceStore.shared
    private let panels: PreviewPanels
    private var shownPing: String?

    init(inbox: Inbox, dock: Dock, client: @escaping @Sendable () async -> EngineClient?) {
        self.inbox = inbox
        store.fetch = { resourceID, pingID in
            guard let c = await client() else { throw CancellationError() }
            return try await c.resource(resourceID, of: pingID)
        }
        panels = PreviewPanels(store: store, dock: dock, prefix: "resource:")
        track()
    }

    private func track() {
        withObservationTracking {
            _ = inbox.current?.id
            _ = inbox.hidden
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
    }
}

/// Keeps one preview panel in the row for each Resource open in `store`, the card's or a
/// Thread's alike, so an attachment opens the same way wherever it is clicked.
@MainActor
final class PreviewPanels {
    private let store: ResourceStore
    private let dock: Dock
    /// Keeps these panels' places in the row apart from other stores' previews of the same Resource.
    private let prefix: String
    private var panels: [String: (panel: DockPanel, host: ZoomedHostingView<ResourcePreview?>)] = [:]

    init(store: ResourceStore, dock: Dock, prefix: String) {
        self.store = store
        self.dock = dock
        self.prefix = prefix
        store.onOpen = { [weak self] _, r in
            self?.update()
            self?.panels[r.id]?.panel.orderFrontRegardless() // already open: bring it forward
        }
        track()
    }

    /// Closes every preview at once, for a store that goes away with its Thread.
    func closeAll() {
        store.closeAll()
        update()
    }

    private func key(_ id: String) -> String { prefix + id }

    private func track() {
        withObservationTracking {
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
        for id in panels.keys where !store.opened.contains(where: { $0.id == id }) {
            panels[id] = nil
            dock.remove(key(id))
        }
        for r in store.opened {
            let key = key(r.id)
            let (p, host) = panels[r.id] ?? {
                let p = DockPanel.make(size: previewSize)
                let host = Zoom.shared.host(ResourcePreview?.none)
                host.sizingOptions = [] // the human sizes the panel by its edges
                p.host(host, minimum: CGSize(width: 280, height: 220))
                panels[r.id] = (p, host)
                print("preview shows \(r.id)")
                return (p, host)
            }()
            host.content = ResourcePreview(
                resource: r, data: store.contents[r.id],
                moved: { [dock] in dock.dropped(key) },
                putBack: { [dock] in dock.putBack(key) },
                onClose: { [store] in store.close(r) })
            if !dock.isOpen(key) {
                dock.show(key, p, apart: store.isDetached(r), closed: { [store] in store.close(r) },
                          detached: { [store] apart in apart ? store.detach(r) : store.dock(r) })
            }
        }
    }
}
