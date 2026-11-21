import AppKit
import SwiftUI

/// What the Bar says about the human's Sessions, from the Engine's event stream: how many are
/// working, how many ended in an error, what the Sessions active today have cost so far, and
/// how many Pings wait while the card is hidden.
public struct BarFigures: Equatable, Sendable {
    public var working = 0
    public var failed = 0
    /// Nil when no Session active today states its cost.
    public var cost: Double?
    public var waiting = 0

    public init(working: Int = 0, failed: Int = 0, cost: Double? = nil, waiting: Int = 0) {
        self.working = working
        self.failed = failed
        self.cost = cost
        self.waiting = waiting
    }

    /// `asking` is every Ping waiting, hidden or not: a Session asking one counts as waiting on
    /// the human, as its Thread says, not as working.
    @MainActor
    public init(directory: Directory, asking: [Ping], waiting: Int, now: Date = Date(), calendar: Calendar = .current) {
        let statuses = ThreadStatus.of(directory, waiting: asking).map(\.status)
        working = statuses.filter { $0 == .running }.count
        failed = statuses.filter { $0 == .error }.count
        let costs = directory.sessions.values
            .filter { s in s.updatedAt.flatMap(Self.date).map { calendar.isDate($0, inSameDayAs: now) } ?? false }
            .compactMap(\.context?.costUsd)
        cost = costs.isEmpty ? nil : costs.reduce(0, +)
        self.waiting = waiting
    }

    /// The Engine writes times in RFC 3339, with or without fractions of a second.
    static func date(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }
}

extension ThreadStatus {
    /// Every Session's status as the Bar counts it and its Threads list shows it, newest first.
    @MainActor
    static func of(_ directory: Directory, waiting: [Ping]) -> [(session: Session, status: ThreadStatus)] {
        let asking = Set(waiting.filter { $0.isNote != true }.map(\.session))
        return directory.sessions.values.sorted { ($0.updatedAt ?? "") > ($1.updatedAt ?? "") }
            .map { ($0, ThreadStatus(state: $0.state, hasWaitingPing: asking.contains($0.id))) }
    }
}

extension ThreadRow {
    /// The Threads the Bar opens into: the Sessions that changed most recently, newest first, and
    /// every older one still Running, Waiting or in Error, so each figure has its Threads in the list.
    /// A Session without a title is named by its Project; its identifier shows only on hover.
    /// How many more Threads Show more lists.
    static let page = 8

    @MainActor
    static func recent(_ directory: Directory, waiting: [Ping], count: Int = page) -> [ThreadRow] {
        ThreadStatus.of(directory, waiting: waiting).enumerated()
            .filter { i, s in i < count || s.status != .done }
            .map { _, s in
                let title = s.session.title.flatMap { $0.isEmpty ? nil : $0 }
                    ?? directory.project(s.session.project).map { "Session in \($0.0.name)" } ?? "Untitled Session"
                let project = directory.project(s.session.project)
                return ThreadRow(ref: .init(harness: s.session.harness ?? "", id: s.session.id), title: title,
                                 status: s.status, project: project?.0, logo: project?.1, details: s.session.details)
            }
    }
}

/// The Bar: the card's place on screen while nothing waits. While the pointer rests on it, it
/// opens into the Threads list below its figures (above them in a bottom corner, so the figures
/// stay where they are); its New Session button opens it into the start panel instead. A click
/// on its row sticks the list open, so it stays without the pointer, and another unsticks it. A Ping
/// that arrives while either is open waits as a button at its top until the pointer leaves the
/// list or the start panel closes. A right click on its row offers Settings and Quit.
public struct BarView: View {
    /// What the Bar has opened into.
    public enum Part: Equatable, Sendable { case threads, start }

