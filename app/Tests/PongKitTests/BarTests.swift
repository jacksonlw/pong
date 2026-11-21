import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// When the window shows the card, the Bar, the Threads list or the start panel, what the Bar's
/// figures count, and how the Bar looks.
@MainActor
final class BarTests: XCTestCase {
    // MARK: Faces

    /// While a Thread is open beside the card, the list stays open when the pointer leaves it, a
    /// Ping still shows on the card, and the list comes back once the card goes.
    func testTheListStaysOpenWhileAThreadIsOpen() {
        let f = CardFaces()
        let ref = SessionThread.SessionRef(harness: "claude-code", id: "a")
        f.set(pointing: true)
        f.set(openThreads: [ref])
        f.set(pointing: false)
        XCTAssertEqual(f.face, .threads, "the pointer left, but a Thread is open")
        f.set(scene: true)
        XCTAssertEqual(f.face, .card, "a Ping still shows")
        f.set(scene: false)
        XCTAssertEqual(f.face, .threads, "the list comes back after the card")
        f.set(openThreads: [])
        XCTAssertEqual(f.face, .bar, "the last Thread closed with the pointer away")
    }

    func testTheCardShowsWhileAPingWaitsAndTheBarOtherwise() {
        let f = CardFaces()
        XCTAssertEqual(f.face, .bar)
        f.set(scene: true)
        XCTAssertEqual(f.face, .card)
        f.set(scene: false)
        XCTAssertEqual(f.face, .bar)
    }

    func testTheBarOpensIntoTheThreadsWhileThePointerRests() {
        let f = CardFaces()
        f.set(pointing: true)
        XCTAssertEqual(f.face, .threads)
        f.set(pointing: false)
        XCTAssertEqual(f.face, .bar)
    }

    func testAPingWaitsBehindTheOpenThreadsUntilThePointerLeaves() {
        let f = CardFaces()
        f.set(pointing: true)
        f.set(scene: true)
        XCTAssertEqual(f.face, .threads, "the list is not taken from under the pointer")
        XCTAssertTrue(f.held)
        f.set(pointing: false)
        XCTAssertEqual(f.face, .card)
        XCTAssertFalse(f.held)
    }

    func testTheHeldPingsButtonShowsTheCard() {
        let f = CardFaces()
        f.set(pointing: true)
        f.set(scene: true)
        f.openHeld()
        XCTAssertEqual(f.face, .card)
        f.set(pointing: true)
        XCTAssertEqual(f.face, .card, "the card stays under the pointer")
    }

    func testAPingOnTheClosedBarShowsTheCardAtOnce() {
        let f = CardFaces()
        f.set(scene: true)
        XCTAssertEqual(f.face, .card)
        XCTAssertFalse(f.held)
    }

    /// The last Ping answered with the pointer still on the card: the Bar takes its place, closed,
    /// and opens only when the pointer comes to it again.
    func testTheLastAnswerLeavesTheBarClosed() {
        let f = CardFaces()
        f.set(scene: true)
        f.set(pointing: true)
        f.set(scene: false)
        XCTAssertEqual(f.face, .bar)
        f.set(pointing: true)
        XCTAssertEqual(f.face, .threads)
    }

    /// New Session opens the start panel, whether the list is open or not; the pointer leaving
    /// does not close it, closing it does.
    func testNewSessionOpensTheStartPanelUntilItCloses() {
        let f = CardFaces()
        f.set(pointing: true)
        f.set(starting: true)
        XCTAssertEqual(f.face, .start)
        f.set(pointing: false)
        XCTAssertEqual(f.face, .start, "the human is typing: the pointer may be anywhere")
        f.set(starting: false)
        XCTAssertEqual(f.face, .bar)
    }

