import AppKit
import PongKit
import SwiftUI

/// The one Settings window. `show()` is the single way in: Settings… in the menu of the right
/// click on the Bar calls it.
@MainActor
final class SettingsWindowController {
    private let client: @Sendable () async -> EngineClient?
    private let login: LoginItem
    private var window: NSWindow?

    init(client: @escaping @Sendable () async -> EngineClient?, login: LoginItem) {
        self.client = client
        self.login = login
    }

    func show() {
        if let window {
            NSApp.activate()
            window.makeKeyAndOrderFront(nil)
            return
        }
        Task { await open() }
    }

    private func open() async {
        guard let c = await client() else { return }
        let model = SettingsModel(api: c, login: login)
        let view = ScrollView {
            SettingsView(model: model, devices: DevicesModel(api: c))
        }
        .frame(width: SettingsView.width)
        .frame(minHeight: 420, idealHeight: 640)
        .task { await model.load() }
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: SettingsView.width, height: 640),
                         styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        w.title = "Pong Settings"
        w.contentView = NSHostingView(rootView: view)
        w.isReleasedWhenClosed = false
        w.center()
        window = w
        NSApp.activate()
        w.makeKeyAndOrderFront(nil)
        print("settings shown")
    }
}
