import AppKit
import SwiftUI

/// The card's standard width, as in the prototype.
public let cardWidth: CGFloat = 420
/// The widths the human can drag the card to.
public let cardWidths: ClosedRange<CGFloat> = 320...840

extension EnvironmentValues {
    /// The card's width now: the standard one unless the human has dragged an edge.
    @Entry public var chosenCardWidth: CGFloat = cardWidth
}

/// What the card's controls do. Each one only passes the human's choice on.
public struct CardActions {
    public var previous: () -> Void = {}
    public var next: () -> Void = {}
    public var send: (Ping.Pong) -> Void = { _ in }
    public var handBack: () -> Void = {}
    public var dismiss: () -> Void = {}
    /// Tucks the card away; the Ping keeps waiting.
    public var hide: () -> Void = {}
    /// The human dragged the card by its grip to somewhere new.
    public var moved: () -> Void = {}
    /// The human double-clicked the grip: the card goes back to its corner.
    public var putBack: () -> Void = {}
    /// Sends an undelivered Pong again: into its own Session, or as a new one when true.
    public var sendAgain: (Bool) -> Void = { _ in }
    /// Keeps an undelivered Pong without sending it again.
    public var leave: () -> Void = {}
    /// The human changed the text they are writing; other Devices show it.
    public var draft: (String) -> Void = { _ in }
    /// Takes back a Pong not yet delivered; the Ping waits again.
    public var withdraw: () -> Void = {}

    public init(previous: @escaping () -> Void = {}, next: @escaping () -> Void = {},
                send: @escaping (Ping.Pong) -> Void = { _ in }, handBack: @escaping () -> Void = {},
                dismiss: @escaping () -> Void = {}, hide: @escaping () -> Void = {},
                moved: @escaping () -> Void = {}, putBack: @escaping () -> Void = {},
                sendAgain: @escaping (Bool) -> Void = { _ in }, leave: @escaping () -> Void = {},
                draft: @escaping (String) -> Void = { _ in }, withdraw: @escaping () -> Void = {}) {
        self.draft = draft
        self.withdraw = withdraw
        self.sendAgain = sendAgain
        self.leave = leave
        self.hide = hide
        self.moved = moved
        self.putBack = putBack
        self.previous = previous
        self.next = next
        self.send = send
        self.handBack = handBack
        self.dismiss = dismiss
    }
}

/// One Ping as a card: the top bar with the position among the deck, where the Ping comes
/// from, its question, its answers in the Engine's order, and the reply row, with its
/// Resources on a card of their own below. A Ping that no longer waits shows what became of it.
public struct CardView: View {
    var ping: Ping
    var position: Int
    var count: Int
    var actions: CardActions
    /// The answer is sent and the Session's next Ping is still being written.
    var writing: Bool
    var stage: Stage
    /// Why the Engine refused this Ping's last Pong, in its words.
    var refusal: String?
    /// Voice mode for this card, when the app has it.
    var voice: VoiceSession?
    @State private var draft = ""
    @State private var ticked: Set<String> = []
    @State private var choosing = false
    @FocusState private var typing: Bool
    /// Where the reply box's caret is, for Speak to write at.
    @State private var caret: Int?
    /// Files added to the reply, by path, kept with the draft.
    @State private var files: [String] = []
    @State private var filesProblem: String?

    private func addFiles(_ paths: [String]) {
        let r = DraftFiles.adding(paths, to: files)
        files = r.paths
        filesProblem = r.problem
        Drafts.shared.setFiles(files, .ping(ping.id))
    }

    private func removeFile(_ path: String) {
        files.removeAll { $0 == path }
        filesProblem = nil
        Drafts.shared.setFiles(files, .ping(ping.id))
    }
    @Environment(Speak.self) private var speak: Speak?
    @Environment(SkillNames.self) private var skillNames: SkillNames?

