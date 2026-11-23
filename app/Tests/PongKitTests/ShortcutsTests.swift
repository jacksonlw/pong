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
        let speak = try XCTUnwrap(ShortcutAction.speak.standard)
        XCTAssertEqual(ShortcutStore(defaults: d).shortcuts, [.toggleMuted: s, .speak: speak])
        store.set(nil, for: .toggleMuted)
        XCTAssertEqual(ShortcutStore(defaults: d).shortcuts, [.speak: speak])
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

    /// Speak has ⌥⌘Space until the human changes or clears it; cleared, it stays cleared.
    func testSpeakHasAStandardShortcutThatCanBeCleared() throws {
        let d = defaults()
        let store = ShortcutStore(defaults: d)
        XCTAssertEqual(store[.speak]?.title, "⌥⌘Space")
        XCTAssertNil(store[.toggleShown], "other actions have none until set")
        store.set(nil, for: .speak)
        XCTAssertNil(ShortcutStore(defaults: d)[.speak])
        let s = Shortcut(keyCode: UInt16(kVK_ANSI_S), modifiers: [.control, .option], key: "S")
        store.set(s, for: .speak)
        XCTAssertEqual(ShortcutStore(defaults: d)[.speak], s)
    }

    /// The standard shortcut steps aside when the human gave the same keys to another action.
    func testSpeaksStandardShortcutNeverTakesAnothersKeys() throws {
        let d = defaults()
        let store = ShortcutStore(defaults: d)
        store.set(ShortcutAction.speak.standard, for: .toggleShown)
        let again = ShortcutStore(defaults: d)
        XCTAssertEqual(again[.toggleShown], ShortcutAction.speak.standard)
        XCTAssertNil(again[.speak])
    }
}
