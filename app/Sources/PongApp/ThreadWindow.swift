import AppKit
import PongKit
import SwiftUI

/// One panel per Thread in the row beside the card, opened from the Bar's Threads list, a Ping's
/// question or a Session link. A plain click puts it in place of the docked Thread; Command,
/// Control or Shift puts it beside. Each panel fetches its Thread and fetches it again on events
/// about it.
@MainActor
final class ThreadWindowController {
    private let client: @Sendable () async -> EngineClient?
    private let dock: Dock
    private var threads: [SessionThread.SessionRef: (panel: DockPanel, live: Task<Void, Never>, previews: PreviewPanels)] = [:]

    /// Brings the card to a Ping that waits.
    private let showInQueue: (Ping) -> Void
    /// Told which Threads are open each time one opens or closes.
    private let changed: (Set<SessionThread.SessionRef>) -> Void

    static let height: CGFloat = 640
    private static func key(_ ref: SessionThread.SessionRef) -> String { "thread:\(ref.harness)/\(ref.id)" }

    init(client: @escaping @Sendable () async -> EngineClient?, dock: Dock, showInQueue: @escaping (Ping) -> Void,
         changed: @escaping (Set<SessionThread.SessionRef>) -> Void) {
        self.client = client
        self.dock = dock
        self.showInQueue = showInQueue
        self.changed = changed
        ThreadOpening.open = { [weak self] ping in
            let harness = CardContext.live.directory.card(for: ping).session?.harness ?? ""
            self?.show(SessionThread.SessionRef(harness: harness, id: ping.session))
        }
        // The Session opens in its Harness; where Pong has no way in, its Thread says so.
        SessionOpening.open = { ping in
            let s = CardContext.live.directory.card(for: ping).session
            if SystemHarnessOpener().open(app: s?.appCommand, resume: s?.resumeCommand, startedBy: s?.startedBy,
                                         ended: s?.state == "ended") { return }
            ThreadOpening.open?(ping)
        }
    }

    func show(_ ref: SessionThread.SessionRef) {
        let beside = opensBeside(NSApp.currentEvent?.modifierFlags ?? [])
        let key = Self.key(ref)
        if let open = threads[ref] {
            open.panel.orderFrontRegardless()
            return
        }
        let client = self.client
        let sender = ThreadSending(
            pong: { id, pong in
                guard let c = await client() else { throw CancellationError() }
                try await c.pong(id, pong)
            },
            followUp: { ref, prompt in
                guard let c = await client() else { throw CancellationError() }
                try await c.followUp(ref, prompt: prompt)
            },
            stop: { ref in
                guard let c = await client() else { throw CancellationError() }
                try await c.stop(ref)
            })
        let model = ThreadModel(ref: ref, sender: sender) { ref in
            guard let c = await client() else { throw CancellationError() }
            return try await c.thread(ref)
        }
        let store = ResourceStore()
        store.fetch = { resourceID, pingID in
            guard let c = await client() else { throw CancellationError() }
            return try await c.resource(resourceID, of: pingID)
        }
        let opener = SystemHarnessOpener()
        let p = DockPanel.make(size: CGSize(width: ThreadView.width, height: Self.height))
        let dock = self.dock
        let root = ThreadWindowContent(model: model, store: store, opener: opener, actions: ThreadActions(
            openInHarness: { model.openInHarness(with: opener) },
            openSession: { [weak self] in self?.show($0) },
            showInQueue: showInQueue,
            stop: { Task { await model.stopTurn() } }),
            moved: { dock.dropped(key) }, putBack: { dock.putBack(key) }, close: { [weak self] in self?.close(ref) })
        let host = Zoom.shared.host(root)
        host.sizingOptions = [] // the human sizes the panel by its edges
        p.host(host, minimum: CGSize(width: 360, height: 320))
        let live = Task {
            await model.refresh()
            while !Task.isCancelled {
                guard let c = await client() else { try? await Task.sleep(for: .milliseconds(500)); continue }
                await model.refresh() // catch up on what changed while not listening
                do { for try await e in c.newEvents() { await model.handle(e) } } catch {}
                try? await Task.sleep(for: .milliseconds(500))
            }
        }
        // Its attachments open in preview panels in the row, as the card's do.
        threads[ref] = (p, live, PreviewPanels(store: store, dock: dock, prefix: "thread-resource:\(ref.harness)/\(ref.id):"))
        changed(Set(threads.keys))
        dock.show(key, p, replacing: beside ? { _ in false } : { $0.hasPrefix("thread:") },
                  closed: { [weak self] in self?.close(ref) })
        print("thread shown \(ref.id)")
    }

    private func close(_ ref: SessionThread.SessionRef) {
        guard let open = threads.removeValue(forKey: ref) else { return }
        open.live.cancel()
        open.previews.closeAll() // its previews go with it
        changed(Set(threads.keys))
        dock.remove(Self.key(ref))
    }
}

/// The Thread under its title strip, filling the panel at whatever size the human gives it.
private struct ThreadWindowContent: View {
    var model: ThreadModel
    var store: ResourceStore
    var opener: SystemHarnessOpener
    var actions: ThreadActions
    var moved: () -> Void
    var putBack: () -> Void
    var close: () -> Void

    var body: some View {
        let opensIn = opener.opensIn(model.thread?.session.openTarget)
        VStack(spacing: 0) {
            ThreadPanelTitle(thread: model.thread, opensIn: opensIn, stop: model.stop, actions: actions,
                             moved: moved, putBack: putBack, onClose: close)
            ThreadPage(model: model, opensIn: opensIn, actions: actions)
                .frame(maxWidth: .infinity)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Brand.page)
        .environment(\.resourceStore, store)
        .environment(\.linkOpener, appLinkOpener)
    }
}
