import AppKit

/// Pong's right-click menu, Settings and Quit. It opens wherever a right click or a Control-click
/// lands in Pong's windows (the Bar, the card, the start panel, a panel beside them) unless
/// what was clicked has a menu of its own, such as a text field or a Resource.
public enum PongMenu {
    public static func make(settings: @escaping () -> Void) -> NSMenu {
        let menu = NSMenu()
        menu.addItem(ActionMenuItem("Settings…", key: ",", action: settings))
        menu.addItem(.separator())
        menu.addItem(ActionMenuItem("Quit Pong", key: "q") { NSApp.terminate(nil) })
        return menu
    }

    /// What a click opens Pong's menu on: the view under it, when the click is a right click or a
    /// Control-click and neither that view nor any view it sits in has a menu for it.
    public static func target(of e: NSEvent) -> NSView? {
        let menuClick = e.type == .rightMouseDown || (e.type == .leftMouseDown && e.modifierFlags.contains(.control))
        guard menuClick, let content = e.window?.contentView else { return nil }
        // The frame view holds the content view and takes the point in the window's coordinates.
        let root = content.superview ?? content
        guard let hit = root.hitTest(e.locationInWindow) else { return nil }
        var view: NSView? = hit
        while let v = view {
            if v.menu(for: e) != nil { return nil }
            view = v.superview
        }
        return hit
    }

    /// Opens Pong's menu on every such click in the app's windows. Keep what it returns.
    @MainActor public static func watch(settings: @escaping () -> Void) -> Any? {
        NSEvent.addLocalMonitorForEvents(matching: [.rightMouseDown, .leftMouseDown]) { e in
            guard let view = target(of: e) else { return e }
            NSMenu.popUpContextMenu(make(settings: settings), with: e, for: view)
            return nil
        }
    }
}
