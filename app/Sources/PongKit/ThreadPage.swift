import SwiftUI

/// The Thread window's page, under the panel's title strip (`ThreadPanelTitle`): the line that
/// says a stop could not be delivered, the Thread scrolling, a button back to the latest Ping
/// while the human reads further up, and the reply row below.
public struct ThreadPage: View {
    var model: ThreadModel
    var opensIn: String?
    var actions: ThreadActions
    @State private var scroll = ThreadScroll()
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    public init(model: ThreadModel, opensIn: String?, actions: ThreadActions) {
        self.model = model
        self.opensIn = opensIn
        self.actions = actions
    }

    /// How long the bar and the button take to come and go.
    static let ease = Animation.easeOut(duration: 0.18)

    public var body: some View {
        VStack(spacing: 0) {
            if let line = model.cannotReach {
                CannotReachLine(line: line)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Brand.surface)
            }
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(spacing: 0) {
                        ThreadView(thread: model.thread, notice: model.notice, actions: actions)
                        Color.clear.frame(height: 0).id(Self.latest)
                    }
                }
                // Opens at the latest Ping, as a chat does.
                .defaultScrollAnchor(.bottom, for: .initialOffset)
                // The Thread arrives after the window opens; a Ping that comes in while the human
                // is at the latest one keeps them there. After the new Pings are laid out, so the
                // bottom is their bottom.
                .onChange(of: model.thread?.pings.count) { old, _ in
                    if old == nil || scroll.atBottom { Task { @MainActor in proxy.scrollTo(Self.latest, anchor: .bottom) } }
                }
                .onScrollGeometryChange(for: ThreadScroll.self) { g in
                    ThreadScroll(offset: g.contentOffset.y + g.contentInsets.top, visible: g.containerSize.height,
                                 content: g.contentSize.height)
                } action: { _, now in
                    withAnimation(reduceMotion ? nil : Self.ease) { scroll = now }
                }
                .overlay(alignment: .bottomTrailing) {
                    if !scroll.atBottom {
                        ScrollToLatest {
                            withAnimation(reduceMotion ? nil : .easeOut(duration: 0.3)) { proxy.scrollTo(Self.latest, anchor: .bottom) }
                        }
                        .padding(16)
                        .transition(.opacity)
                    }
                }
            }
            if let t = model.thread {
                ThreadReply(target: t.replyTarget, sending: model.sending, refusal: model.refusal) { await model.send($0) }
                    // Its own surface, floating clear of the Thread scrolling under it.
                    .brandShadow()
                    .padding(.top, ThreadScroll.replyGap)
            }
        }
        .background(Brand.page)
    }

    /// Below the latest Ping, where the Thread opens and the button goes.
    private static let latest = "latest"
}

/// Where the Thread is scrolled to, as far as the page cares.
struct ThreadScroll: Equatable {
    /// The latest Ping is in view.
    var atBottom = true

    /// What counts as the bottom: the latest Ping in view, with or without the space under it.
    static let bottomSlack: CGFloat = 24
    /// The page's colour between the Thread and the reply row.
    static let replyGap: CGFloat = 8

    init() {}

    init(offset: CGFloat, visible: CGFloat, content: CGFloat) {
        atBottom = offset + visible >= content - Self.bottomSlack
    }
}

/// The title strip of a Thread's panel: the Project's tile, the Project and the Session's title
/// as the card's Project line names them, and a ⋮ menu holding Stop while the Session is Running
/// and the open button's action, then the close control.
public struct ThreadPanelTitle: View {
    var thread: SessionThread?
    var opensIn: String?
    var stop: ThreadStop?
    var actions: ThreadActions
    var moved: () -> Void
    var putBack: () -> Void
    var onClose: () -> Void
    @Environment(\.cardContext) private var ctx

    public init(thread: SessionThread?, opensIn: String?, stop: ThreadStop?, actions: ThreadActions,
                moved: @escaping () -> Void, putBack: @escaping () -> Void, onClose: @escaping () -> Void) {
        self.thread = thread
        self.opensIn = opensIn
        self.stop = stop
        self.actions = actions
        self.moved = moved
        self.putBack = putBack
        self.onClose = onClose
    }

    public var body: some View {
        let project = thread.flatMap { ctx.directory.project($0.session.project) }
        PanelTitle(title: thread.map { ThreadView.title($0, project: project?.0) } ?? "Thread", closeLabel: "Close Thread",
                   menu: thread.map { Self.menu($0, opensIn: opensIn, stop: stop, actions: actions) } ?? [],
                   moved: moved, putBack: putBack, onClose: onClose) {
            if let (p, logo) = project { ProjectTile(project: p, logo: logo, side: 22) } else { PanelSymbol(name: "text.bubble") }
        }
    }

    /// Stop while the Session is Running ("Stopping…" once pressed), and Open in its Harness,
    /// named where Pong knows it; another computer's Session has no way in.
    @MainActor
    static func menu(_ t: SessionThread, opensIn: String?, stop: ThreadStop?, actions: ThreadActions) -> [PanelAction] {
        var out: [PanelAction] = []
        switch stop {
        case .offered: out.append(PanelAction("Stop", symbol: "stop.fill", action: actions.stop))
        case .stopping: out.append(PanelAction("Stopping…", symbol: "stop.fill", enabled: false))
        case nil: break
        }
        if t.remote != true {
            let mark = opensIn == nil ? nil : HarnessMark.image(t.session.harness)
            out.append(PanelAction(opensIn.map { "Open in \($0)" } ?? "Open in the Harness",
                                   symbol: mark == nil ? "arrow.up.forward.app" : nil, image: mark, action: actions.openInHarness))
        }
        return out
    }
}

/// Takes the human back down to the latest Ping.
struct ScrollToLatest: View {
    var action: () -> Void

    var body: some View {
        HoverButton(action: action) { hover in
            Image(systemName: "arrow.down").imageScale(.large)
                .font(Brand.Style.label.font)
                .foregroundStyle(Brand.text)
                .frame(width: 32, height: 32)
                .background(hover ? Brand.rowHover : Brand.surface, in: Rectangle())
                .brandShadow()
                .contentShape(Rectangle())
        }
        .help("Go to the latest Ping")
        .accessibilityLabel("Go to the latest Ping")
    }
}