    /// A Ping that arrives while the human writes a new Session waits behind the start panel,
    /// and is the card once the panel closes or its button is clicked.
    func testAPingWaitsBehindTheStartPanel() {
        let f = CardFaces()
        f.set(starting: true)
        f.set(scene: true)
        XCTAssertEqual(f.face, .start)
        XCTAssertTrue(f.held)
        f.set(starting: false)
        XCTAssertEqual(f.face, .card)

        f.set(starting: true)
        XCTAssertEqual(f.face, .start, "New Session opens the panel over a waiting Ping too")
        f.openHeld()
        XCTAssertEqual(f.face, .card)
        XCTAssertFalse(f.held)
    }

    /// Leaving a question the human opened from the Threads list goes back to the list.
    func testLeavingAHeldPingGoesBackToTheThreads() {
        let f = CardFaces()
        f.set(pointing: true)
        f.set(scene: true)
        f.openHeld()
        XCTAssertEqual(f.face, .card)
        f.set(scene: false)
        XCTAssertEqual(f.face, .threads)
        f.set(pointing: false)
        XCTAssertEqual(f.face, .bar)
    }

    /// Leaving a question the human opened over the start panel goes back to their draft, while
    /// the panel is still open.
    func testLeavingAHeldPingGoesBackToTheDraft() {
        let f = CardFaces()
        f.set(starting: true)
        f.set(scene: true)
        f.openHeld()
        XCTAssertEqual(f.face, .card)
        f.set(pointing: false)
        XCTAssertEqual(f.face, .card, "the start panel stays aside under the card")
        f.set(scene: false)
        XCTAssertEqual(f.face, .start)

        f.set(scene: true)
        f.openHeld()
        f.set(starting: false)
        f.set(scene: false)
        XCTAssertEqual(f.face, .bar, "the panel closed while the card showed")
    }

    /// The card the pointer's leaving showed goes to the closed Bar: the pointer is elsewhere.
    func testAPingShownAsThePointerLeftGoesBackToTheBar() {
        let f = CardFaces()
        f.set(pointing: true)
        f.set(scene: true)
        f.set(pointing: false)
        f.set(scene: false)
        XCTAssertEqual(f.face, .bar)
    }

    /// The Bar's waiting button shows the card, not the held Ping's button over the list, and
    /// leaving the card goes back to the list.
    func testTheWaitingButtonShowsTheCard() {
        let f = CardFaces()
        f.set(pointing: true)
        f.openCard()
        XCTAssertEqual(f.face, .threads, "nothing to show until the card has its scene")
        f.set(scene: true)
        XCTAssertEqual(f.face, .card)
        XCTAssertFalse(f.held)
        f.set(scene: false)
        XCTAssertEqual(f.face, .threads)
    }

    // MARK: Figures

    static let now = BarFigures.date("2026-11-19T15:00:00Z")!
    static var utc: Calendar { var c = Calendar(identifier: .gregorian); c.timeZone = TimeZone(identifier: "UTC")!; return c }

