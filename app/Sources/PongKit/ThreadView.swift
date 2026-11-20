import AppKit
import SwiftUI

/// What the Thread window's controls do.
public struct ThreadActions {
    public var openInHarness: () -> Void = {}
    public var openSession: (SessionThread.SessionRef) -> Void = { _ in }
    /// Brings the card to a Ping that waits.
    public var showInQueue: (Ping) -> Void = { _ in }
    /// Asks for the Session's turn to be stopped.
    public var stop: () -> Void = {}

    public init(openInHarness: @escaping () -> Void = {},
                openSession: @escaping (SessionThread.SessionRef) -> Void = { _ in },
                showInQueue: @escaping (Ping) -> Void = { _ in },
                stop: @escaping () -> Void = {}) {
        self.openInHarness = openInHarness
        self.openSession = openSession
        self.showInQueue = showInQueue
        self.stop = stop
    }
}

/// The marks of the Harnesses Pong works with, from the kit, as template images: each takes the
/// colour of the text beside it.
@MainActor
enum HarnessMark {
    private static var loaded: [String: NSImage] = [:]

    /// Nil for a Harness the kit has no mark for.
    static func image(_ harness: String?) -> NSImage? {
        guard let harness, !harness.isEmpty else { return nil }
        if let image = loaded[harness] { return image }
        guard let image = brandResource("harness-\(harness)", "svg").flatMap(NSImage.init(contentsOf:)) else { return nil }
        image.isTemplate = true
        loaded[harness] = image
        return image
    }
}

/// A Session's Pings and the human's Pongs, oldest first, with their Resources. It shows only
/// what the Engine reports for the Thread, never the Session's transcript.
public struct ThreadView: View {
    var thread: SessionThread?
    var notice: String?
    /// What the open button's destination is called, as in "Claude" or "Terminal"; nil when
    /// Pong cannot name it.
    var opensIn: String?
    /// What the header shows for stopping the Session's turn, and the line under it when the
    /// last stop could not be delivered.
    var stop: ThreadStop?
    var cannotReach: String?
    var actions: ThreadActions
    /// The past Pings whose context the human opened.
    @State private var opened: Set<String>
    @Environment(\.cardContext) private var ctx

    public static let width: CGFloat = 520