    /// Where this Ping's Session's Skills are read.
    private var skillPlace: SkillNames.Place? {
        let card = ctx.directory.card(for: ping)
        guard let harness = card.session?.harness ?? card.ping?.harness else { return nil }
        let project = card.project?.id
        return SkillNames.Place(harness: harness, project: project, folder: project == nil ? ping.folder : nil,
                                computer: ping.computer?.id)
    }
    @State private var questionHover = false
    /// The question starts unfolded, for tests.
    private var unfolded = false
    @Environment(\.cardContext) private var ctx
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.chosenCardWidth) private var width

    /// How the middle of the card is staged between Pings (see CardStage): whether its
    /// content is visible, the height it eases to, and where its laid-out height is reported.
    public struct Stage {
        public var visible = true
        /// Which side the content sits off to while hidden (see CardStage.slide).
        public var slide = 0
        /// The drawn box's height as it eases to the content's (see InboxCard); nil fills the card.
        public var box: CGFloat?
        public var measured: (CGFloat) -> Void = { _ in }

        public init(visible: Bool = true, slide: Int = 0, box: CGFloat? = nil, measured: @escaping (CGFloat) -> Void = { _ in }) {
            self.visible = visible
            self.slide = slide
            self.box = box
            self.measured = measured
        }
    }

    public init(ping: Ping, position: Int = 0, count: Int = 1, writing: Bool = false, stage: Stage = Stage(),
                refusal: String? = nil, voice: VoiceSession? = nil, actions: CardActions = CardActions()) {
        self.ping = ping
        self.voice = voice
        self.refusal = refusal
        self.position = position
        self.count = count
        self.writing = writing
        self.stage = stage
        self.actions = actions
        // What was typed here before the card closed, the app quit or the human moved on.
        _draft = State(initialValue: ListText.bulleted(Drafts.shared.text(.ping(ping.id))))
        _files = State(initialValue: Drafts.shared.files(.ping(ping.id)))
    }

    /// A card as it stands with text typed and the caret's choices open or not, for tests.
    init(ping: Ping, count: Int = 1, refusal: String? = nil, draft: String, choosing: Bool = false) {
        self.init(ping: ping, count: count, refusal: refusal)
        _draft = State(initialValue: draft)
        _choosing = State(initialValue: choosing)
    }

    /// A card whose long question the human has unfolded, for tests.
    init(ping: Ping, unfolded: Bool) {
        self.init(ping: ping)
        self.unfolded = unfolded
    }

    public var body: some View {
        // The Resources' card rides on the question card's drawn bottom edge as the box eases,
        // placed in the same layout pass, and fades with the card's content between Pings.
        BoxStack(box: stage.box, gap: 8) {
            questionCard()
            if !writing, !(ping.resources ?? []).isEmpty {
                ResourceBar(ping: ping)
                    .transaction { $0.animation = nil }
                    .modifier(Staged(visible: stage.visible, slide: stage.slide))
            }
        }
        .frame(width: width)
    }

    /// Fades content out and in between Pings, with a 3 pt shift showing which way the human
    /// moved through the deck. The shift eases on the fade's own curve and clock, so the
    /// content keeps moving until it is gone.
    private struct Staged: ViewModifier {
        var visible: Bool
        var slide: Int
        @Environment(\.accessibilityReduceMotion) private var reduceMotion

        func body(content: Content) -> some View {
            let d = visible ? CardStage.Timing.standard.fadeIn : CardStage.Timing.standard.fadeOut
            content
                .opacity(visible ? 1 : 0)
                .modifier(Shift(x: reduceMotion ? 0 : CGFloat(slide) * 3))
                .animation(reduceMotion ? nil : CardMotion.ease(d), value: visible)
        }
    }

    /// An offset that moves in an animation even though the content inside takes none: the
    /// content is told never to animate its layout, which stops a plain offset too.
    private struct Shift: ViewModifier, Animatable {
        var x: CGFloat
        var animatableData: CGFloat {
            get { x }
            set { x = newValue }
        }

        func body(content: Content) -> some View { content.offset(x: x) }
    }

    private func questionCard() -> some View {
        CardLayout(box: stage.box, pinsLast: showsReply || voice != nil) {
            topBar()
            middle()
                // The content never resizes in an animation, whatever is in flight: it lays
                // out at once and fades.
                .transaction { $0.animation = nil }
                .modifier(Staged(visible: stage.visible, slide: stage.slide))
            if showsReply || voice != nil {
                // The reply row stays the same between Pings, so it never fades: the layout
                // keeps it on the box's bottom edge as the box eases to its height.
                VStack(alignment: .leading, spacing: 0) {
                    if showsReply { replyRow() }
                    if let voice { voiceRows(voice) }
                }
                .transaction { $0.animation = nil }
                .zIndex(1)
            }
        }
        .frame(width: width, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true)
        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { stage.measured($0) }
        // The card lays out at its new height at once; only the drawn box eases to it,
        // its top fixed and its bottom edge moving. It clips, leaving room for a focus outline.
        .background(Brand.surface, in: CardBox(height: stage.box))
        .mask { CardBox(height: stage.box, outset: 4) }
        // A click anywhere on the card takes over from voice's slow fill.
        .simultaneousGesture(TapGesture().onEnded { voice?.takeOver() })
        .onChange(of: voice?.fill.phase) { old, new in
            guard let fill = voice?.fill else { return }
            if new == .takenOver {
                if several { ticked.formUnion(fill.choices) }
                draft = fill.text
                actions.draft(ListText.markdown(fill.text))
            }
        }
    }

    /// Stacks the card's parts at their natural heights, and reports that natural height,
    /// but keeps the last part (the reply row) on the box's bottom edge while the box eases.
    /// Placing it in the same layout pass keeps it inside the box on every frame.
    private struct CardLayout: Layout {
        var box: CGFloat?
        var pinsLast: Bool

        var animatableData: CGFloat {
            get { box ?? -1 }
            set { box = newValue < 0 ? nil : newValue }
        }

        func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
            let width = proposal.width ?? cardWidth
            let height = subviews.reduce(0) { $0 + $1.sizeThatFits(ProposedViewSize(width: width, height: nil)).height }
            return CGSize(width: width, height: height)
        }

        func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
            var y = bounds.minY
            for (i, s) in subviews.enumerated() {
                let h = s.sizeThatFits(ProposedViewSize(width: bounds.width, height: nil)).height
                if pinsLast, i == subviews.count - 1, let box { y = bounds.minY + box - h }
                s.place(at: CGPoint(x: bounds.minX, y: y), proposal: ProposedViewSize(width: bounds.width, height: h))
                y += h
            }
        }
    }

    /// The card's box: from the top of the card down to `height`, so that easing the height
    /// moves only the bottom edge. Nil fills the card.
    private struct CardBox: Shape {
        var height: CGFloat?
        var outset: CGFloat = 0

        var animatableData: CGFloat {
            get { height ?? -1 }
            set { height = newValue < 0 ? nil : newValue }
        }

        func path(in r: CGRect) -> Path {
            Path(CGRect(x: r.minX - outset, y: r.minY - outset,
                        width: r.width + 2 * outset, height: (height ?? r.height) + 2 * outset))
        }
    }

    /// The reply box shows the text voice is writing while it fills in an answer. A list shows
    /// bullets here, and is Markdown everywhere else: in the draft other Devices see and the Pong.
    private var shownDraft: Binding<String> {
        Binding(get: { voice?.fill.active == true ? voice?.fill.text ?? "" : draft }, set: {
            let shown = ListText.bulleted($0)
            if shown != draft { actions.draft(ListText.markdown(shown)) }
            draft = shown
        })
    }

    /// Answers voice has understood and is filling in.
    private var voiceChoices: [String] { voice?.fill.active == true ? voice?.fill.choices ?? [] : [] }

    private func middle() -> some View {
        VStack(alignment: .leading, spacing: 0) {
            ProjectLine(ping: ping)
            // The tags belong to the Ping just answered, not the one being written.
            if showsTags {
                // A Project's tile reaches the Project line's bottom edge; its text alone stops
                // about 3 points short of it.
                TagLine(ping: ping).padding(.top, Self.tagGap - (ctx.directory.card(for: ping).project == nil ? 3 : 0))
            }
            if writing {
                HStack(spacing: 8) {
                    TypingDots()
                    Text("Writing the next question")
                        .brandStyle(.body)
                        .foregroundStyle(Brand.secondary)
                }
                .padding(.top, 26)
                .padding(.bottom, 30)
                .accessibilityElement(children: .combine)
            } else {
                FoldedQuestion(text: ping.question, unfolded: unfolded)
                    // A click opens the Thread, so the question steps up on hover like a button,
                    // its block reaching just past the text without moving it.
                    .padding(.horizontal, 6).padding(.vertical, 3)
                    .background(questionHover ? Brand.row : .clear, in: Rectangle())
                    .hoverHighlight($questionHover)
                    .padding(.horizontal, -6).padding(.vertical, -3)
                    // Under the Tag row, the question's own leading (about 4.5 points above its
                    // first line) makes up the rest of the gap, so the row sits midway.
                    .padding(.top, showsTags ? Self.tagGap - 4.5 : 10)
                    .padding(.bottom, 14)
                    .contextHover(.question, ping: ping, topInset: 7) // level with the hover block, not the padding above it
                    .onTapGesture { ThreadOpening.open?(ping) }
                answers()
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 4)
        .padding(.bottom, showsReply ? 2 : 16)
    }

    private var isNote: Bool { ping.isNote == true }

    /// The Tags belong to the Ping just answered, not the one being written.
    private var showsTags: Bool { !writing && TagLine.shows(ping) }

    /// The seen space between the Tag row and the Project line above it, and the same between
    /// the row and the question below it.
    static let tagGap: CGFloat = 12
    private var several: Bool { ping.allowSeveral == true }
    /// The reply row is gone while the next question is being written.
    private var showsReply: Bool { ping.isWaiting && !isNote && !writing }
    private var canReply: Bool { ping.isWaiting && !writing }

    // MARK: Top bar

    private func topBar() -> some View {
        HStack(spacing: 6) {
            // Sits opposite the hide button, at the other end of the bar.
            DragGrip()
            IconButton(symbol: "chevron.left", label: "Previous Ping", enabled: position > 0, action: actions.previous)
            HStack(spacing: 10) {
                GeometryReader { g in
                    ZStack(alignment: .leading) {
                        Rectangle().fill(Brand.track)
                        Rectangle().fill(Brand.text).frame(width: g.size.width * CGFloat(position + 1) / CGFloat(max(count, 1)))
                    }
                    // On the box's curve and clock, so the bar and the box move as one.
                    .animation(reduceMotion ? nil : CardMotion.easeOut(CardStage.Timing.standard.box), value: position)
                    .animation(reduceMotion ? nil : CardMotion.easeOut(CardStage.Timing.standard.box), value: count)
                }
                .frame(height: 4)
                Text("\(position + 1) of \(count)")
                    .font(Brand.Style.secondary.font)
                    .foregroundStyle(Brand.secondary)
            }
            // The progress bar is part of the drag area behind the top bar.
            .allowsHitTesting(false)
            IconButton(symbol: "chevron.right", label: "Next Ping", enabled: position < count - 1, action: actions.next)
            if let voice, voice.on {
                IconButton(symbol: voice.listening ? "speaker.wave.2.fill" : "speaker.wave.2", label: "Listen",
                           help: "Read this Ping aloud, then listen for your answer.",
                           enabled: ping.isWaiting && !voice.listening, action: { voice.start() })
            }
            // Hides the card only; nothing on the card that reads as close ends a Ping.
            IconButton(symbol: "xmark", label: "Hide", help: "Hide the card. The Ping keeps waiting.",
                       enabled: true, action: actions.hide)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
        // Anywhere on the bar but its buttons moves the card.
        .background { WindowDragArea(moved: actions.moved, putBack: actions.putBack) }
    }

    // MARK: Voice

    @ViewBuilder
    private func voiceRows(_ voice: VoiceSession) -> some View {
        if let problem = voice.problem {
            Label(problem, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
                .brandStyle(.secondary)
                .foregroundStyle(Brand.text)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 16)
                .padding(.bottom, 12)
        }
        if voice.fill.active {
            CountdownBar(progress: voice.fill.progress, paused: voice.fill.paused)
        }
        if voice.listening {
            ListeningBar(level: voice.level, stop: { voice.stop() })
        }
    }

    // MARK: Answers

    @ViewBuilder
    private func answers() -> some View {
        let list = ping.answers ?? []
        VStack(alignment: .leading, spacing: 8) {
            if let status {
                // A Pong that missed its Harness reads as the Thread shows it: red, with the error mark.
                if ping.pong?.missedHarness == true { DeliveryLine(text: status, missed: true) } else { Hint(text: status) }
            } else if several { Hint(text: "Choose any that apply, then send.") }
            if isNote {
                if ping.isWaiting { OptionRow(label: "Dismiss", mark: .none, action: actions.dismiss) }
            } else {
                if let pong = ping.pong, pong.needsHuman { undelivered(pong) }
                if let lost = ping.lost { Hint(text: lost.reason) }
                if ping.state == .answered, ping.pong?.delivery == "sent" {
                    // Not yet delivered, perhaps waiting for an unreachable computer: it can be taken back.
                    OptionRow(label: "Withdraw this Pong", mark: .none, action: actions.withdraw)
                }
                ForEach(Array(list.enumerated()), id: \.element.label) { i, a in
                    option(a.label)
                        .contextHover(.answer(a.label), ping: ping)
                }
                if let text = ping.pong?.text {
                    Text(ListText.bulleted(text))
                        .brandStyle(.body)
                        .foregroundStyle(Brand.text)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.vertical, 9)
                        .padding(.horizontal, 12)
                        .background(Brand.row, in: Rectangle())
                        .accessibilityLabel("You wrote: \(text)")
                }
            }
        }
    }

    private func option(_ label: String) -> some View {
        let pong = ping.pong
        if several {
            let on = ping.isWaiting ? ticked.contains(label) || voiceChoices.contains(label) : pong?.answers?.contains(label) == true
            return OptionRow(label: label, mark: .box(on), enabled: ping.isWaiting) {
                if ticked.contains(label) { ticked.remove(label) } else { ticked.insert(label) }
            }
        }
        // The recommended answer comes first and nothing else marks it.
        return OptionRow(label: label, mark: .none, enabled: ping.isWaiting, chosen: pong?.answer == label || voiceChoices.contains(label)) {
            actions.send(Ping.Pong(answer: label, text: typed))
        }
    }

    /// What the human can do with a Pong that came back: send it again into its own Session,
    /// even one sitting idle, send it as a new Session, or leave it.
    @ViewBuilder
    private func undelivered(_ pong: Ping.Pong) -> some View {
        if let refusal { Hint(text: refusal) }
        OptionRow(label: "Send again", mark: .none) { actions.sendAgain(false) }
        OptionRow(label: "Send as a new Session", mark: .none) { actions.sendAgain(true) }
        OptionRow(label: "Leave it", mark: .none, action: actions.leave)
    }

    /// What became of a Ping that no longer waits, and where its Pong stands.
    private var status: String? {
        switch ping.state {
        case .waiting: nil
        case .answered: ping.pong?.deliveryLine ?? "Answered"
        case .handedBack: "Handed back to the Harness"
        case .answeredInHarness: "Answered in the Harness"
        case .withdrawn: "Withdrawn by the agent"
        case .dismissed: "Dismissed"
        case .other(let raw): raw
        }
    }

    // MARK: Reply row

    private var typed: String? {
        let t = ListText.markdown(draft).trimmingCharacters(in: .whitespacesAndNewlines)
        return t.isEmpty ? nil : t
    }

    private var canSend: Bool { canReply && (typed != nil || !ticked.isEmpty || !files.isEmpty) }

    private func sendReply(newSession: Bool = false) {
        choosing = false
        guard canSend else { return }
        speak?.stop(.ping(ping.id))
        let (sending, problem) = PongFile.sending(files)
        if let problem {
            filesProblem = problem
            return
        }
        let ticks = (ping.answers ?? []).map(\.label).filter(ticked.contains)
        actions.send(Ping.Pong(answers: ticks.isEmpty ? nil : ticks, text: typed, newSession: newSession ? true : nil,
                               files: sending.isEmpty ? nil : sending))
    }

    /// The caret's send choices need typed text: a new Session starts from it. Hand back
    /// needs none, so the caret opens whenever the Ping waits.
    private var canChoose: Bool { canReply && typed != nil }

    private var placeholder: String {
        if writing { return "" }
        if ping.approves == "plan" { return "Or say what should change" }
        return several ? "Add text (optional)" : "Write a reply"
    }

    private func attachSpeak(_ can: Bool) {
        guard let speak else { return }
        let box = SpeakBox.ping(ping.id)
        guard can else { speak.detach(box); return }
        let text = shownDraft
        speak.attach(box, read: { text.wrappedValue }, write: { text.wrappedValue = $0 }, caret: { caret })
    }

    private func replyRow() -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .bottom, spacing: 8) {
                ReplyField(text: shownDraft, placeholder: placeholder, enabled: canReply, typing: $typing,
                           escape: {
                               if speak?.box == .ping(ping.id) {
                                   speak?.stop()
                                   return true
                               }
                               guard choosing else { return false }
                               choosing = false
                               return true
                           }, caret: { caret = $0 }, skills: skillNames?.offer(skillPlace) ?? .none,
                           addFiles: canReply ? { addFiles($0) } : nil) { sendReply() }
                    .task(id: skillPlace) { await skillNames?.load(skillPlace) }
                    // A draft written on another Device shows here while nobody types here.
                    .onChange(of: ping.draft?.text, initial: true) { before, theirs in
                        guard let theirs, !typing, ListText.bulleted(theirs) != draft else { return }
                        // On opening, the draft kept on this Device wins: the shared one may be
                        // older than what was typed here.
                        if before == theirs, !draft.isEmpty { return }
                        draft = ListText.bulleted(theirs)
                        // A draft kept here is replaced too, so it cannot win over the newer one later.
                        if !Drafts.shared.text(.ping(ping.id)).isEmpty { Drafts.shared.set(theirs, .ping(ping.id)) }
                    }
                SendGroup(canSend: canSend, canOpen: canReply, typed: canChoose,
                          send: { sendReply() }, caret: { choosing.toggle() })
                if let speak {
                    SpeakButton(speak: speak, box: .ping(ping.id), enabled: canReply) {
                        speak.toggle(.ping(ping.id), text: shownDraft, caret: caret)
                    }
                }
            }
            AddedFiles(paths: files, problem: filesProblem, remove: removeFile)
            if let problem = speak?.problem, problem.box == .ping(ping.id) {
                Label(problem.text, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.text)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let refusal {
                Label(refusal, systemImage: "exclamationmark.circle").inlineIcon(.secondary)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.text)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityLabel("Not sent: \(refusal)")
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 12)
        .padding(.bottom, 14)
        // The Speak shortcut writes here while the box can be written in; closing the box, or
        // the card moving to another Ping, ends Speak in it.
        .onChange(of: canReply, initial: true) { _, can in attachSpeak(can) }
        .onChange(of: ping.id) { old, _ in
            speak?.detach(.ping(old))
            attachSpeak(canReply)
        }
        .onChange(of: typing) { _, now in speak?.focus(.ping(ping.id), now) }
        .onDisappear { speak?.detach(.ping(ping.id)) }
        .overlay(alignment: .top) {
            // Hangs from the top of the reply row over the card's answers, as in the prototype.
            Color.clear.frame(height: 0).overlay(alignment: .bottomTrailing) {
                if choosing && canReply {
                    let card = ctx.directory.card(for: ping)
                    SendMenu(used: card.session?.context?.usedFraction,
                             canSend: canChoose, offersNewSession: ping.blocksAgent != true,
                             newSession: { sendReply(newSession: true) }, thisSession: { sendReply() },
                             handBack: ping.isRemote ? nil : { choosing = false; actions.handBack() })
                        .fixedSize()
                        .padding(.trailing, 16)
                        .offset(y: 6)
                        .transition(reduceMotion ? .identity : CardMotion.fade)
                }
            }
        }
    }
}

