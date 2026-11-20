import AppKit
import ServiceManagement
import UserNotifications
import PongKit

// The real macOS side of running in the background. Wired only when running as the real
// bundle without PONG_HOME (see main.swift), so development and tests never register a login
// item, ask for a permission or post a notification.

@MainActor
struct MainAppLoginItem: LoginItem {
    var status: LoginItemStatus {
        switch SMAppService.mainApp.status {
        case .enabled: .enabled
        case .requiresApproval: .requiresApproval
        case .notFound: .notFound
        default: .notRegistered
        }
    }
    func register() throws { try SMAppService.mainApp.register() }
    func unregister() throws { try SMAppService.mainApp.unregister() }
}

/// Stands in for the login item under PONG_HOME and outside the real bundle, so development
/// never registers one; Settings' switch flips it in memory only.
@MainActor
final class PretendLoginItem: LoginItem {
    var status: LoginItemStatus = .notRegistered
    func register() throws { status = .enabled }
    func unregister() throws { status = .notRegistered }
}

/// Posts through UNUserNotificationCenter; opening a notification brings up the card on its Ping.
@MainActor
final class SystemNotifier: NSObject, Notifier, @preconcurrency UNUserNotificationCenterDelegate {
    private let center = UNUserNotificationCenter.current()
    private let open: (String) -> Void

    init(open: @escaping (String) -> Void) {
        self.open = open
        super.init()
        center.delegate = self
    }

    func requestPermission() async -> Bool {
        (try? await center.requestAuthorization(options: [.alert, .sound])) ?? false
    }

    func post(_ ping: Ping, sound: PingSound?) {
        let c = UNMutableNotificationContent()
        let card = CardContext.live.directory.card(for: ping)
        c.title = card.project?.name
            ?? ping.folder.map { URL(fileURLWithPath: $0).lastPathComponent }
            ?? card.session?.title ?? "Pong"
        c.body = ping.question
        c.userInfo = ["ping": ping.id]
        // The file is in the app's own Resources (scripts/build-app.sh); macOS plays it at the
        // alert volume and keeps it quiet under Focus.
        c.sound = sound.map { UNNotificationSound(named: UNNotificationSoundName($0.fileName)) }
        center.add(UNNotificationRequest(identifier: ping.id, content: c, trigger: nil))
    }

    func remove(pingIDs: [String]) {
        center.removeDeliveredNotifications(withIdentifiers: pingIDs)
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        if let id = response.notification.request.content.userInfo["ping"] as? String { open(id) }
        done()
    }
}

/// Starts login-item setup and lock-screen notifications for the real app.
@MainActor
final class Background {
    private var notifier: SystemNotifier!
    private var lock: LockNotifications!
    private let inbox: Inbox
    private var screenLocked = false

    init(inbox: Inbox, client: @escaping @Sendable () async -> EngineClient?) {
        self.inbox = inbox
        notifier = SystemNotifier { id in inbox.show(id) }
        lock = LockNotifications(notifier: notifier)

        // Screen lock: the distributed notifications loginwindow posts. Apple does not document
        // them; there is no documented lock notification for apps.
        let dnc = DistributedNotificationCenter.default()
        for (name, locked) in [("com.apple.screenIsLocked", true), ("com.apple.screenIsUnlocked", false)] {
            dnc.addObserver(forName: .init(name), object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.screenLocked = locked; _ = self?.update() }
            }
        }

        let notifier = notifier!
        Task {
            // Without the Engine's record this launch skips setup rather than redo the first launch.
            guard let c = await client(), let record = try? await EngineLaunchRecord.load(c) else { return }
            await setUpBackground(login: MainAppLoginItem(), record: record, notifier: notifier) { text in
                let a = NSAlert()
                a.messageText = "Pong"
                a.informativeText = text
                NSApp.activate()
                a.runModal()
            }
        }
    }

    /// Called by the app whenever the waiting Pings change, with the sounds of those that just
    /// arrived. Returns the ids it posted a notification for, which carried their sound.
    func update(sounds: [String: PingSound] = [:]) -> Set<String> {
        lock.update(waiting: inbox.waiting, screenLocked: screenLocked, sounds: sounds)
    }
}
