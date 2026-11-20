import XCTest
@testable import PongKit

@MainActor
final class FakeLogin: LoginItem {
    var status: LoginItemStatus
    var denies = false
    var registrations = 0
    init(_ s: LoginItemStatus) { status = s }
    func register() throws {
        registrations += 1
        if denies { status = .requiresApproval; throw NSError(domain: "SMAppServiceErrorDomain", code: 1) }
        status = .enabled
    }
    func unregister() throws { status = .notRegistered }
}

@MainActor
final class FakeRecord: LaunchRecord {
    var firstLaunchDone = false
    var toldLoginDisallowed = false
}

@MainActor
final class FakeNotifier: Notifier {
    var events: [String] = []
    var grant = false
    func requestPermission() async -> Bool { events.append("ask"); return grant }
    func post(_ ping: Ping, sound: PingSound?) { events.append("post \(ping.id)" + (sound.map { " \($0.rawValue)" } ?? "")) }
    func remove(pingIDs: [String]) { events.append("remove \(pingIDs.joined(separator: ","))") }
}

func ping(_ id: String) -> Ping {
    Ping(id: id, session: "s", folder: nil, question: "Ship \(id)?", answers: nil, isNote: nil, state: "waiting")
}

@MainActor
final class BackgroundTests: XCTestCase {
    func run(_ login: FakeLogin, _ record: FakeRecord, _ n: FakeNotifier) async -> [String] {
        var log: [String] = []
        await setUpBackground(login: login, record: record, notifier: n) { msg in
            log.append(msg); n.events.append("tell")
        }
        return log
    }

    func testFirstLaunchRegistersOnceAndSaysWhereToTurnItOff() async {
        let login = FakeLogin(.notRegistered), record = FakeRecord(), n = FakeNotifier()
        let told = await run(login, record, n)
        XCTAssertEqual(login.status, .enabled)
        XCTAssertEqual(told.first, BackgroundText.loginOn)
        XCTAssertTrue(told[0].contains("System Settings > General > Login Items"))

        let again = await run(login, record, n)
        XCTAssertEqual(login.registrations, 1)
        XCTAssertEqual(again, [])
    }

    func testDisallowedIsSaidOnceAndNotAskedAgain() async {
        let login = FakeLogin(.notRegistered), record = FakeRecord(), n = FakeNotifier()
        login.denies = true
        let told = await run(login, record, n)
        XCTAssertEqual(told.first, BackgroundText.loginDisallowed)
        for _ in 0..<3 {
            let again = await run(login, record, n)
            XCTAssertEqual(again, [])
        }
        XCTAssertEqual(login.registrations, 1)
    }

    func testTurnedOffLaterIsSaidOnce() async {
        let login = FakeLogin(.notRegistered), record = FakeRecord(), n = FakeNotifier()
        _ = await run(login, record, n)
        login.status = .requiresApproval // the human switched it off in System Settings
        let first = await run(login, record, n)
        let second = await run(login, record, n)
        XCTAssertEqual(first, [BackgroundText.loginDisallowed])
        XCTAssertEqual(second, [])
        XCTAssertEqual(login.registrations, 1)
    }

    func testNotificationPermissionIsExplainedBeforeItIsRequested() async {
        let login = FakeLogin(.notRegistered), record = FakeRecord(), n = FakeNotifier()
        let told = await run(login, record, n)
        XCTAssertEqual(told.last, BackgroundText.notificationsWhy)
        XCTAssertEqual(n.events.suffix(2), ["tell", "ask"])
        _ = await run(login, record, n)
        XCTAssertEqual(n.events.filter { $0 == "ask" }.count, 1)
    }

    func testNotificationOnlyWhileLocked() {
        let n = FakeNotifier(), lock = LockNotifications(notifier: n)
        lock.update(waiting: [ping("a")], screenLocked: false) // visible on the card
        XCTAssertEqual(n.events, [])
        lock.update(waiting: [ping("a")], screenLocked: true)
        lock.update(waiting: [ping("a"), ping("b")], screenLocked: true)
        lock.update(waiting: [ping("a"), ping("b")], screenLocked: true)
        XCTAssertEqual(n.events, ["post a", "post b"])
        lock.update(waiting: [ping("b")], screenLocked: false) // a got its Pong
        lock.update(waiting: [ping("b"), ping("c")], screenLocked: false)
        XCTAssertEqual(n.events, ["post a", "post b", "remove a"])
    }

    func testOpeningANotificationShowsThatPing() {
        let inbox = Inbox(pings: [ping("a"), ping("b")])
        XCTAssertEqual(inbox.current?.id, "a")
        inbox.show("b")
        XCTAssertEqual(inbox.current?.id, "b")
        inbox.show("gone") // not a Ping the card can show: it stays where it was
        XCTAssertEqual(inbox.current?.id, "b")
    }
}
