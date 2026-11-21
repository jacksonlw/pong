import AppKit

/// The main menu: Settings… and Edit. Pong shows no menu bar, but a menu's key equivalents are
/// how keys reach the app: ⌘, opens Settings while one of Pong's panels has focus, and Cut, Copy,
/// Paste, Select All, Undo and Redo reach a text field only this way, so without it the reply
/// boxes take none of their shortcuts.
@MainActor
func mainMenu(settings: @escaping () -> Void) -> NSMenu {
    let main = NSMenu()

    let app = NSMenu(title: "Pong")
    let open = ClosureMenuItem(title: "Settings…", key: ",", action: settings)
    app.addItem(open)
    main.addItem(withTitle: "Pong", action: nil, keyEquivalent: "").submenu = app

    let edit = NSMenu(title: "Edit")
    func add(_ title: String, _ action: Selector, _ key: String, _ modifiers: NSEvent.ModifierFlags = .command) {
        let item = edit.addItem(withTitle: title, action: action, keyEquivalent: key)
        item.keyEquivalentModifierMask = modifiers
    }
    add("Undo", Selector(("undo:")), "z")
    add("Redo", Selector(("redo:")), "z", [.command, .shift])
    edit.addItem(.separator())
    add("Cut", #selector(NSText.cut(_:)), "x")
    add("Copy", #selector(NSText.copy(_:)), "c")
    add("Paste", #selector(NSText.paste(_:)), "v")
    add("Select All", #selector(NSText.selectAll(_:)), "a")
    main.addItem(withTitle: "Edit", action: nil, keyEquivalent: "").submenu = edit
    return main
}

/// A menu item that runs a closure.
private final class ClosureMenuItem: NSMenuItem {
    private let run: () -> Void

    init(title: String, key: String, action: @escaping () -> Void) {
        run = action
        super.init(title: title, action: #selector(fire), keyEquivalent: key)
        target = self
    }

    required init(coder: NSCoder) { fatalError("not used") }

    @objc private func fire() { run() }
}
