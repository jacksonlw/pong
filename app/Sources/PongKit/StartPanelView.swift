import SwiftUI
import UniformTypeIdentifiers

/// The start panel: start a Session in a Project (prototype `.tool`). The Bar opens into it from
/// its New Session button, and draws the surface and margins around it.
public struct StartPanelView: View {
    @Bindable var model: StartPanelModel
    var actions: Actions
    @FocusState private var typing: Bool
    @State private var selection: TextSelection?
    @Environment(Speak.self) private var speak: Speak?
    @Environment(SkillNames.self) private var skillNames: SkillNames?
    /// Which of the Skill menu's names ↑ and ↓ have reached.
    @State private var menuIndex = 0
    /// The query Escape closed the menu on, so it stays closed until something else is typed.
    @State private var menuClosed: SkillCompletion.Query?

    /// Where the Skills are read: the chosen Harness in the chosen Project or folder.
    private var skillPlace: SkillNames.Place? {
        guard let harness = model.harness else { return nil }
        switch model.place {
        case .project(let id)?: return SkillNames.Place(harness: harness, project: id, computer: model.chosenComputer)
        case .folder(let path)?: return SkillNames.Place(harness: harness, folder: path)
        case nil: return nil
        }
    }

    /// The Skill menu for the name typed before the caret, while one is.
    private var skillMenu: SkillMenuState? {
        let offer = skillNames?.offer(skillPlace) ?? .none
        guard let trigger = offer.trigger, cursor.isEmpty else { return nil }
        let at = model.text.utf16.distance(from: model.text.startIndex, to: cursor.lowerBound)
        guard let q = SkillCompletion.query(in: model.text, caret: at, trigger: trigger), q != menuClosed else { return nil }
        let items = SkillCompletion.matches(q.typed, in: offer.names)
        return items.isEmpty ? nil : SkillMenuState(query: q, items: items, index: min(menuIndex, items.count - 1))
    }

    private func pickSkill(_ name: String) {
        guard let m = skillMenu, let trigger = skillNames?.offer(skillPlace).trigger else { return }
        let (text, at) = SkillCompletion.pick(name, for: m.query, in: model.text, trigger: trigger)
        model.text = text
        let i = text.utf16.index(text.startIndex, offsetBy: at)
        selection = TextSelection(insertionPoint: i)
        menuIndex = 0
    }

    /// Moves through the open menu; false when none is open.
    private func moveInMenu(_ by: Int) -> Bool {
        guard let m = skillMenu else { return false }
        menuIndex = (m.index + by + m.items.count) % m.items.count
        return true
    }
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var fade: AnyTransition { reduceMotion ? .identity : CardMotion.fade }

    /// What the panel's controls do outside it.
    public struct Actions {
        public var chooseFolder: () -> Void = {}
        public var started: () -> Void = {}
        /// Asks the human for files to add for the agent.
        public var chooseFiles: () -> Void = {}
        /// Files were dropped on the panel, which may not be the key window then.
        public var dropped: () -> Void = {}
        /// Asks the human for a logo image for a Project, from its Project menu.
        public var chooseLogo: (Project) -> Void = { _ in }
        public init(chooseFolder: @escaping () -> Void = {}, started: @escaping () -> Void = {},
                    chooseFiles: @escaping () -> Void = {}, dropped: @escaping () -> Void = {},
                    chooseLogo: @escaping (Project) -> Void = { _ in }) {
            self.chooseFolder = chooseFolder
            self.started = started
            self.chooseFiles = chooseFiles
            self.dropped = dropped
            self.chooseLogo = chooseLogo
        }
    }