struct Hint: View {
    var text: String

    var body: some View {
        Text(text)
            .brandStyle(.secondary)
            .foregroundStyle(Brand.secondary)
    }
}

/// An answer or a checkbox answer, drawn as an equal block; every answer looks the same,
/// and a chosen one takes the selected roles.
struct OptionRow: View {
    enum Mark { case none, box(Bool) }

    var label: String
    var mark: Mark
    var enabled = true
    var chosen = false
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        let fg = chosen ? Brand.onSelected : enabled ? Brand.text : Brand.secondary
        Button(action: action) {
            HStack(spacing: 10) {
                switch mark {
                case .none: EmptyView()
                case .box(let on):
                    Image(systemName: "checkmark").imageScale(.large)
                        .font(Brand.Style.label.font)
                        .brandIcon()
                        .foregroundStyle(Brand.onSelected)
                        .opacity(on ? 1 : 0)
                        .frame(width: 20, height: 20)
                        .background(on ? Brand.selected : Brand.surface, in: Rectangle())
                }
                Text(label)
                    .brandStyle(.answer)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .foregroundStyle(fg)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 11)
            .padding(.horizontal, 14)
            .background(chosen ? Brand.selected : hover && enabled ? Brand.rowHover : Brand.row, in: Rectangle())
            .contentShape(Rectangle())
        }
        .buttonStyle(FocusedPlain())
        .allowsHitTesting(enabled)
        .hoverHighlight($hover)
        .accessibilityAddTraits(isBox ? .isToggle : [])
    }

    private var isBox: Bool { if case .box = mark { true } else { false } }
}

