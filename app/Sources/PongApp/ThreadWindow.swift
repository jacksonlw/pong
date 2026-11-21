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
    private var threads: [SessionThread.SessionRef: (DockPanel, Task<Void, Never>)] = [:]

    /// Brings the card to a Ping that waits.
    private let showInQueue: (Ping) -> Void

    static let height: CGFloat = 640
    private static func key(_ ref: SessionThread.SessionRef) -> String { "thread:\(ref.harness)/\(ref.id)" }

    init(client: @escaping @Sendable () async -> EngineClient?, dock: Dock, showInQueue: @escaping (Ping) -> Void) {
        self.client = client
        self.dock = dock
        self.showInQueue = showInQueue
        ThreadOpening.open = { [weak self] ping in
            let harness = CardContext.live.directory.card(for: ping).session?.harness ?? ""
            self?.show(SessionThread.SessionRef(harness: harness, id: ping.session))
        }
        // The Session opens in its Harness; where Pong has no way in, its Thread says so.
        SessionOpening.open = { ping in
            let s = CardContext.live.directory.card(for: ping).session
            if SystemHarnessOpener().open(app: s?.appCommand, resume: s?.resumeCommand, startedBy: s?.startedBy) { return }
            ThreadOpening.open?(ping)
        }
    }

    func show(_ ref: SessionThread.SessionRef) {
        let beside = opensBeside(NSApp.currentEvent?.modifierFlags ?? [])
        let key = Self.key(ref)
        if let (p, _) = threads[ref] {
            p.orderFrontRegardless()
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
            moved: { dock.dropped(key) }, putBack: { dock.putBack(key) }, close: { [weak self] in self?.close(ref) },
            widen: { [weak p] width in p?.setContentSize(NSSize(width: width, height: Self.height)) })
        p.contentView = FirstMouseHostingView(rootView: root)
        let live = Task {
            await model.refresh()
            while !Task.isCancelled {
                guard let c = await client() else { try? await Task.sleep(for: .milliseconds(500)); continue }
                await model.refresh() // catch up on what changed while not listening
                do { for try await e in c.newEvents() { await model.handle(e) } } catch {}
                try? await Task.sleep(for: .milliseconds(500))
            }
        }
        threads[ref] = (p, live)
        dock.show(key, p, replacing: beside ? { _ in false } : { $0.hasPrefix("thread:") },
                  closed: { [weak self] in self?.close(ref) })
        print("thread shown \(ref.id)")
    }

    private func close(_ ref: SessionThread.SessionRef) {
        guard let (_, live) = threads.removeValue(forKey: ref) else { return }
        live.cancel()
        dock.remove(Self.key(ref))
    }
}

/// The Thread under its title strip, with the opened Resource's preview beside it.
private struct ThreadWindowContent: View {
    var model: ThreadModel
    var store: ResourceStore
    var opener: SystemHarnessOpener
    var actions: ThreadActions
    var moved: () -> Void
    var putBack: () -> Void
    var close: () -> Void
    /// Makes the panel this wide, to fit a preview beside the Thread or not.
    var widen: (CGFloat) -> Void

    /// The Project and the Session, as the card's Project line names them.
    private var title: String {
        guard let t = model.thread else { return "Thread" }
        return ThreadView.title(t, project: CardContext.live.directory.project(t.session.project)?.0)
    }

    private var width: CGFloat { ThreadView.width + (store.opened.isEmpty ? 0 : previewSize.width + 8) }

    var body: some View {
        VStack(spacing: 0) {
            PanelTitle(symbol: "text.bubble", title: title, closeLabel: "Close Thread", moved: moved, putBack: putBack, onClose: close)
            HStack(alignment: .top, spacing: 8) {
                ThreadPage(model: model, opensIn: opener.opensIn(model.thread?.session.openTarget), actions: actions)
                    .frame(width: ThreadView.width)
                if let r = store.opened.last {
                    ResourcePreview(resource: r, data: store.contents[r.id]) { store.close(r) }
                        .frame(width: previewSize.width)
                }
            }
        }
        .frame(width: width, height: ThreadWindowController.height)
        .background(Brand.page)
        .environment(\.resourceStore, store)
        .environment(\.linkOpener, appLinkOpener)
        .onChange(of: width) { _, now in widen(now) }
    }
}