    public init(model: StartPanelModel, actions: Actions = Actions()) {
        self.model = model
        self.actions = actions
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            label("New Session")
            if model.projects.isEmpty {
                Text("No Projects yet.")
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.bottom, 8)
            }
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 8), GridItem(.flexible(), spacing: 8)], spacing: 8) {
                ForEach(model.projects, id: \.id) { project in
                    ProjectButton(project: project, model: model, sections: sections) {
                        model.choose(project: project.id)
                        typing = true
                    }
                }
                tile(selected: model.folderName != nil, action: actions.chooseFolder) {
                    Image(systemName: "folder").imageScale(.large).brandIcon().frame(width: 22, height: 22)
                    Text(model.folderName ?? "Other folder…").allowsTightening(true)
                }
            }
            .padding(.bottom, 12)

            if let placement = model.placement {
                ComputerChoice(placement: placement, chosen: model.chosenComputer) { model.computer = $0 }
                    .padding(.bottom, 10)
                    .transition(fade)
            }

            // Return starts the Session; Shift-Return starts a new line, and the next item in a list.
            TextField("", text: $model.text, selection: $selection, axis: .vertical)
                .lineLimit(3...6)
                .brandFieldLines(model.text, 3...6)
                .brandPlaceholder("What should the agent do?", shown: model.text.isEmpty)
                .brandStyle(.body)
                .focused($typing)
                .brandField(focused: typing)
                .onSubmit(start)
                .onKeyPress(.return, phases: .down) { press in
                    if press.modifiers.isEmpty, let m = skillMenu {
                        pickSkill(m.items[m.index])
                        return .handled
                    }
                    guard press.modifiers.contains(.shift) else { return .ignored }
                    newLine()
                    return .handled
                }
                .onKeyPress(.tab, phases: .down) { _ in
                    guard let m = skillMenu else { return .ignored }
                    pickSkill(m.items[m.index])
                    return .handled
                }
                // ↑ on the first line steps back through the Project's recent prompts; ↓ on the
                // last line steps forward again.
                .onKeyPress(.escape, phases: .down) { _ in
                    if let m = skillMenu {
                        menuClosed = m.query
                        return .handled
                    }
                    guard speak?.box == .start else { return .ignored }
                    speak?.stop()
                    return .handled
                }
                .onKeyPress(.upArrow, phases: .down) { press in
                    if press.modifiers.isEmpty, moveInMenu(-1) { return .handled }
                    guard press.modifiers.isEmpty, !model.text[..<cursor.lowerBound].contains("\n"),
                          model.recallOlder() else { return .ignored }
                    selection = TextSelection(insertionPoint: model.text.startIndex)
                    return .handled
                }
                .onKeyPress(.downArrow, phases: .down) { press in
                    if press.modifiers.isEmpty, moveInMenu(1) { return .handled }
                    guard press.modifiers.isEmpty, !model.text[cursor.upperBound...].contains("\n"),
                          model.recallNewer() else { return .ignored }
                    selection = TextSelection(insertionPoint: model.text.endIndex)
                    return .handled
                }
                .onChange(of: model.text) { _, new in
                    let shown = ListText.bulleted(new)
                    if shown != new { model.text = shown }
                    if skillMenu == nil { menuIndex = 0 }
                }
                .overlay(alignment: .topLeading) {
                    if let m = skillMenu {
                        SkillMenu(state: m, trigger: skillNames?.offer(skillPlace).trigger ?? "/") { pickSkill($0) }
                            .alignmentGuide(.top) { $0[.bottom] + 4 }
                    }
                }
                .task(id: skillPlace) { await skillNames?.load(skillPlace) }

            ForEach(model.files, id: \.self) { path in
                FileRow(path: path) { model.remove(file: path) }
                    .padding(.top, 4)
                    .transition(fade)
            }

            if let problem = speak?.problem, problem.box == .start {
                Text(problem.text)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.warning)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 8)
                    .transition(fade)
            }

            if let problem = model.problem {
                Text(problem)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.warning)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 8)
                    .accessibilityLabel("Could not start: \(problem)")
                    .transition(fade)
            }

            HStack(spacing: 8) {
                HoverButton(action: actions.chooseFiles) { hover in
                    Image(systemName: "plus").imageScale(.large)
                        .font(Brand.Style.label.font)
                        .brandIcon()
                        .frame(width: 40, height: 40)
                        .background(hover ? Brand.rowHover : Brand.row, in: Rectangle())
                        .contentShape(Rectangle())
                }
                .help("Add files for the agent to read")
                .accessibilityLabel("Add files")
                startButton
                if let speak { speakTile(speak) }
            }
            .padding(.top, 10)
        }
        .padding(.top, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .foregroundStyle(Brand.text)
        // What sits below a file, problem or computer choice that comes or goes eases to its
        // new place as the Bar's surface eases to the panel's new height.
        .animation(reduceMotion ? nil : CardMotion.box, value: model.files)
        .animation(reduceMotion ? nil : CardMotion.box, value: model.problem)
        .animation(reduceMotion ? nil : CardMotion.box, value: model.placement)
        .onAppear { typing = true }
        .onAppear {
            let model = model
            speak?.attach(.start, read: { model.text }, write: { model.text = $0 },
                          caret: { model.text.utf16.count })
        }
        .onChange(of: typing) { _, now in speak?.focus(.start, now) }
        .onDisappear { speak?.detach(.start) }
        .onDrop(of: [.fileURL], isTargeted: nil) { providers in
            actions.dropped()
            for p in providers {
                _ = p.loadObject(ofClass: URL.self) { url, _ in
                    guard let url else { return }
                    Task { @MainActor in model.add(files: [url.path]) }
                }
            }
            return true
        }
    }

    /// Speak's microphone, a tile like Add files', right of Start Session.
    private func speakTile(_ speak: Speak) -> some View {
        let on = speak.box == .start
        return HoverButton(action: {
            speak.toggle(.start, text: $model.text, caret: model.text.utf16.distance(from: model.text.startIndex, to: cursor.lowerBound))
        }) { hover in
            ZStack {
                if on {
                    LevelMeter(level: speak.level)
                } else {
                    Image(systemName: "mic").imageScale(.large)
                        .font(Brand.Style.label.font)
                        .brandIcon()
                }
            }
            .frame(width: 40, height: 40)
            .background(hover ? Brand.rowHover : Brand.row, in: Rectangle())
            .foregroundStyle(Brand.text)
            .contentShape(Rectangle())
        }
        .help(on ? "Stop. What you said stays in the box." : "Speak into the box. Nothing is sent until you start the Session.")
        .accessibilityLabel(on ? "Stop speaking" : "Speak")
    }

    /// The Session starts in the Harness last used in the Project, else the first one offered.
    private var startButton: some View {
        HoverButton(action: start) { hover in
            Text(model.starting ? "Starting…" : "Start Session")
                .brandStyle(.answer)
                .frame(maxWidth: .infinity, minHeight: 40)
                .foregroundStyle(model.canStart ? Brand.onSelected : Brand.secondary)
                .background(Rectangle().fill(model.canStart ? (hover ? Brand.selectedHover : Brand.selected) : Brand.row))
                .contentShape(Rectangle())
        }
        .disabled(!model.canStart)
    }

    /// The selected range, or the end of the text when the field has not said.
    private var cursor: Range<String.Index> {
        if case .selection(let r)? = selection?.indices, r.upperBound <= model.text.endIndex { return r }
        return model.text.endIndex..<model.text.endIndex
    }

    private func newLine() {
        let (text, at) = ListText.newLine(in: model.text, at: cursor)
        model.text = text
        selection = TextSelection(insertionPoint: at)
    }

    private var sections: [ProjectMenuSection] {
        ProjectMenuSection.standard(model: model, chooseLogo: actions.chooseLogo)
    }

    private func start() {
        speak?.stop(.start)
        Task { if await model.start() { actions.started() } }
    }

    private func label(_ s: String) -> some View {
        Text(s).brandStyle(.label).foregroundStyle(Brand.secondary).padding(.bottom, 6)
    }

    private func tile<C: View>(selected: Bool, action: @escaping () -> Void, @ViewBuilder content: () -> C) -> some View {
        Tile(selected: selected, action: action, content: content())
    }

    /// A Project's tile, with the gear that opens its Project menu. The gear shows while the tile
    /// is hovered or has keyboard focus, and while the menu is open; Control-Return on the
    /// focused tile opens the menu too.
    private struct ProjectButton: View {
        var project: Project
        @Bindable var model: StartPanelModel
        var sections: [ProjectMenuSection]
        var choose: () -> Void
        @State private var hover = false
        @FocusState private var focused: Bool

        private var open: Bool { model.menuOpen == project.id }
        private var selected: Bool { model.place == .project(project.id) }

        var body: some View {
            Tile(selected: selected, action: choose, trailingRoom: 22, content: Group {
                ProjectTile(project: project, logo: model.logos[project.id])
                Text(project.name)
            })
            .focused($focused)
            .onKeyPress(.return, phases: .down) { press in
                guard press.modifiers.contains(.control) else { return .ignored }
                model.menuOpen = project.id
                return .handled
            }
            .accessibilityAction(named: "Project menu") { model.menuOpen = project.id }
            .overlay(alignment: .trailing) {
                if hover || focused || open {
                    IconButton(symbol: "gearshape", label: "Project menu for \(project.name)",
                               colour: selected ? Brand.onSelected : nil,
                               hoverFill: selected ? Brand.selectedHover : Brand.rowHover) { model.menuOpen = project.id }
                    .focusable(false)
                }
            }
            .hoverHighlight($hover)
            .popover(isPresented: Binding(get: { open }, set: { if !$0, open { model.menuOpen = nil } }), arrowEdge: .trailing) {
                if let p = model.projects.first(where: { $0.id == project.id }) {
                    ProjectMenu(project: p, logo: model.logos[p.id], sections: sections, problem: model.problem)
                }
            }
        }
    }

    private struct Tile<C: View>: View {
        var selected: Bool
        var action: () -> Void
        var trailingRoom: CGFloat = 0
        var content: C
        @State private var hover = false

        var body: some View {
        Button(action: action) {
            HStack(spacing: 10) { content }
                .brandStyle(.answer)
                .lineLimit(1)
                .truncationMode(.tail)
                .foregroundStyle(selected ? Brand.onSelected : Brand.text)
                .padding(.horizontal, 10)
                .padding(.trailing, trailingRoom)
                .frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                .background(Rectangle().fill(selected ? Brand.selected : hover ? Brand.rowHover : Brand.row))
                .contentShape(Rectangle())
        }
        .buttonStyle(FocusedPlain())
        .hoverHighlight($hover)
        .accessibilityAddTraits(selected ? .isSelected : [])
        }
    }

    /// A file the human added, with the button that takes it out again.
    struct FileRow: View {
        var path: String
        var remove: () -> Void

        var body: some View {
            let name = URL(fileURLWithPath: path).lastPathComponent
            HStack(spacing: 8) {
                Image(systemName: "doc").imageScale(.large).font(Brand.Style.label.font).brandIcon().foregroundStyle(Brand.secondary)
                Text(name).brandStyle(.label).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 0)
                IconButton(symbol: "xmark", label: "Remove \(name)", help: path, hoverFill: Brand.rowHover, action: remove)
            }
            .padding(.leading, 10)
            .frame(minHeight: 26)
            .background(Brand.row, in: Rectangle())
            .help(path)
        }
    }

}