/// A button with an icon and no words, the one every such button in Pong uses: the same size,
/// hit area, hover and icon everywhere.
struct IconButton: View {
    var symbol: String
    var label: String
    var help: String?
    var enabled = true
    /// The icon's colour where the button sits on something other than Pong's surface.
    var colour: Color?
    /// What fills the square at rest, and on hover.
    var fill: Color = .clear
    var hoverFill: Color = Brand.row
    /// Drawn as if hovered, as while what it opens is open.
    var held = false
    /// A turn of the icon, in degrees.
    var turn: Double = 0
    var action: () -> Void
    @State private var hover = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol).imageScale(.large)
                .font(Brand.Style.label.font)
                .brandIcon()
                .rotationEffect(.degrees(turn))
                .animation(reduceMotion ? nil : CardMotion.box, value: turn)
                .foregroundStyle(colour ?? (hover && enabled ? Brand.text : Brand.secondary))
                .frame(width: 30, height: 30)
                .background((hover || held) && enabled ? hoverFill : fill, in: Rectangle())
                .contentShape(Rectangle())
        }
        .buttonStyle(FocusedPlain())
        .disabled(!enabled)
        .hoverHighlight($hover)
        .help(help ?? label)
        .accessibilityLabel(label)
    }
}

/// The Pong mark at the start of the card's top bar and of the Bar, in the same place on both.
/// The bar around it moves the window (`WindowDragArea`).
struct DragGrip: View {
    /// The Bar's list is stuck open: the mark stays stepped up, as if held down.
    var pressed = false
    /// What the bar around the mark does, said on hover.
    var hint = "Drag to move. Double-click to put it back."
    @Environment(\.cardMarkHeld) private var held
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Where the mark sits from the top-left corner of the bar it starts.
    static let inset = EdgeInsets(top: 6, leading: 8, bottom: 6, trailing: 0)

