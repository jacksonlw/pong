import SwiftUI

/// The Thread window's page: the Thread scrolling under a low bar that its header eases into,
/// a button back to the latest Ping while the human reads further up, and the reply row below.
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
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(spacing: 0) {
                        ThreadView(thread: model.thread, notice: model.notice, opensIn: opensIn, stop: model.stop,
                                   cannotReach: model.cannotReach, actions: actions)
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
                .overlay(alignment: .top) {
                    if scroll.collapsed, let t = model.thread {
                        ThreadBar(thread: t, opensIn: opensIn, stop: model.stop, cannotReach: model.cannotReach,
                                  openInHarness: actions.openInHarness, stopTurn: actions.stop)
                            .transition(reduceMotion ? .opacity : .move(edge: .top).combined(with: .opacity))
                    }
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
    /// The header's Session title has gone up under the bar, so the bar shows.
    var collapsed = false
    /// The latest Ping is in view.
    var atBottom = true

    /// How far the Thread scrolls before its header has become the bar: about where the Session
    /// title starts, under the Project line.
    static let collapseAt: CGFloat = 44
    /// What counts as the bottom: the latest Ping in view, with or without the space under it.
    static let bottomSlack: CGFloat = 24
    /// The page's colour between the Thread and the reply row.
    static let replyGap: CGFloat = 8

    init() {}

    init(offset: CGFloat, visible: CGFloat, content: CGFloat) {
        collapsed = offset > Self.collapseAt
        atBottom = offset + visible >= content - Self.bottomSlack
    }
}

/// The low bar at the top of a scrolled Thread: the Project and the Session's title, as the card's
/// Project line names them, Stop while the Session is Running, and the open button. Under it,
/// the line that says a stop could not be delivered.
public struct ThreadBar: View {
    var thread: SessionThread
    var opensIn: String?
    var stop: ThreadStop?
    var cannotReach: String?
    var openInHarness: () -> Void
    var stopTurn: () -> Void
    @Environment(\.cardContext) private var ctx

    public init(thread: SessionThread, opensIn: String?, stop: ThreadStop? = nil, cannotReach: String? = nil,
                openInHarness: @escaping () -> Void = {}, stopTurn: @escaping () -> Void = {}) {
        self.thread = thread
        self.opensIn = opensIn
        self.stop = stop
        self.cannotReach = cannotReach
        self.openInHarness = openInHarness
        self.stopTurn = stopTurn
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            row
            if let cannotReach { CannotReachLine(line: cannotReach) }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
        .frame(idealWidth: ThreadView.width, maxWidth: .infinity, alignment: .leading)
        .background(Brand.surface)
        .brandShadow()
        .accessibilityElement(children: .contain)
    }

    private var row: some View {
        let project = ctx.directory.project(thread.session.project)
        return HStack(spacing: 8) {
            if let (p, logo) = project { ProjectTile(project: p, logo: logo, side: 18) }
            Text(ThreadView.title(thread, project: project?.0))
                .brandStyle(.label)
                .foregroundStyle(Brand.text)
                .lineLimit(1)
                .truncationMode(.tail)
                .help("Session \(thread.session.id)")
            Spacer(minLength: 12)
            ThreadStopControl(stop: stop, action: stopTurn)
            ThreadOpenButton(thread: thread, opensIn: opensIn, action: openInHarness)
        }
    }
}

/// Opens the Session in its Harness, named where Pong knows it; another computer's Session has none.
struct ThreadOpenButton: View {
    var thread: SessionThread
    var opensIn: String?
    var action: () -> Void

    var body: some View {
        if thread.remote != true {
            Button(action: action) {
                HStack(spacing: 6) {
                    if opensIn != nil, let mark = HarnessMark.image(thread.session.harness) {
                        Image(nsImage: mark).resizable().frame(width: 17, height: 17).accessibilityHidden(true)
                    }
                    Text(opensIn.map { "Open in \($0)" } ?? "Open in the Harness")
                }
            }
            .buttonStyle(.brand)
        }
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
