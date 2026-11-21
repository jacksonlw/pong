import AppKit
import Carbon.HIToolbox
@testable import PongKit
import XCTest

@MainActor
final class ShortcutsTests: XCTestCase {
    private func defaults() -> UserDefaults {
        let name = "shortcuts-\(UUID().uuidString)"
        let d = UserDefaults(suiteName: name)!
        addTeardownBlock { d.removePersistentDomain(forName: name) }
        return d
    }

    private func press(_ code: Int, _ chars: String, _ mods: NSEvent.ModifierFlags) -> NSEvent {
        NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: mods, timestamp: 0, windowNumber: 0,
                         context: nil, characters: chars, charactersIgnoringModifiers: chars,
                         isARepeat: false, keyCode: UInt16(code))!
    }

    /// A key press becomes a shortcut only with Command, Option or Control, so it never takes plain typing.
    func testAKeyPressBecomesAShortcut() throws {
        let s = try XCTUnwrap(Shortcut(event: press(kVK_ANSI_P, "p", [.control, .option, .command])))
        XCTAssertEqual(s.title, "⌃⌥⌘P")
        XCTAssertEqual(s.carbonModifiers, UInt32(controlKey | optionKey | cmdKey))
        XCTAssertNil(Shortcut(event: press(kVK_ANSI_P, "p", [])))
        XCTAssertNil(Shortcut(event: press(kVK_ANSI_P, "P", [.shift])))
        XCTAssertEqual(Shortcut(event: press(kVK_Space, " ", [.option]))?.title, "⌥Space")
    }

    /// Shortcuts are kept in the Mac's defaults and come back in a new store; one taken by another
    /// action moves, and clearing removes it.
    func testShortcutsAreKeptAndMove() throws {
        let d = defaults()
        let store = ShortcutStore(defaults: d)
        var changes = 0
        store.changed = { changes += 1 }
        let s = Shortcut(keyCode: UInt16(kVK_ANSI_M), modifiers: [.control, .option], key: "M")
        store.set(s, for: .toggleShown)
        XCTAssertEqual(ShortcutStore(defaults: d)[.toggleShown], s)
        store.set(s, for: .toggleMuted)
        XCTAssertNil(store[.toggleShown])
        XCTAssertEqual(ShortcutStore(defaults: d).shortcuts, [.toggleMuted: s])
        store.set(nil, for: .toggleMuted)
        XCTAssertEqual(ShortcutStore(defaults: d).shortcuts, [:])
        XCTAssertEqual(changes, 3)
    }

    /// The menu bar menu shows each shortcut beside what it does.
    func testTheMenuShowsShortcuts() {
        let s = Shortcut(keyCode: UInt16(kVK_ANSI_P), modifiers: [.control, .command], key: "P")
        let menu = PongMenu.status(shown: true, muted: false, toggleShown: {}, toggleMuted: {}, settings: {},
                                   shortcuts: [.toggleShown: s])
        XCTAssertEqual(menu.items.map(\.title), ["Hide Pong", "Mute Ping Sounds", "", "Settings…", "", "Quit Pong"])
        XCTAssertEqual(menu.items[0].keyEquivalent, "p")
        XCTAssertEqual(menu.items[0].keyEquivalentModifierMask, [.control, .command])
        XCTAssertEqual(menu.items[1].keyEquivalent, "")
    }
}