    var figures: BarFigures
    var threads: [ThreadRow]
    /// The Threads open in panels beside the card, drawn selected in the list.
    var openThreads: Set<SessionThread.SessionRef>
    /// Lists more Threads; nil when every one is listed.
    var more: (() -> Void)?
    var open: Part?
    /// The human stuck the Threads list open.
    var stuck: Bool
    /// The start panel, shown while `open` is `.start`.
    var start: StartPanelView?
    /// The question of a Ping held behind the open list, and where it is from.
    var held: (question: String, from: String?)?
    var anchorsBottom: Bool
    var actions: Actions
    /// The part last opened, kept in the layout while it closes.
    @State private var part: Part
    /// The part is in the layout (it stays there while it closes), and its content is visible.
    @State private var listed: Bool
    @State private var visible: Bool
    /// The start panel last opened, kept on screen while it closes.
    @State private var kept: StartPanelView?
    /// The opening or closing under way, cancelled by the next one.
    @State private var moving: Task<Void, Never>?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.chosenCardWidth) private var width

    public struct Actions {
        public var openThread: (SessionThread.SessionRef) -> Void = { _ in }
        /// The held Ping's button: the card shows it now.
        public var openHeld: () -> Void = {}
        /// Pings wait behind a hidden card: the card comes back on the first.
        public var unhide: () -> Void = {}
        /// The pointer came onto the Bar or left it.
        public var pointing: (Bool) -> Void = { _ in }
        /// The human dragged the Bar somewhere new, or double-clicked it back to its corner.
        public var moved: () -> Void = {}
        public var putBack: () -> Void = {}
        /// A click on the row: the Threads list sticks open, or comes unstuck.
        public var stick: () -> Void = {}
        /// The New Session button: opens the start panel, or closes it.
        public var newSession: () -> Void = {}
        /// Settings…, from the right click on the row.
        public var settings: () -> Void = {}

        public init(openThread: @escaping (SessionThread.SessionRef) -> Void = { _ in }, openHeld: @escaping () -> Void = {},
                    unhide: @escaping () -> Void = {}, pointing: @escaping (Bool) -> Void = { _ in },
                    moved: @escaping () -> Void = {}, putBack: @escaping () -> Void = {}, stick: @escaping () -> Void = {},
                    newSession: @escaping () -> Void = {}, settings: @escaping () -> Void = {}) {
            self.moved = moved
            self.stick = stick
            self.putBack = putBack
            self.newSession = newSession
            self.settings = settings
            self.openThread = openThread
            self.openHeld = openHeld
            self.unhide = unhide
            self.pointing = pointing
        }
    }

    public init(figures: BarFigures, threads: [ThreadRow], openThreads: Set<SessionThread.SessionRef> = [], more: (() -> Void)? = nil,
                open: Part? = nil, stuck: Bool = false, start: StartPanelView? = nil,
                held: (question: String, from: String?)? = nil, anchorsBottom: Bool = false, actions: Actions = Actions()) {
        self.figures = figures
        self.threads = threads
        self.openThreads = openThreads
        self.more = more
        self.open = open
        self.stuck = stuck
        self.start = start
        self.held = held
        self.anchorsBottom = anchorsBottom
        self.actions = actions
        _part = State(initialValue: open ?? .threads)
        _listed = State(initialValue: open != nil)
        _visible = State(initialValue: open != nil)
        _kept = State(initialValue: start)
    }

    public var body: some View {
        VStack(spacing: 0) {
            if anchorsBottom { listPart }
            figuresRow
            if !anchorsBottom { listPart }
        }
        .frame(width: width)
        .foregroundStyle(Brand.text)
        .background(HoverTracker { actions.pointing($0) })
        .onChange(of: open) { _, now in
            if now == .start, let start { kept = start }
            move(to: now)
        }
    }

    // MARK: Figures

    /// The mark where the card has it, then the figures at the far end. Anywhere on the row
    /// but the waiting button moves the window, as the card's top bar does.
    private var figuresRow: some View {
        HStack(spacing: 14) {
            DragGrip(pressed: stuck, hint: stuck ? "Click to let the Threads close. Drag to move. Double-click to put it back."
                                                 : "Click to keep the Threads open. Drag to move. Double-click to put it back.")
            Spacer(minLength: 0)
            Group {
                // The spinner says working, so only the count is written; VoiceOver hears the words.
                if figures.working > 0 {
                    figure(Spinner(size: Brand.Style.answer.size * 1.2), "\(figures.working)", colour: Brand.text,
                           spoken: figures.working == 1 ? "1 Session working" : "\(figures.working) Sessions working")
                        .transition(fade)
                } else {
                    figure(Image(systemName: "checkmark.circle").imageScale(.large).brandIcon().foregroundStyle(Brand.statusDone), nil, colour: Brand.secondary,
                           spoken: "No Sessions working")
                        .transition(fade)
                }
                if figures.failed > 0 {
                    figure(Image(systemName: "exclamationmark.triangle").imageScale(.large).brandIcon().foregroundStyle(Brand.statusError),
                           figures.failed == 1 ? "1 error" : "\(figures.failed) errors", colour: Brand.statusError)
                        .transition(fade)
                }
                if let cost = figures.cost {
                    figure(Image(systemName: "dollarsign.circle").imageScale(.large).brandIcon().foregroundStyle(Brand.secondary),
                           String(format: "$%.2f today", cost), colour: Brand.secondary)
                        .transition(fade)
                }
            }
            .allowsHitTesting(false)
            if figures.waiting > 0 { waitingButton.transition(fade) }
            newSessionButton
        }
        // The figures beside one that comes or goes ease to their new places.
        .animation(reduceMotion ? nil : CardMotion.box, value: shownFigures)
        .padding(DragGrip.inset)
        .padding(.trailing, 6)
        .frame(height: Self.height)
        // The drag area sits in front of the surface: a colour in front would take the clicks.
        .background { WindowDragArea(moved: actions.moved, putBack: actions.putBack, menu: menu, clicked: actions.stick) }
        .background(Brand.surface)
        .accessibilityElement(children: .contain)
    }

    /// The Bar's height when closed.
    public static let height: CGFloat = 42

    private var fade: AnyTransition { reduceMotion ? .identity : CardMotion.fade }

    /// Which figures are on the row: a change of a count alone moves nothing.
    private var shownFigures: [Bool] {
        [figures.working > 0, figures.failed > 0, figures.cost != nil, figures.waiting > 0]
    }

    /// An icon and its text; `spoken` replaces both for VoiceOver when the text alone says too little.
    private func figure<I: View>(_ icon: I, _ text: String?, colour: Color, spoken: String? = nil) -> some View {
        HStack(spacing: 6) {
            icon.font(Brand.Style.answer.font).frame(width: 18, height: 18)
            if let text { Text(text).brandStyle(.label).foregroundStyle(colour).monospacedDigit().fixedSize() }
        }
        .accessibilityElement(children: .combine)
        .modifier(SpokenAs(label: spoken))
    }

    /// Pings wait behind a hidden card: a click brings it back.
    private var waitingButton: some View {
        HoverButton(action: actions.unhide) { hover in
            HStack(spacing: 6) {
                Image(systemName: "exclamationmark.bubble").imageScale(.large).font(Brand.Style.answer.font).brandIcon().foregroundStyle(Brand.statusWaiting)
                Text("\(figures.waiting) waiting").brandStyle(.label).monospacedDigit()
            }
            .padding(.horizontal, 8)
            .frame(height: 26)
            .background(hover ? Brand.rowHover : Brand.row, in: Rectangle())
            .contentShape(Rectangle())
        }
        .help("Show the card")
    }

    /// Opens the start panel, and closes it again.
    private var newSessionButton: some View {
        let closes = open == .start
        // A quarter turn of the plus makes the cross that closes the panel.
        return IconButton(symbol: "plus", label: closes ? "Close New Session" : "New Session",
                          help: closes ? "Close" : "New Session", held: closes, turn: closes ? 45 : 0,
                          action: actions.newSession)
    }

    /// The right click on the row.
    private func menu() -> NSMenu { PongMenu.make(settings: actions.settings) }

    // MARK: What it opens into

    @ViewBuilder private var listPart: some View {
        if listed {
            VStack(alignment: .leading, spacing: 8) {
                if !anchorsBottom, let held { heldButton(held).padding(.horizontal, ThreadRows.inset) }
                if part == .start, let start = start ?? kept { start.padding(.horizontal, ThreadRows.inset) } else { ThreadRows(threads: threads, open: openThreads, more: more, openThread: actions.openThread) }
                if anchorsBottom, let held { heldButton(held).padding(.horizontal, ThreadRows.inset) }
            }
            .opacity(visible ? 1 : 0)
            .padding(anchorsBottom ? .top : .bottom, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Brand.surface)
        }
    }

    /// The Ping that arrived while the list or the start panel was open. It stays put at the edge
    /// nearest the figures; the card takes its place when the pointer leaves the list or the start
    /// panel closes.
    private func heldButton(_ held: (question: String, from: String?)) -> some View {
        HoverButton(action: actions.openHeld) { hover in
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.bubble").imageScale(.large).font(Brand.Style.label.font).brandIcon().foregroundStyle(Brand.onSelected)
                VStack(alignment: .leading, spacing: 1) {
                    if let from = held.from {
                        Text(from).brandStyle(.secondary).foregroundStyle(Brand.onSelected).lineLimit(1)
                    }
                    Text(held.question).brandStyle(.label).foregroundStyle(Brand.onSelected).lineLimit(1).truncationMode(.tail)
                }
                Spacer(minLength: 8)
                Image(systemName: "arrow.right").imageScale(.large).font(Brand.Style.label.font).brandIcon().foregroundStyle(Brand.onSelected)
            }
            .padding(.horizontal, 10)
            .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
            .background(hover ? Brand.selectedHover : Brand.selected, in: Rectangle())
            .contentShape(Rectangle())
        }
        .accessibilityLabel("New question: \(held.question)")
        .accessibilityHint("Shows the card")
        .transition(reduceMotion ? .identity : CardMotion.fade)
    }

    /// Opens, closes and swaps the list and the start panel on the questions card's timings:
    /// the content fades out, the window's surface eases to the new height (`CardFaceStack`),
    /// and the new content fades in from a moment into the ease.
    private func move(to now: Part?) {
        moving?.cancel()
        if reduceMotion {
            if let now { part = now }
            (listed, visible) = (now != nil, now != nil)
            if now == nil { kept = nil }
            return
        }
        let t = CardStage.Timing.standard
        moving = Task { @MainActor in
            if visible && (now == nil || part != now) {
                withAnimation(CardMotion.ease(t.fadeOut)) { visible = false }
                try? await Task.sleep(for: t.fadeOut)
                guard !Task.isCancelled else { return }
            }
            guard let now else {
                (listed, kept) = (false, nil)
                return
            }
            part = now
            listed = true
            try? await Task.sleep(for: .milliseconds(16) + t.settle) // the surface's ease is under way
            guard !Task.isCancelled else { return }
            withAnimation(CardMotion.ease(t.fadeIn)) { visible = true }
        }
    }
}

/// Gives a figure its spoken label when it has one, and leaves it as combined otherwise.
private struct SpokenAs: ViewModifier {
    var label: String?
    func body(content: Content) -> some View {
        if let label { content.accessibilityLabel(label) } else { content }
    }
}

/// A menu item that runs a closure.
final class ActionMenuItem: NSMenuItem {
    private let run: () -> Void

    init(_ title: String, key: String, action run: @escaping () -> Void) {
        self.run = run
        super.init(title: title, action: #selector(runAction), keyEquivalent: key)
        target = self
    }

    required init(coder: NSCoder) { fatalError("not used") }

    @objc private func runAction() { run() }
}
