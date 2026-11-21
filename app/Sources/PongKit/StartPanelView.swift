import SwiftUI
import UniformTypeIdentifiers

/// The start panel: start a Session in a Project (prototype `.tool`). The Bar opens into it from
/// its New Session button, and draws the surface and margins around it.
public struct StartPanelView: View {
    @Bindable var model: StartPanelModel
    var actions: Actions
    @FocusState private var typing: Bool
    @State private var selection: TextSelection?
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
                    guard press.modifiers.contains(.shift) else { return .ignored }
                    newLine()
                    return .handled
                }
                .onChange(of: model.text) { _, new in
                    let shown = ListText.bulleted(new)
                    if shown != new { model.text = shown }
                }

            ForEach(model.files, id: \.self) { path in
                FileRow(path: path) { model.remove(file: path) }
                    .padding(.top, 4)
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

    private func newLine() {
        var cursor = model.text.endIndex..<model.text.endIndex
        if case .selection(let r)? = selection?.indices, r.upperBound <= model.text.endIndex { cursor = r }
        let (text, at) = ListText.newLine(in: model.text, at: cursor)
        model.text = text
        selection = TextSelection(insertionPoint: at)
    }

    private var sections: [ProjectMenuSection] {
        ProjectMenuSection.standard(model: model, chooseLogo: actions.chooseLogo)
    }

    private func start() {
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
    private struct FileRow: View {
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
