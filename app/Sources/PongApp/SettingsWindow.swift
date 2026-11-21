import AppKit
import PongKit
import SwiftUI

/// The one Settings panel, in the row beside the card. `show()` is the single way in: Settings…
/// in the menu of the right click on the Bar calls it.
@MainActor
final class SettingsWindowController {
    private let client: @Sendable () async -> EngineClient?
    private let login: LoginItem
    private let dock: Dock
    private let shortcuts: ShortcutStore
    private var panel: DockPanel?
    private static let key = "settings"
    static let height: CGFloat = 640

    init(client: @escaping @Sendable () async -> EngineClient?, login: LoginItem, dock: Dock, shortcuts: ShortcutStore) {
        self.shortcuts = shortcuts
        self.client = client
        self.login = login
        self.dock = dock
    }

    func show() {
        if let panel { return panel.orderFrontRegardless() }
        Task { await open() }
    }

    private func open() async {
        guard panel == nil, let c = await client(), panel == nil else { return }
        let model = SettingsModel(api: c, login: login)
        let dock = dock
        let view = VStack(spacing: 0) {
            PanelTitle(symbol: "gearshape", title: "Settings", closeLabel: "Close Settings",
                       moved: { dock.dropped(Self.key) }, putBack: { dock.putBack(Self.key) },
                       onClose: { [weak self] in self?.close() })
            ScrollView {
                SettingsView(model: model, devices: DevicesModel(api: c), shortcuts: shortcuts)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Brand.page)
        .task { await model.load() }
        let p = DockPanel.make(size: CGSize(width: SettingsView.width, height: Self.height))
        let host = FirstMouseHostingView(rootView: view)
        host.sizingOptions = [] // the human sizes the panel by its edges
        p.host(host, minimum: CGSize(width: 360, height: 320))
        panel = p
        dock.show(Self.key, p, closed: { [weak self] in self?.close() })
        print("settings shown")
    }

    private func close() {
        guard panel != nil else { return }
        panel = nil
        dock.remove(Self.key)
    }
}