    var body: some View {
        PongMark()
            .foregroundStyle(pressed ? Brand.text : Brand.secondary)
            .frame(width: 30, height: 30)
            .background(pressed ? Brand.rowHover : .clear, in: Rectangle())
            .animation(reduceMotion ? nil : CardMotion.box, value: pressed)
            .opacity(held ? 0 : 1)
            // The mouse goes through the mark to the bar's drag area behind it.
            .allowsHitTesting(false)
            .help(hint)
            .accessibilityLabel("Move")
    }
}

/// The kit's mark (the bird), drawn in the current foreground colour.
struct PongMark: View {
    private static let image: NSImage? = brandResource("pong-mark-ink", "svg").flatMap(NSImage.init(contentsOf:))

    var body: some View {
        if let image = Self.image {
            Image(nsImage: image)
                .renderingMode(.template)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(width: 22)
                .accessibilityHidden(true)
        }
    }
}

/// Moves its window with the mouse, and reports a move once the mouse is let go. Shows an open
/// hand while the pointer is over it and a closed hand while dragging, even while Pong is
/// inactive (cursor rects only work in the key window, which the card rarely is). With `menu`,
/// a right click (or Control-click) opens that menu. With `clicked`, a click that moves nothing
/// is reported at once; the click that turns out to start a double-click is reported a second
/// time, which takes it back.
struct WindowDragArea: NSViewRepresentable {
    var moved: () -> Void
    var putBack: () -> Void
    var menu: (() -> NSMenu)? = nil
    var clicked: (() -> Void)? = nil

