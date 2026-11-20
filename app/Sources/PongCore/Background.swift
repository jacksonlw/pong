import Foundation

// Running in the background: start at login, and a system notification when the
// card cannot be seen. macOS sits behind the small protocols below so the decisions are tested
// with fakes; the real implementations live in the Pong target.

/// What macOS reports about Pong starting at login (mirrors `SMAppService.Status`).
public enum LoginItemStatus: Sendable {
    case enabled, notRegistered, requiresApproval, notFound
}

/// Pong's own login item (`SMAppService.mainApp`).
@MainActor
public protocol LoginItem {
    var status: LoginItemStatus { get }
    func register() throws
    func unregister() throws
}

/// The two first-launch flags, kept by the Engine (`EngineLaunchRecord`).
@MainActor
public protocol LaunchRecord: AnyObject {
    var firstLaunchDone: Bool { get set }
    var toldLoginDisallowed: Bool { get set }
}

/// System notifications (`UNUserNotificationCenter`).
@MainActor
public protocol Notifier: AnyObject {
    /// Asks macOS for permission; true if granted.
    func requestPermission() async -> Bool
    /// Posts a notification for the Ping, playing `sound` with it.
    func post(_ ping: Ping, sound: PingSound?)
    func remove(pingIDs: [String])
}

public enum BackgroundText {
    public static let loginOn = "Pong now starts when you log in. To stop this, open System Settings > General > Login Items and switch Pong off."
    public static let loginDisallowed = "macOS does not allow Pong to start at login. Pong will not ask again. To allow it, open System Settings > General > Login Items and switch Pong on."
    public static let notificationsWhy = "Next, macOS asks whether Pong may send notifications. Pong sends one for each waiting Ping, and only while your screen is locked. Without them, the card still shows every Ping."
}

/// First-launch and every-launch setup. `tell` shows the human a message and returns when
/// they have read it.
@MainActor
public func setUpBackground(login: LoginItem, record: LaunchRecord, notifier: Notifier,
                            tell: (String) async -> Void) async {
    if !record.firstLaunchDone {
        record.firstLaunchDone = true
        var allowed = true
        if login.status != .enabled {
            do { try login.register() } catch { allowed = false }
        }
        if allowed && login.status == .requiresApproval { allowed = false }
        if allowed {
            await tell(BackgroundText.loginOn)
        } else {
            record.toldLoginDisallowed = true
            await tell(BackgroundText.loginDisallowed)
        }
        await tell(BackgroundText.notificationsWhy)
        _ = await notifier.requestPermission()
        return
    }
    if login.status == .requiresApproval && !record.toldLoginDisallowed {
        record.toldLoginDisallowed = true
        await tell(BackgroundText.loginDisallowed)
    }
}

/// Posts a system notification for each waiting Ping while the screen is locked (the card
/// cannot be seen then), never while it is unlocked, and withdraws it once the Ping is no
/// longer waiting. A Ping that just arrived carries its sound on the notification. A Thought
/// never waits, and is never posted even if one did.
@MainActor
public final class LockNotifications {
    private let notifier: Notifier
    private var posted: Set<String> = []

    public init(notifier: Notifier) {
        self.notifier = notifier
    }

    /// Returns the ids of the Pings it posted now; their sounds played with the notification.
    @discardableResult
    public func update(waiting: [Ping], screenLocked: Bool, sounds: [String: PingSound] = [:]) -> Set<String> {
        let ids = Set(waiting.map(\.id))
        let gone = posted.subtracting(ids)
        if !gone.isEmpty {
            notifier.remove(pingIDs: gone.sorted())
            posted.subtract(gone)
        }
        guard screenLocked else { return [] }
        var now: Set<String> = []
        for p in waiting where !posted.contains(p.id) && !p.isThought {
            posted.insert(p.id)
            now.insert(p.id)
            notifier.post(p, sound: sounds[p.id])
        }
        return now
    }
}