/// The computer a new Session will start on, Pong's choice first, which the human can change.
/// When none can take it, each computer's reason.
struct ComputerChoice: View {
    var placement: Placement
    var chosen: String?
    var choose: (String) -> Void

    var body: some View {
        let eligible = placement.computers.filter(\.eligible)
        VStack(alignment: .leading, spacing: 6) {
            if eligible.isEmpty {
                Text("No computer can take this Session.").brandStyle(.secondary).foregroundStyle(Brand.text)
                ForEach(placement.computers) { c in
                    Text("\(c.name): \((c.reasons ?? []).joined(separator: "; ")).")
                        .brandStyle(.secondary)
                        .foregroundStyle(Brand.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            } else {
                HStack(spacing: 8) {
                    Text("Runs on").brandStyle(.secondary).foregroundStyle(Brand.secondary)
                    BrandSegments(options: eligible.map { (Optional($0.id), $0.name) },
                                  selection: Binding(get: { chosen }, set: { if let id = $0 { choose(id) } }))
                        .fixedSize()
                        .accessibilityLabel("Computer")
                }
                if let w = eligible.first(where: { $0.id == chosen })?.warning {
                    Text(w).brandStyle(.secondary).foregroundStyle(Brand.warning).fixedSize(horizontal: false, vertical: true)
                }
            }
        }
    }
}