    func makeNSView(context: Context) -> Area { Area() }

    func updateNSView(_ v: Area, context: Context) {
        v.moved = moved
        v.putBack = putBack
        v.contextMenu = menu
        v.clicked = clicked
    }

    final class Area: NSView, PointerCursorOwner {
        var moved: () -> Void = {}
        var putBack: () -> Void = {}
        var contextMenu: (() -> NSMenu)?
        var clicked: (() -> Void)?
        /// The last press was reported as a click, so a double-click takes it back.
        private var reported = false
        /// Where the mouse and the window's top-left corner were when the press began. The top
        /// left, not the origin: the window may grow or shrink under the pointer mid-drag, and
        /// the grabbed spot must stay under it.
        private var start: (mouse: NSPoint, topLeft: NSPoint)?

        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
        override var mouseDownCanMoveWindow: Bool { false }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            trackingAreas.forEach(removeTrackingArea)
            addTrackingArea(NSTrackingArea(rect: .zero, options: [.mouseEnteredAndExited, .mouseMoved, .cursorUpdate, .activeAlways, .inVisibleRect],
                                           owner: self))
        }

        /// The bar's buttons sit on the area and hit-test before it, so over them the pointer is an arrow.
        func pointerCursor(at point: NSPoint) -> NSCursor? { .openHand }

        private func point(_ e: NSEvent) {
            if start == nil { window?.showPointerCursor() }
        }

        override func mouseEntered(with e: NSEvent) { point(e) }
        override func mouseMoved(with e: NSEvent) { point(e) }
        /// AppKit asks the view under the pointer for its cursor as the pointer moves; left to
        /// NSView this is the arrow, which then fights the open hand set on each move.
        override func cursorUpdate(with e: NSEvent) { point(e) }
        override func mouseExited(with e: NSEvent) { point(e) }

        override func menu(for event: NSEvent) -> NSMenu? { contextMenu?() }

        override func mouseDown(with e: NSEvent) {
            if e.modifierFlags.contains(.control), let menu = contextMenu?() {
                return NSMenu.popUpContextMenu(menu, with: e, for: self)
            }
            if e.clickCount == 2 {
                start = nil
                if reported { clicked?() }
                reported = false
                putBack()
                return
            }
            reported = false
            guard let w = window else { return }
            start = (NSEvent.mouseLocation, NSPoint(x: w.frame.minX, y: w.frame.maxY))
            NSCursor.closedHand.set()
        }