    func directory() -> Directory {
        let d = Directory()
        d.apply(##"data: {"seq": 0, "kind": "project.changed", "at": "2026-11-19T08:00:00Z", "data": {"id": "p-api", "name": "billing-api", "colour": "#c4ecd4"}}"##)
        let sessions: [(String, String, String, String)] = [
            ("a", "working", "2026-11-19T14:58:00.123Z", #", "context": {"cost_usd": 3.18}"#),
            ("b", "working", "2026-11-19T14:40:00Z", #", "context": {"cost_usd": 1.02}"#),
            ("c", "failed", "2026-11-19T09:00:00Z", ""),
            ("d", "idle", "2026-11-18T22:00:00Z", #", "context": {"cost_usd": 40}"#),
        ]
        for (i, (id, state, at, ctx)) in sessions.enumerated() {
            d.apply(#"data: {"seq": \#(i + 1), "kind": "session.changed", "at": "\#(at)", "data": {"harness": "claude-code", "id": "\#(id)", "title": "Session \#(id)", "project": "\#(id == "d" ? "" : "p-api")", "state": "\#(state)", "updated_at": "\#(at)"\#(ctx)}}"#)
        }
        return d
    }

    func testFiguresCountWorkingAndFailedSessionsAndTodaysCost() {
        let f = BarFigures(directory: directory(), asking: [], waiting: 2, now: Self.now, calendar: Self.utc)
        XCTAssertEqual(f.working, 2)
        XCTAssertEqual(f.failed, 1)
        XCTAssertEqual(try XCTUnwrap(f.cost), 4.20, accuracy: 0.001, "yesterday's Session is not counted")
        XCTAssertEqual(f.waiting, 2)
        XCTAssertNil(BarFigures(directory: Directory(), asking: [], waiting: 0).cost, "no cost known: none shown")
    }

    /// As on a real install: a Session left working hours ago, behind ten newer ones, and a
    /// working Session that asks a Ping. The figures count what the Threads list shows.
    func testTheFiguresAndTheThreadsAgree() throws {
        let d = directory()
        for i in 0..<10 {
            let at = "2026-11-19T14:59:\(String(format: "%02d", i))Z"
            d.apply(#"data: {"seq": \#(10 + i), "kind": "session.changed", "at": "\#(at)", "data": {"harness": "claude-code", "id": "new\#(i)", "state": "idle", "updated_at": "\#(at)"}}"#)
        }
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true })
        var asking = ping
        asking.session = "a"
        let rows = ThreadRow.recent(d, waiting: [asking])
        let figures = BarFigures(directory: d, asking: [asking], waiting: 0)
        XCTAssertEqual(rows.first { $0.ref.id == "a" }?.status, .waiting, "asking a Ping: Waiting, not Running")
        XCTAssertEqual(rows.first { $0.ref.id == "b" }?.status, .running, "an older working Session is still listed")
        XCTAssertEqual(rows.first { $0.ref.id == "c" }?.status, .error, "an older failed one too")
        XCTAssertEqual(rows.count, 11, "the newest eight, and the three older ones not Done")
        XCTAssertNil(rows.first { $0.ref.id == "d" }, "an older idle one is not")
        XCTAssertEqual(figures.working, rows.filter { $0.status == .running }.count)
        XCTAssertEqual(figures.failed, rows.filter { $0.status == .error }.count)
        XCTAssertEqual(figures.working, 1)
    }

    /// Show more lists older Threads a page at a time.
    func testShowMoreListsOlderThreads() {
        let d = directory()
        for i in 0..<12 {
            let at = "2026-11-19T15:00:\(String(format: "%02d", i))Z"
            d.apply(#"data: {"seq": \#(10 + i), "kind": "session.changed", "at": "\#(at)", "data": {"harness": "claude-code", "id": "new\#(i)", "state": "idle", "updated_at": "\#(at)"}}"#)
        }
        XCTAssertEqual(ThreadRow.recent(d, waiting: []).count, 11, "the newest eight and three older ones not Done")
        XCTAssertEqual(ThreadRow.recent(d, waiting: [], count: 2 * ThreadRow.page).count, 16)
        XCTAssertEqual(ThreadRow.recent(d, waiting: [], count: 3 * ThreadRow.page).count, d.sessions.count, "every Thread")
    }

    func testThreadsAreTheNewestSessions() {
        let rows = ThreadRow.recent(directory(), waiting: [])
        XCTAssertEqual(rows.map(\.ref.id), ["a", "b", "c", "d"])
        XCTAssertEqual(rows.map(\.status), [.running, .running, .error, .done])
    }

    // MARK: A Thread's details

    static let usEnglish = Locale(identifier: "en_US")

    func details(state: String = "working", summary: String? = "Moving the login form to the new token API; the tests are next.") -> ThreadDetails {
        ThreadDetails(summary: summary, createdAt: "2026-11-19T12:46:00.000Z", turnStartedAt: "2026-11-19T14:54:00Z", state: state,
                      context: ContextFigures(usedFraction: 0.42, tokens: 420_000, window: 1_000_000, costUsd: 3.18, model: "claude-opus-5-5"))
    }

    func testAThreadsDetailsSayWhereItStandsAndHowLongItHasRun() throws {
        let s = details().sections(now: Self.now, calendar: Self.utc, locale: Self.usEnglish)
        XCTAssertEqual(s.map(\.label), ["", ""], "the summary and the figures stand without labels")
        guard case .text(let summary) = s.first?.body else { return XCTFail("no summary: \(s)") }
        XCTAssertEqual(summary, "Moving the login form to the new token API; the tests are next.")
        guard case .figures(let rows) = s.last?.body else { return XCTFail("no figures: \(s)") }
        XCTAssertEqual(rows.map(\.0), ["Started", "Running for", "This turn", "Memory", "Tokens", "Cost", "Model"])
        XCTAssertEqual(rows.map(\.1), ["Today, 12:46\u{202F}PM", "2:14:00", "6:00", "42% full", "420k of 1M", "$3.18 so far", "claude-opus-5-5"])
    }

    func testAnIdleThreadsDetailsLeaveOutTheTurn() throws {
        let s = details(state: "idle", summary: nil).sections(now: Self.now.addingTimeInterval(86_400 * 2), calendar: Self.utc, locale: Self.usEnglish)
        XCTAssertEqual(s.count, 1)
        guard case .figures(let rows) = s.first?.body else { return XCTFail("no figures: \(s)") }
        XCTAssertEqual(Array(rows.map(\.0).prefix(3)), ["Started", "Running for", "Memory"])
        XCTAssertEqual(rows[0].1, "Nov 19, 12:46\u{202F}PM")
        XCTAssertEqual(rows[1].1, "2d 2:14:00")
        XCTAssertTrue(ThreadDetails(summary: "", createdAt: nil, turnStartedAt: nil, state: nil, context: nil)
            .sections(now: Self.now).isEmpty, "nothing known: no details open")
    }

    func testTheBarsThreadsCarryTheirSessionsDetails() {
        let d = directory()
        d.apply(#"data: {"seq": 9, "kind": "session.changed", "at": "2026-11-19T14:59:00Z", "data": {"harness": "claude-code", "id": "a", "title": "Session a", "state": "working", "summary": "Writing the PR", "created_at": "2026-11-19T12:46:00Z", "turn_started_at": "2026-11-19T14:54:00Z", "updated_at": "2026-11-19T14:59:00Z"}}"#)
        let a = ThreadRow.recent(d, waiting: []).first { $0.ref.id == "a" }
        XCTAssertEqual(a?.details?.summary, "Writing the PR")
        XCTAssertNotNil(a?.details?.createdAt)
        XCTAssertTrue(a?.details?.working == true)
    }

    // MARK: Moving it

    /// What a press at a point lands on, from the top-left of the view, in a window as the app has it.
    func pressed<V: View>(_ view: V, at points: [CGPoint]) -> [Bool] {
        let host = NSHostingView(rootView: view.environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover())))
        let size = host.fittingSize
        let w = NSPanel(contentRect: NSRect(origin: .zero, size: size), styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        w.contentView = host
        host.layoutSubtreeIfNeeded()
        return points.map { p in w.contentView?.superview?.hitTest(NSPoint(x: p.x, y: size.height - p.y)) is WindowDragArea.Area }
    }

    /// A press anywhere on the Bar's row but its buttons moves the window: on the mark, in the
    /// gap and on the figures.
    func testTheBarsRowMovesTheWindow() {
        let row = [CGPoint(x: 23, y: 21), CGPoint(x: 200, y: 21), CGPoint(x: 380, y: 21)]
        XCTAssertEqual(pressed(BarView(figures: BarFigures(working: 1, failed: 1, cost: 2), threads: rows()), at: row),
                       [true, true, true])
    }

    /// A right click on the Bar's row offers Settings and Quit.
    func testTheBarsRowOffersSettingsAndQuit() throws {
        var opened = false
        let host = NSHostingView(rootView: BarView(figures: BarFigures(), threads: [], actions: .init(settings: { opened = true }))
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover())))
        let w = NSPanel(contentRect: NSRect(origin: .zero, size: host.fittingSize), styleMask: [.borderless, .nonactivatingPanel],
                        backing: .buffered, defer: false)
        w.contentView = host
        host.layoutSubtreeIfNeeded()
        let area = try XCTUnwrap(w.contentView?.superview?.hitTest(NSPoint(x: 200, y: host.fittingSize.height - 21)) as? WindowDragArea.Area)
        let click = try XCTUnwrap(NSEvent.mouseEvent(with: .rightMouseDown, location: .zero, modifierFlags: [], timestamp: 0,
                                                      windowNumber: w.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
        let menu = try XCTUnwrap(area.menu(for: click))
        XCTAssertEqual(menu.items.map(\.title), ["Settings…", "", "Quit Pong"])
        menu.performActionForItem(at: 0)
        XCTAssertTrue(opened)
    }

    /// A right click or Control-click anywhere in Pong's windows (here the card's question) opens
    /// Pong's menu, unless what was clicked has a menu of its own: a Resource's, a text field's.
    func testARightClickAnywhereOpensPongsMenu() throws {
        func click(_ view: some View, type: NSEvent.EventType = .rightMouseDown, keys: NSEvent.ModifierFlags = []) throws -> NSView? {
            let host = NSHostingView(rootView: view.frame(width: 200, height: 100))
            let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 200, height: 100), styleMask: [.borderless], backing: .buffered, defer: false)
            w.contentView = host
            host.layoutSubtreeIfNeeded()
            let e = try XCTUnwrap(NSEvent.mouseEvent(with: type, location: NSPoint(x: 100, y: 50), modifierFlags: keys, timestamp: 0,
                                                     windowNumber: w.windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: 1))
            return PongMenu.target(of: e)
        }
        XCTAssertNotNil(try click(Text("Which branch?")), "a right click on a question")
        XCTAssertNotNil(try click(Text("Which branch?"), type: .leftMouseDown, keys: .control), "a Control-click")
        XCTAssertNil(try click(Text("Which branch?"), type: .leftMouseDown), "a plain click")
        XCTAssertNil(try click(Text("plan.md").contextMenu { Button("Open") {} }), "a Resource keeps its own menu")
        XCTAssertNil(try click(TextEditor(text: .constant("an answer"))), "a text field keeps its own menu")
        XCTAssertEqual(PongMenu.make(settings: {}).items.map(\.title), ["Settings…", "", "Quit Pong"])
    }

    /// The card's top bar moves the window too: on the mark and on the progress bar.
    func testTheCardsTopBarMovesTheWindow() throws {
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true })
        XCTAssertEqual(pressed(CardView(ping: ping), at: [CGPoint(x: 23, y: 21), CGPoint(x: 150, y: 21)]), [true, true])
    }

    // MARK: How it looks

    func rows() -> [ThreadRow] { ThreadRow.recent(directory(), waiting: []) }

    func bar(_ figures: BarFigures, open: BarView.Part? = nil, held: (question: String, from: String?)? = nil, bottom: Bool = false) -> some View {
        BarView(figures: figures, threads: rows(), open: open, held: held, anchorsBottom: bottom)
            .environment(\.typingAnimates, false)
            .padding(12)
            .background(Brand.page)
    }

    func testBarLooks() throws {
        try assertSnapshot(bar(BarFigures(working: 2, failed: 1, cost: 4.2)), named: "bar-figures")
        try assertSnapshot(bar(BarFigures()), named: "bar-nothing-working")
        try assertSnapshot(bar(BarFigures(working: 1, waiting: 3)), named: "bar-waiting")
    }

    func testOpenBarLooks() throws {
        let figures = BarFigures(working: 2, failed: 1, cost: 4.2)
        try assertSnapshot(bar(figures, open: .threads), named: "bar-threads")
        try assertSnapshot(bar(figures, open: .threads, held: ("Which database should the cache use?", "Pong")), named: "bar-threads-held")
        try assertSnapshot(bar(figures, open: .threads, held: ("Which database should the cache use?", "Pong"), bottom: true),
                           named: "bar-threads-held-bottom")
    }
}
