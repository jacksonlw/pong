import AppKit
import PongKit
import SwiftUI
import UniformTypeIdentifiers

/// The start panel in the Bar: start a Session. The Bar's New Session button opens it, and the
/// card's window takes typing while it is open. It closes when a Session starts, on Escape, on
/// the button again. A click elsewhere leaves it open, with what was typed.
@MainActor @Observable
final class BarStart {
    /// The panel the Bar shows; nil while closed.
    private(set) var view: StartPanelView?
    var isOpen: Bool { model != nil }

    @ObservationIgnored private let client: @Sendable () async -> EngineClient?
    @ObservationIgnored private let window: NSPanel
    /// Told when the panel opens or closes, so the window's face follows.
    @ObservationIgnored private let changed: (Bool) -> Void
    @ObservationIgnored private var model: StartPanelModel?

    init(client: @escaping @Sendable () async -> EngineClient?, window: NSPanel, changed: @escaping (Bool) -> Void) {
        self.client = client
        self.window = window
        self.changed = changed
    }

    func toggle() {
        if isOpen { close() } else { Task { await open() } }
    }

    func open() async {
        guard model == nil, let c = await client(), model == nil else { return }
        let model = StartPanelModel(api: c, drafts: .standard)
        self.model = model
        view = StartPanelView(model: model, actions: .init(
            chooseFolder: { [weak self] in self?.chooseFolder() },
            started: { [weak self] in self?.close() },
            chooseFiles: { [weak self] in self?.chooseFiles() },
            dropped: { [weak self] in self?.takeKey() },
            chooseLogo: { [weak self] p in self?.chooseLogo(for: p) }))
        changed(true)
        takeKey()
        print("start panel shown")
        await model.load()
    }

    /// Typing goes to the panel. The card's window is never key otherwise.
    func takeKey() {
        NSApp.activate()
        window.makeKeyAndOrderFront(nil)
    }

    func close() {
        guard model != nil else { return }
        model = nil
        view = nil
        changed(false)
        // Typing goes back to the app the human was in.
        if window.isKeyWindow { NSApp.deactivate() }
        print("start panel closed")
    }

    private func pick(_ open: NSOpenPanel) -> NSApplication.ModalResponse {
        let response = open.runModal()
        takeKey()
        return response
    }

    private func chooseFolder() {
        let open = NSOpenPanel()
        open.canChooseDirectories = true
        open.canChooseFiles = false
        open.message = "Choose the folder the Session works in."
        if pick(open) == .OK, let url = open.url { model?.choose(folder: url.path) }
    }

    private func chooseFiles() {
        let open = NSOpenPanel()
        open.canChooseDirectories = true
        open.allowsMultipleSelection = true
        open.message = "Choose files for the agent to read."
        if pick(open) == .OK { model?.add(files: open.urls.map(\.path)) }
    }

    private func chooseLogo(for project: Project) {
        let open = NSOpenPanel()
        open.allowedContentTypes = [.png, .jpeg, .svg]
        open.message = "Choose an image as the logo of \(project.name)."
        if pick(open) == .OK, let url = open.url, let data = try? Data(contentsOf: url), let model {
            Task { await model.setLogo(project.id, image: data) }
        }
        // The Project menu closed when the picker took key: it opens again, showing the new logo.
        model?.menuOpen = project.id
    }
}
