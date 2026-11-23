import AppKit
import PongCore
import PongKit

/// Pong's menu bar item: show or hide the card and the Bar, mute the Ping sound, Settings and Quit.
@MainActor
final class StatusItem: NSObject, NSMenuDelegate {
    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let card: CardController
    private let directory: Directory
    private let client: @Sendable () async -> EngineClient?
    private let settings: () -> Void
    private let shortcuts: ShortcutStore
    private var hotKeys: HotKeys?
    /// The Ping sound to go back to when the human unmutes.
    private static let unmutedKey = "pingSoundBeforeMute"

    init(card: CardController, directory: Directory, client: @escaping @Sendable () async -> EngineClient?,
         settings: @escaping () -> Void, shortcuts: ShortcutStore) {
        self.card = card
        self.directory = directory
        self.client = client
        self.settings = settings
        self.shortcuts = shortcuts
        super.init()
        let keys = HotKeys { [weak self] in self?.perform($0) }
        keys.register(shortcuts.shortcuts)
        shortcuts.changed = { [weak keys, shortcuts] in keys?.register(shortcuts.shortcuts) }
        hotKeys = keys
        item.button?.image = PongMenu.menuBarImage
        item.button?.toolTip = "Pong"
        item.menu = NSMenu()
        item.menu?.delegate = self
    }

    /// The menu is built as it opens, so it shows the card and the sound as they are now.
    func menuNeedsUpdate(_ menu: NSMenu) {
        let fresh = PongMenu.status(shown: !card.hidden, muted: directory.pingSound == .off,
                                    toggleShown: { [card] in card.set(hidden: !card.hidden) },
                                    toggleMuted: { [weak self] in self?.toggleMute() },
                                    settings: settings, shortcuts: shortcuts.shortcuts)
        menu.removeAllItems()
        for i in fresh.items {
            fresh.removeItem(i)
            menu.addItem(i)
        }
    }

    private func perform(_ a: ShortcutAction) {
        switch a {
        case .toggleShown: card.set(hidden: !card.hidden)
        case .toggleMuted: toggleMute()
        case .speak: Hearing.speak.shortcut(openStart: { [card] in card.openStart() })
        }
    }

    private func toggleMute() {
        let now = directory.pingSound
        let next: PingSoundMode
        if now == .off {
            next = UserDefaults.standard.string(forKey: Self.unmutedKey).flatMap(PingSoundMode.init) ?? .first
        } else {
            UserDefaults.standard.set(now.rawValue, forKey: Self.unmutedKey)
            next = .off
        }
        let client = client
        Task { _ = try? await client()?.changeSettings(SettingsChange(pingSound: next)) }
    }
}
