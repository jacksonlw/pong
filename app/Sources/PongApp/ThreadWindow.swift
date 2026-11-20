import AppKit
import PongKit
import SwiftUI

/// One ordinary window per Thread, opened by clicking a Ping's question (an explicit click, so it
/// activates Pong). Each window fetches its Thread and fetches it again on events about it.
@MainActor
final class ThreadWindowController {
    private let client: @Sendable () async -> EngineClient?
    private var windows: [SessionThread.SessionRef: (NSWindow, Task<Void, Never>)] = [:]
    private var closing: [NSObjectProtocol] = []

    /// Brings the card to a Ping that waits.
    private let showInQueue: (Ping) -> Void

    init(client: @escaping @Sendable () async -> EngineClient?, showInQueue: @escaping (Ping) -> Void) {
        self.client = client
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
        NSApp.activate()
        if let (w, _) = windows[ref] {
            w.makeKeyAndOrderFront(nil)
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
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: ThreadView.width, height: 600),
                         styleMask: [.titled, .closable, .resizable, .miniaturizable], backing: .buffered, defer: false)
        w.title = "Thread"
        let root = ThreadWindowContent(model: model, store: store, opener: opener, actions: ThreadActions(
            openInHarness: { model.openInHarness(with: opener) },
            openSession: { [weak self] in self?.show($0) },
            showInQueue: showInQueue,
            stop: { Task { await model.stopTurn() } }), retitle: { [weak w] in w?.title = $0 })
        w.contentView = NSHostingView(rootView: root)
        w.contentMinSize = NSSize(width: ThreadWindowContent.minWidth, height: 360)
        w.isReleasedWhenClosed = false
        w.center()
        if let last = windows.values.first?.0 { w.setFrameTopLeftPoint(NSPoint(x: last.frame.minX + 24, y: last.frame.maxY - 24)) }
        let live = Task {
            await model.refresh()
            while !Task.isCancelled {
                guard let c = await client() else { try? await Task.sleep(for: .milliseconds(500)); continue }
                await model.refresh() // catch up on what changed while not listening
                do { for try await e in c.newEvents() { await model.handle(e) } } catch {}
                try? await Task.sleep(for: .milliseconds(500))
            }
        }
        windows[ref] = (w, live)
        closing.append(NotificationCenter.default.addObserver(forName: NSWindow.willCloseNotification, object: w, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.windows[ref]?.1.cancel()
                self?.windows[ref] = nil
            }
        })
        w.makeKeyAndOrderFront(nil)
        print("thread shown \(ref.id)")
    }
}

/// The Thread with the opened Resource's preview beside it.
private struct ThreadWindowContent: View {
    var model: ThreadModel
    var store: ResourceStore
    var opener: SystemHarnessOpener
    var actions: ThreadActions
    /// Names the window after the Project and the Session, as the card's Project line does.
    var retitle: (String) -> Void

    static let minWidth: CGFloat = 420

    private var title: String {
        guard let t = model.thread else { return "Thread" }
        return ThreadView.title(t, project: CardContext.live.directory.project(t.session.project)?.0)
    }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            ThreadPage(model: model, opensIn: opener.opensIn(model.thread?.session.openTarget), actions: actions)
                .frame(minWidth: Self.minWidth, idealWidth: ThreadView.width, maxWidth: .infinity)
            if let r = store.opened.last {
                ResourcePreview(resource: r, data: store.contents[r.id]) { store.close(r) }
                    .frame(width: previewSize.width)
            }
        }
        .frame(minHeight: 360, idealHeight: 600)
        .background(Brand.page)
        .environment(\.resourceStore, store)
        .environment(\.linkOpener, appLinkOpener)
        .onChange(of: title, initial: true) { _, now in retitle(now) }
        .onChange(of: store.opened.last?.id) { _, _ in
            guard let w = NSApp.keyWindow else { return }
            let width = ThreadView.width + (store.opened.isEmpty ? 0 : previewSize.width + 8)
            var f = w.frame
            f.size.width = width
            w.setFrame(f, display: true, animate: false)
        }
    }
}
