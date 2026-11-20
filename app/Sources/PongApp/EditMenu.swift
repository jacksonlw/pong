import AppKit

/// The main menu, holding only Edit. Pong shows no menu bar, but Cut, Copy, Paste, Select All,
/// Undo and Redo reach a text field only through a menu's key equivalents, so without this the
/// reply boxes take none of their shortcuts.
@MainActor
func editMenu() -> NSMenu {
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

    let main = NSMenu()
    let item = main.addItem(withTitle: "Edit", action: nil, keyEquivalent: "")
    item.submenu = edit
    return main
}