        override func mouseDragged(with e: NSEvent) {
            NSCursor.closedHand.set()
            guard let s = start, let w = window else { return }
            let p = NSEvent.mouseLocation
            let topLeft = NSPoint(x: s.topLeft.x + p.x - s.mouse.x, y: s.topLeft.y + p.y - s.mouse.y)
            (w as? GrabbedWindow)?.grabbed(at: topLeft)
            w.setFrameOrigin(NSPoint(x: topLeft.x, y: topLeft.y - w.frame.height))
        }

        override func mouseUp(with e: NSEvent) {
            guard let s = start else { return }
            start = nil
            window?.showPointerCursor()
            (window as? GrabbedWindow)?.grabbed(at: nil)
            if let w = window, NSPoint(x: w.frame.minX, y: w.frame.maxY) != s.topLeft { moved() } else if let clicked { reported = true; clicked() }
        }
    }
}

/// A window that places itself (in a corner, at a size that eases) and must hold still under
/// the pointer while `WindowDragArea` moves it: told the top-left corner at each step of a
/// drag, and nil once the mouse is let go.
public protocol GrabbedWindow: NSWindow {
    func grabbed(at topLeft: NSPoint?)
}

extension EnvironmentValues {
    /// The mark is drawn still above the faces while one becomes another (`CardFaceStack`), so
    /// each face's own mark stays hidden.
    @Entry var cardMarkHeld = false
    /// Whether the typing dots move; off while the card is hidden, and in snapshot tests.
    @Entry public var typingAnimates = true
}

/// Three dots that blink in turn while an agent is writing, as in the prototype. They move
/// only while shown, and stay still with Reduce Motion.
struct TypingDots: View {
    @Environment(\.typingAnimates) private var animates
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let moving = animates && !reduceMotion
        TimelineView(.animation(paused: !moving)) { t in
            HStack(spacing: 4) {
                ForEach(0..<3) { i in
                    Circle().fill(Brand.secondary).frame(width: 5, height: 5)
                        .opacity(moving ? Self.blink(t.date.timeIntervalSinceReferenceDate - Double(i) * 0.2) : [0.4, 0.7, 1][i])
                }
            }
            .padding(.vertical, 6)
            .padding(.horizontal, 8)
            .background(Brand.row, in: Rectangle())
        }
        .accessibilityHidden(true)
    }

    /// 0.25 → 1 → 0.25 over the first 80% of a 1.2 s cycle.
    static func blink(_ time: Double) -> Double {
        let f = (time / 1.2).truncatingRemainder(dividingBy: 1)
        let x = f < 0 ? f + 1 : f
        guard x < 0.8 else { return 0.25 }
        return 0.25 + 0.75 * (1 - abs(x - 0.4) / 0.4)
    }
}

/// Floats just below the card while the card's Session has more Pings coming.
public struct TypingBadge: View {

    public init() {}

    public var body: some View {
        HStack(spacing: 8) {
            TypingDots()
            Text("Asking more questions")
                .brandStyle(.secondary)
                .foregroundStyle(Brand.secondary)
        }
        .padding(.vertical, 4)
        .padding(.leading, 5)
        .padding(.trailing, 12)
        .background(Brand.surface, in: Rectangle())
        .accessibilityElement(children: .combine)
    }
}

