import SwiftUI

/// One section of the Project menu: a title over its controls. The menu shows whatever sections
/// it is given, so a later per-Project setting is one more section, not a change to the menu.
public struct ProjectMenuSection: Identifiable {
    public var title: String
    public var content: (Project) -> AnyView
    public var id: String { title }

    public init<V: View>(_ title: String, @ViewBuilder content: @escaping (Project) -> V) {
        self.title = title
        self.content = { AnyView(content($0)) }
    }

    /// Name, colour and logo, each saved by the Engine as soon as it is changed. `chooseLogo`
    /// asks the human for an image file (an open panel in the app).
    @MainActor
    public static func standard(model: StartPanelModel, chooseLogo: @escaping (Project) -> Void) -> [ProjectMenuSection] {
        [
            ProjectMenuSection("Name") { p in
                NameField(project: p) { name in Task { await model.rename(p.id, to: name) } }
            },
            ProjectMenuSection("Colour") { p in
                Swatches(project: p, palette: model.palette) { c in Task { await model.setColour(p.id, c) } }
            },
            ProjectMenuSection("Logo") { p in
                HStack(spacing: 6) {
                    Button(p.logo == nil ? "Choose Logo…" : "Change Logo…") { chooseLogo(p) }.buttonStyle(.brand)
                    if p.logo != nil {
                        Button("Remove") { Task { await model.removeLogo(p.id) } }.buttonStyle(.brand)
                    }
                }
            },
            ProjectMenuSection("Other Devices") { p in
                VStack(alignment: .leading, spacing: 6) {
                    if p.remote != true {
                        Toggle(isOn: Binding(get: { p.enabled == true }, set: { on in Task { await model.setEnabled(p.id, on) } })) {
                            Text("Take Sessions started on your other Devices")
                        }
                        .toggleStyle(BrandToggleStyle())
                    }
                    Text(p.enabledOn?.isEmpty == false ? "They run on \(p.enabledOn!.joined(separator: ", "))." : "No computer takes them yet.")
                        .brandStyle(.secondary)
                        .foregroundStyle(Brand.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            },
        ]
    }
}

/// The Project menu, opened from the gear on a Project in the Bar's start panel.
struct ProjectMenu: View {
    var project: Project
    var logo: Data?
    var sections: [ProjectMenuSection]
    var problem: String?

    static let width: CGFloat = 268

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 10) {
                ProjectTile(project: project, logo: logo)
                Text(project.name).brandStyle(.answer).lineLimit(1).truncationMode(.tail)
            }
            ForEach(sections) { section in
                VStack(alignment: .leading, spacing: 6) {
                    Text(section.title).brandStyle(.label).foregroundStyle(Brand.secondary)
                    section.content(project)
                }
            }
            if let problem {
                Text(problem)
                    .brandStyle(.secondary)
                    .foregroundStyle(Brand.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(14)
        .frame(width: Self.width, alignment: .leading)
        .background(Brand.surface)
        .foregroundStyle(Brand.text)
    }
}

/// The name, saved on Return and when the menu closes.
private struct NameField: View {
    var project: Project
    var save: (String) -> Void
    @State private var name: String
    @FocusState private var naming: Bool

    init(project: Project, save: @escaping (String) -> Void) {
        self.project = project
        self.save = save
        _name = State(initialValue: project.name)
    }

    var body: some View {
        TextField("", text: $name)
            .brandStyle(.body)
            .focused($naming)
            .brandField(focused: naming)
            .accessibilityLabel("Name")
            .onSubmit { commit() }
            .onDisappear { commit() }
            .onChange(of: project.name) { _, n in name = n }
    }

    private func commit() {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if !n.isEmpty, n != project.name { save(n) }
    }
}

private struct Swatches: View {
    var project: Project
    var palette: [String]
    var choose: (String) -> Void

    var body: some View {
        HStack(spacing: 2) {
            ForEach(palette, id: \.self) { c in
                let chosen = c.caseInsensitiveCompare(project.colour) == .orderedSame
                HoverButton { choose(c) } label: { hover in
                    Rectangle().fill(Color(hexString: c) ?? .gray)
                        .frame(width: 22, height: 22)
                        .overlay {
                            if chosen {
                                Image(systemName: "checkmark").font(Brand.Style.label.font).imageScale(.medium)
                                    .foregroundStyle(ProjectTile.readableInk(on: c))
                            }
                        }
                        .padding(3)
                        .background(hover ? Brand.rowHover : .clear, in: Rectangle())
                }
                .accessibilityLabel("Colour \(c)")
                .accessibilityAddTraits(chosen ? .isSelected : [])
            }
        }
    }
}