    public init(thread: SessionThread?, notice: String? = nil, opensIn: String? = nil, stop: ThreadStop? = nil,
                cannotReach: String? = nil, actions: ThreadActions = .init(), opened: Set<String> = []) {
        self.thread = thread
        self.notice = notice
        self.opensIn = opensIn
        self.stop = stop
        self.cannotReach = cannotReach
        self.actions = actions
        _opened = State(initialValue: opened)
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let t = thread {
                header(t)
                VStack(alignment: .leading, spacing: 18) {
                    if t.pings.isEmpty {
                        Text("No Pings yet").brandStyle(.body).foregroundStyle(Brand.secondary)
                    }
                    ForEach(t.pings) { ping in entry(ping, context: t.contexts[ping.id]) }
                }
                .padding(20)
            } else {
                Text("Loading the Thread").brandStyle(.body).foregroundStyle(Brand.secondary).padding(20)
            }
        }
        // As wide as the window gives it, so a scroller never covers or cuts the Thread.
        .frame(idealWidth: Self.width, maxWidth: .infinity, alignment: .leading)
        .background(Brand.page)
    }

    /// Where the Session is: its Project's name, else its folder's.
    public static func place(_ t: SessionThread, project: Project?) -> String {
        project?.name ?? t.session.folder.map { URL(fileURLWithPath: $0).lastPathComponent } ?? ""
    }

    /// The Project and the Session's title, as the card's Project line names them; the window's title.
    public static func title(_ t: SessionThread, project: Project?) -> String {
        let place = place(t, project: project), title = t.session.title ?? "Untitled Session"
        return place.isEmpty ? title : "\(place) · \(title)"
    }

    private func header(_ t: SessionThread) -> some View {
        let project = ctx.directory.project(t.session.project)
        let place = Self.place(t, project: project?.0)
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .center, spacing: 8) {
                if let (p, logo) = project { ProjectTile(project: p, logo: logo) }
                if !place.isEmpty {
                    Text(place).brandStyle(.secondary).foregroundStyle(Brand.secondary).lineLimit(1)
                }
                Spacer(minLength: 12)
                ThreadStopControl(stop: stop, action: actions.stop)
                ThreadOpenButton(thread: t, opensIn: opensIn, action: actions.openInHarness)
            }
            if let cannotReach { CannotReachLine(line: cannotReach) }
            Text(t.session.title ?? "Untitled Session")
                .brandStyle(.question)
                .foregroundStyle(Brand.text)
                .fixedSize(horizontal: false, vertical: true)
                .help("Session \(t.session.id)")
            if let from = t.session.continuedFrom {
                HoverButton { actions.openSession(from) } label: { hover in
                    Label("Continued from an earlier Session", systemImage: "arrow.uturn.backward").inlineIcon(.label)
                        .brandStyle(.label)
                        .foregroundStyle(hover ? Brand.secondary : Brand.link)
                        .underline()
                }
                .help("Session \(from.id)")
                .accessibilityHint("Opens the Thread it was continued from")
            }
            if let notice {
                Text(notice)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.text)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Brand.row, in: Rectangle())
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Brand.surface)
    }

    private func entry(_ ping: Ping, context: PingContext?) -> some View {
        let details = Details(ping, context)
        let open = opened.contains(ping.id)
        return VStack(alignment: .leading, spacing: 8) {
            if ping.line == true || ping.isThought {
                // One of Pong's own lines, such as who stopped a turn, or the agent's Thought:
                // plain words, no block, no label, and it does not open.
                Text(ping.question).brandStyle(.secondary).foregroundStyle(Brand.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.leading, 4)
            } else if !Self.showsQuestion(ping) {
                // Text the human wrote with no Ping waiting: only the Pong below.
            } else if Self.opensInQueue(ping) {
                // A question that waits is a button: it steps up on hover and shows on the card.
                HoverButton { actions.showInQueue(ping) } label: { hover in bubble(ping, hover: hover, details: nil) }
                    .help("Answer this Ping on the card")
                    .accessibilityHint("Shows this Ping in the Queue")
            } else if !details.isEmpty {
                // A past Ping opens what the card showed with it: its answers and its context.
                HoverButton { toggle(ping.id) } label: { hover in bubble(ping, hover: hover, details: open ? details : nil) }
                    .help(open ? "Hide what the card showed" : "Show what the card showed")
                    .accessibilityHint(open ? "Hides its answers and context" : "Shows its answers and context")
            } else {
                bubble(ping, hover: false, details: nil, selectable: true)
            }

            if let pong = ping.pong {
                HStack {
                    Spacer(minLength: 60)
                    VStack(alignment: .trailing, spacing: 4) {
                        ForEach(Self.lines(of: pong), id: \.self) { line in
                            Text(line).brandStyle(.body).foregroundStyle(Brand.onSelected)
                                .multilineTextAlignment(.trailing)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .padding(.horizontal, 14).padding(.vertical, 10)
                    .background(Brand.selected, in: Rectangle())
                }
                if let d = thread?.deliveryLine(of: ping) {
                    HStack {
                        Spacer()
                        DeliveryLine(text: d, missed: pong.missedHarness)
                    }
                }
            } else if let s = Self.stateLine(ping) {
                Text(s).brandStyle(.secondary).foregroundStyle(Brand.secondary).padding(.leading, 4)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private func toggle(_ id: String) {
        withAnimation(reduceMotion ? nil : hoverEase) {
            if opened.contains(id) { opened.remove(id) } else { opened.insert(id) }
        }
    }

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// A Ping's question with its Tags and Resources, on its block, and what the card showed with
    /// it when the human opened it.
    private func bubble(_ ping: Ping, hover: Bool, details: Details?, selectable: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            if ping.isNote == true {
                Text("Note").brandStyle(.label).foregroundStyle(Brand.secondary)
            }
            if TagLine.shows(ping) { TagLine(ping: ping) }
            let question = Text(ping.question)
                .brandStyle(ping.isNote == true ? .body : .answer)
                .foregroundStyle(Brand.text)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: true)
            // A question that is a button takes the click itself, so its text is not selected.
            if selectable { question.textSelection(.enabled) } else { question }
            if let details { detailsView(details) }
            if !(ping.resources ?? []).isEmpty { ResourceList(ping: ping) }
        }
        .padding(14)
        .frame(maxWidth: 420, alignment: .leading)
        .background(hover ? Brand.rowHover : Brand.surface, in: Rectangle())
        .contentShape(Rectangle())
    }

    private func detailsView(_ d: Details) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(d.answers, id: \.label) { a in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    // The chosen answers are ticked; the others stay in the secondary colour.
                    Image(systemName: "checkmark").imageScale(.large)
                        .font(Brand.Style.label.font)
                        .frame(width: 12)
                        .foregroundStyle(Brand.text)
                        .opacity(a.chosen ? 1 : 0)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(a.label).brandStyle(.answer).foregroundStyle(a.chosen ? Brand.text : Brand.secondary)
                        if let leads = a.leadsTo, !leads.isEmpty {
                            Text(leads).brandStyle(.secondary).foregroundStyle(Brand.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        if a.hardToUndo {
                            Text("Hard to undo").brandStyle(.label).foregroundStyle(Brand.warning)
                        }
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityAddTraits(a.chosen ? .isSelected : [])
            }
            ContextSectionList(sections: d.sections)
        }
        .padding(.top, 2)
        .transition(.opacity)
    }

    /// What the card showed with a past Ping besides its question: the answers it offered, the
    /// chosen ones ticked, with what each leads to, and the context shown beside the card.
    struct Details {
        struct Answer { var label: String; var chosen: Bool; var leadsTo: String?; var hardToUndo: Bool }
        var answers: [Answer]
        var sections: [ContextSection]
        var isEmpty: Bool { answers.isEmpty && sections.isEmpty }

        init(_ ping: Ping, _ context: PingContext?) {
            let chosen = Set([ping.pong?.answer].compactMap { $0} + (ping.pong?.answers ?? []))
            let explained = Dictionary((context?.answers ?? []).map { ($0.label, $0) }, uniquingKeysWith: { a, _ in a })
            answers = (ping.answers ?? []).map {
                Answer(label: $0.label, chosen: chosen.contains($0.label),
                       leadsTo: explained[$0.label]?.leadsTo, hardToUndo: explained[$0.label]?.hardToUndo == true)
            }
            // The answers show above with what they lead to, so only the background and why stay.
            sections = Directory.Card(ping: context).sections(.question).filter {
                if case .answers = $0.body { return false }
                return true
            }
        }
    }

    /// Text written in the Thread while no Ping waited has no question to show.
    static func showsQuestion(_ ping: Ping) -> Bool { ping.unasked != true }

    /// A question that waits for a Pong can be shown in the Queue.
    static func opensInQueue(_ ping: Ping) -> Bool { ping.isWaiting && ping.isNote != true }

    static func lines(of pong: Ping.Pong) -> [String] {
        var out: [String] = []
        if let a = pong.answer { out.append(a) }
        if let a = pong.answers, !a.isEmpty { out.append(a.joined(separator: ", ")) }
        if let t = pong.text, !t.isEmpty { out.append(t) }
        return out
    }

    /// Words for the Ping's state when no Pong carries it.
    static func stateLine(_ ping: Ping) -> String? {
        switch ping.state {
        case .waiting: ping.isNote == true ? nil : "Waiting for a Pong"
        case .handedBack: "Handed back to the Harness"
        case .answeredInHarness: "Answered in the Harness"
        case .withdrawn: "Withdrawn"
        case .answered, .dismissed, .other: nil
        }
    }
}

/// Stop, beside the open button while the Session is Running: an ordinary action in the bar's
/// neutral button, never a warning. Pressed, it gives way to "Stopping…" until the turn has ended.
struct ThreadStopControl: View {
    var stop: ThreadStop?
    var action: () -> Void

    var body: some View {
        switch stop {
        case .offered:
            Button(action: action) { Label("Stop", systemImage: "stop.fill").imageScale(.large) }
                .buttonStyle(.brand)
                .help("Stops this turn at the agent's next tool call. The Session stays open.")
        case .stopping:
            Text("Stopping…").brandStyle(.label).foregroundStyle(Brand.secondary)
        case nil:
            EmptyView()
        }
    }
}

/// Under the bar when a stop could not be delivered: "Can't reach the MacBook."
struct CannotReachLine: View {
    var line: String

    var body: some View {
        Label(line, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
            .brandStyle(.secondary)
            .foregroundStyle(Brand.text)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// The reply row at the foot of a Thread: the card's reply box and Send. What is written goes
/// where `target` says; with nowhere to go the box is off and says why.
public struct ThreadReply: View {
    var target: ThreadReplyTarget
    var sending: Bool
    var refusal: String?
    /// Sends the text; true when it went, and the box then empties.
    var send: (String) async -> Bool
    @State private var draft: String
    @FocusState private var typing: Bool

    public init(target: ThreadReplyTarget, sending: Bool = false, refusal: String? = nil, draft: String = "",
                send: @escaping (String) async -> Bool = { _ in false }) {
        self.target = target
        self.sending = sending
        self.refusal = refusal
        self.send = send
        _draft = State(initialValue: draft)
    }

    private var enabled: Bool {
        if case .none = target { return false }
        return !sending
    }

    private var placeholder: String {
        switch target {
        case .ping: "Write a reply"
        case .session: "Write to this Session"
        case .none(let why): why
        }
    }

    private var typed: String? {
        let t = ListText.markdown(draft).trimmingCharacters(in: .whitespacesAndNewlines)
        return t.isEmpty ? nil : t
    }

    private var canSend: Bool { enabled && typed != nil }

    private func submit() {
        guard canSend, let typed else { return }
        Task { if await send(typed) { draft = "" } }
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .bottom, spacing: 8) {
                ReplyField(text: Binding(get: { draft }, set: { draft = ListText.bulleted($0) }),
                           placeholder: placeholder, enabled: enabled, typing: $typing, send: submit)
                SendGroup(canSend: canSend, canOpen: false, typed: false, send: submit)
            }
            if let refusal {
                Label(refusal, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.text)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityLabel("Not sent: \(refusal)")
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 14)
        .frame(idealWidth: ThreadView.width, maxWidth: .infinity, alignment: .leading)
        .background(Brand.surface)
    }
}

/// Where a Pong in the Thread stands: in the error colour with its icon when it did not reach the
/// Harness, else quietly.
struct DeliveryLine: View {
    var text: String
    var missed: Bool

    var body: some View {
        if missed {
            Label(text, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
                .brandStyle(.secondary)
                .foregroundStyle(Brand.statusError)
        } else {
            Text(text).brandStyle(.secondary).foregroundStyle(Brand.secondary)
        }
    }
}