/// The card for the Inbox's current Ping, wired to the Inbox, with its changes sequenced by
/// a CardStage that the owner keeps in step with `inbox.scene`.
public struct InboxCard: View {
    var inbox: Inbox
    var stage: CardStage
    var voice: VoiceSession?
    /// The card's drawn box, easing to the stage's height between Pings; nil fills the card.
    @State private var box: CGFloat?
    /// Runs until the box's ease is over.
    @State private var settling: Task<Void, Never>?
    var moved: () -> Void
    var putBack: () -> Void
    /// The card's window is pinned by its bottom edge (a bottom corner), so the box's top
    /// edge moves as it eases instead of its bottom.
    var anchorsBottom: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.cardContext) private var ctx
    @Environment(\.linkOpener) private var clicks

    public init(inbox: Inbox, stage: CardStage, voice: VoiceSession? = nil, anchorsBottom: Bool = false,
                moved: @escaping () -> Void = {}, putBack: @escaping () -> Void = {}) {
        self.inbox = inbox
        self.stage = stage
        self.voice = voice
        self.anchorsBottom = anchorsBottom
        self.moved = moved
        self.putBack = putBack
    }

    /// How far the drawn box is from the content's height while it eases.
    private var lag: CGFloat { box.map { $0 - (stage.height ?? $0) } ?? 0 }

    /// The card is at rest: no change of content is in flight and the box has finished easing.
    /// The box is then let go, so the card is simply its content's size.
    private func rest() {
        if stage.visible, settling == nil { box = nil }
    }

    public var body: some View {
        BoxStack(box: box, height: stage.height, gap: 8) {
            if let scene = stage.shown {
                let ping = scene.ping
                // The bar follows the Inbox at once, so it animates on the card still showing.
                // After the last answer the Inbox has no current Ping: the bar then stays where
                // the shown content is, and does not fall back to the start.
                CardView(ping: ping, position: inbox.currentID == nil ? scene.position : inbox.position,
                         count: inbox.deck.count, writing: scene.writing,
                         stage: CardView.Stage(visible: stage.visible, slide: stage.slide, box: box,
                                               measured: { [key = scene.key] in stage.measured($0, for: key) }),
                         refusal: inbox.refusals[ping.id], voice: voice,
                         actions: CardActions(
                    previous: { inbox.previous() },
                    next: { inbox.next() },
                    send: { stage.choose($0); inbox.send($0, for: ping) },
                    handBack: { inbox.handBack(ping) },
                    dismiss: { inbox.dismiss(ping) },
                    hide: { inbox.hide() },
                    moved: moved,
                    putBack: putBack,
                    sendAgain: { inbox.sendAgain(ping, newSession: $0) },
                    leave: { inbox.leave(ping) },
                    draft: { inbox.draft($0, for: ping) },
                    withdraw: { inbox.withdraw(ping) }))
                .id(ping.id) // a fresh reply box and ticks for each Ping
                if inbox.moreComing && !scene.writing {
                    // Only the badge fades in or out; the card is never caught in its animation.
                    TypingBadge()
                        .transition(reduceMotion ? .identity : .opacity.animation(CardMotion.ease(CardStage.Timing.standard.fadeIn)))
                }
            }
        }
        // Pinned by the bottom, everything rides up or down with the box's top edge instead,
        // so the box's bottom stays where the window's is.
        .offset(y: anchorsBottom ? -lag : 0)
        // The box eases to the content's height; the card's reply row and the badge ride its
        // bottom edge on the same animation, so they all move as one.
        // That easing is only for a change of content. At rest the box is let go, so a reply
        // box growing as the human types moves the whole card at once, as one piece.
        .onChange(of: stage.visible) { _, visible in
            if visible { rest() } else { box = stage.height } // held from the fade out, before the content changes
        }
        .onChange(of: stage.height) { _, new in
            guard let new, box != nil else { box = nil; return }
            if reduceMotion { box = new; return }
            withAnimation(CardMotion.easeOut(stage.timing.box)) { box = new }
            settling?.cancel()
            settling = Task {
                try? await Task.sleep(for: stage.timing.box + .milliseconds(40))
                guard !Task.isCancelled else { return }
                settling = nil
                rest()
            }
        }
        .onChange(of: reduceMotion, initial: true) { stage.timing = reduceMotion ? .reduced : .standard }
        .onChange(of: ctx.directory.voice, initial: true) { voice?.on = ctx.directory.voice }
        .onChange(of: ctx.directory.voiceAutoListen, initial: true) { voice?.autoListen = ctx.directory.voiceAutoListen }
        .onChange(of: stage.shown?.ping, initial: true) { voice?.show(stage.shown?.writing == true ? nil : stage.shown?.ping) }
        .onAppear {
            voice?.quickContext = { [directory = ctx.directory] in directory.card(for: $0).ping?.context }
            voice?.actions = VoiceActions(send: { [stage, inbox] pong, ping in stage.choose(pong); inbox.send(pong, for: ping) },
                                          dismiss: { [inbox] in inbox.dismiss($0) },
                                          showContext: { [hover = ctx.hover] ping, open in
                                              hover.top = nil
                                              hover.target = open ? ContextHover.Target(ping: ping, topic: .question) : nil
                                          },
                                          openResource: { [clicks] r, ping in
                                              if r.isLink { Task { await ResourceStore.shared.openLink(r, of: ping, clicks: clicks) } }
                                              else { ResourceStore.shared.open(r, of: ping) }
                                          })
        }
    }
}

/// Shopify Polaris's motion curves, shared by everything on the card that moves.
/// `ease` responds quickly and finishes with control; `easeOut` starts at top speed and
/// settles into place.
enum CardMotion {
    static func ease(_ d: Duration) -> Animation {
        .timingCurve(0.25, 0.1, 0.25, 1, duration: CardStage.Timing.seconds(d))
    }
    static func easeOut(_ d: Duration) -> Animation {
        .timingCurve(0.19, 0.91, 0.38, 1, duration: CardStage.Timing.seconds(d))
    }
    /// The box's ease, for content that moves or changes height as something above it comes and goes.
    static var box: Animation { easeOut(CardStage.Timing.standard.box) }
    /// Content that comes and goes fades in and out on the card's timings.
    static var fade: AnyTransition {
        .asymmetric(insertion: .opacity.animation(ease(CardStage.Timing.standard.fadeIn)),
                    removal: .opacity.animation(ease(CardStage.Timing.standard.fadeOut)))
    }
}

/// The card with the typing badge centred below it. Reports their natural height, but moves
/// the badge with the card's drawn box while the box eases to `height`, in the same layout pass.
private struct BoxStack: Layout {
    var box: CGFloat?
    /// The height the box eases to; nil is the first part's own.
    var height: CGFloat?
    var gap: CGFloat

    var animatableData: CGFloat {
        get { box ?? -1 }
        set { box = newValue < 0 ? nil : newValue }
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let sizes = subviews.map { $0.sizeThatFits(.unspecified) }
        let height = sizes.map(\.height).reduce(0, +) + gap * CGFloat(max(sizes.count - 1, 0))
        return CGSize(width: sizes.map(\.width).max() ?? 0, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for (i, s) in subviews.enumerated() {
            let size = s.sizeThatFits(.unspecified)
            s.place(at: CGPoint(x: bounds.midX - size.width / 2, y: y), proposal: ProposedViewSize(size))
            let lag = i == 0 ? box.map { $0 - (height ?? size.height) } ?? 0 : 0 // how far the box is from its height
            y += size.height + lag + gap
        }
    }
}
