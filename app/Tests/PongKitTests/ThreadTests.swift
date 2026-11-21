import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The Thread from `GET /thread` recorded from a real Engine (Fixtures/thread/), its live
/// refetching, and Open in the Harness against a fake opener.
@MainActor
final class ThreadTests: XCTestCase {
    static func fixture(_ name: String) throws -> SessionThread {
        try SessionThread.decode(Data(contentsOf: CardSnapshotTests.here.appendingPathComponent("Fixtures/thread/\(name).json")))
    }

    final class FakeOpener: HarnessOpener {
        var opened: [String] = []
        var copied: [String] = []
        var fails = false
        var apps: [String] = []
        var appFails = false
        func openApp(running command: String) throws {
            if appFails { throw CocoaError(.executableLoad) }
            apps.append(command)
        }
        func openTerminal(running command: String) throws {
            if fails { throw CocoaError(.fileReadUnknown) }
            opened.append(command)
        }
        func copy(_ text: String) { copied.append(text) }
    }

    func testPingsAndPongsInOrderWithResources() throws {
        let t = try Self.fixture("s-new")
        XCTAssertEqual(t.pings.map(\.question), [
            "The rounding test fails. Fix rounding in the cart or the tax step?",
            "Rounding fixed in the tax step; all tests pass.",
            "Which checks should run before release?",
            "Ready to put the fix on main?",
        ])
        XCTAssertEqual(ThreadView.lines(of: try XCTUnwrap(t.pings[0].pong)), ["Tax step", "Keep the cart untouched"])
        XCTAssertEqual(ThreadView.lines(of: try XCTUnwrap(t.pings[2].pong)), ["lint, unit"])
        XCTAssertEqual(t.pings[0].resources?.map(\.title), ["Test output"])
        XCTAssertEqual(t.session.continuedFrom, .init(harness: "", id: "s-old"))
    }

    /// Fetched again on a Ping of this Session or a Session change, not on another Session's Ping.
    func testRefetchesOnEventsAboutTheSession() async throws {
        let first = try Self.fixture("s-old"), later = try Self.fixture("s-new")
        let calls = Counter()
        let model = ThreadModel(ref: .init(harness: "", id: "s-new")) { _ in await calls.next() == 1 ? first : later }
        await model.refresh()
        XCTAssertEqual(model.thread, first)
        var other = later.pings[0]
        other.session = "s-elsewhere"
        await model.handle(EngineEvent(seq: 1, kind: "ping.created", ping: other))
        XCTAssertEqual(model.thread, first)
        await model.handle(EngineEvent(seq: 2, kind: "ping.ponged", ping: later.pings[0]))
        XCTAssertEqual(model.thread, later)
        await model.handle(EngineEvent(seq: 3, kind: "session.changed", ping: nil))
        let n = await calls.count
        XCTAssertEqual(n, 3)
    }

    actor Counter {
        var count = 0
        func next() -> Int { count += 1; return count }
    }

    func model(_ t: SessionThread) -> ThreadModel { ThreadModel(ref: t.session.ref, thread: t) { _ in t } }

    func testEngineStartedSessionOpensATerminal() throws {
        var t = try Self.fixture("s-new")
        t.session.startedBy = "engine"
        t.session.resumeCommand = "cd /Users/me/shop && claude --resume s-new"
        let opener = FakeOpener(), m = model(t)
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.opened, ["cd /Users/me/shop && claude --resume s-new"])
        XCTAssertNil(m.notice)

        opener.fails = true
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.copied, ["cd /Users/me/shop && claude --resume s-new"])
        XCTAssertNotNil(m.notice)
    }

    /// A Session with an app command opens in its Harness's own app, and in a terminal when that fails.
    func testEngineStartedSessionOpensInTheHarnessApp() throws {
        var t = try Self.fixture("s-new")
        t.session.startedBy = "engine"
        t.session.resumeCommand = "cd /Users/me/shop && claude --resume s-new"
        t.session.appCommand = "cd /Users/me/shop && claude --desktop --resume s-new"
        let opener = FakeOpener(), m = model(t)
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.apps, ["cd /Users/me/shop && claude --desktop --resume s-new"])
        XCTAssertEqual(opener.opened, [])

        opener.appFails = true
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.opened, ["cd /Users/me/shop && claude --resume s-new"])
    }

    /// A Session the human started in the Harness's own app opens there, and never in a terminal,
    /// where resuming it would start a second copy.
    func testHumanStartedSessionOpensInTheHarnessApp() throws {
        var t = try Self.fixture("s-new")
        t.session.resumeCommand = "cd /Users/me/shop && claude --resume s-new"
        t.session.appCommand = "open 'claude://code/continue?session=local_1'"
        let opener = FakeOpener(), m = model(t)
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.apps, ["open 'claude://code/continue?session=local_1'"])
        XCTAssertNil(m.notice)

        opener.appFails = true
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.opened, [])
        XCTAssertEqual(opener.copied, ["cd /Users/me/shop && claude --resume s-new"])
    }

    /// The Engine reports no hosting app and no resume command for a Session the human started.
    func testHumanStartedSessionSaysWhatItCannotDo() throws {
        let opener = FakeOpener(), m = model(try Self.fixture("s-new"))
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.opened, [])
        XCTAssertEqual(opener.copied, [])
        XCTAssertNotNil(m.notice)

        var t = try Self.fixture("s-new")
        t.session.resumeCommand = "cd /Users/me/shop && claude --resume s-new"
        let m2 = model(t)
        m2.openInHarness(with: opener)
        XCTAssertEqual(opener.opened, [])
        XCTAssertEqual(opener.copied, ["cd /Users/me/shop && claude --resume s-new"])
        XCTAssertTrue(m2.notice?.contains("copied") == true)
    }

    /// An ended Session, whoever started it, has no copy running anywhere, so it opens in a
    /// terminal: a Session Pong started that never reached the Claude app is one.
    func testEndedSessionOpensATerminal() throws {
        var t = try Self.fixture("s-new")
        t.session.state = "ended"
        t.session.resumeCommand = "cd /Users/me/shop && claude --resume s-new"
        XCTAssertEqual(t.session.openTarget, .terminal)
        let opener = FakeOpener(), m = model(t)
        m.openInHarness(with: opener)
        XCTAssertEqual(opener.opened, ["cd /Users/me/shop && claude --resume s-new"])
        XCTAssertEqual(opener.copied, [])
        XCTAssertNil(m.notice)
    }

    /// The open button names what opening tries first, and nothing when Pong cannot say.
    func testOpenTargetIsWhatOpeningTriesFirst() throws {
        var s = try Self.fixture("s-new").session
        XCTAssertNil(s.openTarget)
        s.resumeCommand = "cd /Users/me/shop && claude --resume s-new"
        XCTAssertNil(s.openTarget, "a Session the human started is only copied")
        s.startedBy = "engine"
        XCTAssertEqual(s.openTarget, .terminal)
        s.appCommand = "cd /Users/me/shop && claude --desktop --resume s-new"
        XCTAssertNil(s.openTarget, "an app the Engine does not name")
        s.appName = "Claude"
        XCTAssertEqual(s.openTarget, .app("Claude"))
    }

    func testAppNameDecodes() throws {
        let json = #"{"session": {"id": "s", "app_command": "open x", "app_name": "Claude"}, "pings": []}"#
        XCTAssertEqual(try SessionThread.decode(Data(json.utf8)).session.openTarget, .app("Claude"))
    }

    func testTheKitHasAMarkForEachHarnessPongWorksWith() {
        XCTAssertNotNil(HarnessMark.image("claude-code"))
        XCTAssertNotNil(HarnessMark.image("codex"))
        XCTAssertNil(HarnessMark.image("another"))
        XCTAssertNil(HarnessMark.image(nil))
    }

    // Writing in the Thread.

    actor Sent {
        var pongs: [String] = []
        var prompts: [String] = []
        var refusal: String?
        func refuse(_ why: String?) { refusal = why }
        func pong(_ id: String, _ p: Ping.Pong) throws {
            if let refusal { throw EngineClient.Failure(description: refusal, message: refusal, status: 409) }
            pongs.append("\(id) \(p.text ?? "-")")
        }
        func prompt(_ ref: SessionThread.SessionRef, _ text: String) throws {
            if let refusal { throw EngineClient.Failure(description: refusal, message: refusal, status: 409) }
            prompts.append("\(ref.id) \(text)")
        }
        nonisolated var sender: ThreadSending {
            ThreadSending(pong: { try await self.pong($0, $1) }, followUp: { try await self.prompt($0, $1) })
        }
    }

    /// Text is the Pong of the latest Ping that waits; with none waiting it goes to a Session
    /// whose Thread takes text; any other Session says why it takes none.
    func testTextGoesToTheWaitingPingElseToTheSession() async throws {
        var t = try Self.fixture("s-new")
        let sent = Sent()
        var m = ThreadModel(ref: t.session.ref, thread: t, sender: sent.sender) { [t] _ in t }
        XCTAssertEqual(t.replyTarget, .ping(t.pings[3]), "the question, not the Note that also waits")
        let went = await m.send("After review, please")
        XCTAssertTrue(went)

        t.pings[3].state = .answered
        XCTAssertEqual(t.replyTarget, .none("Nothing waits for a Pong here"), "its Harness takes no text")
        t.takesText = true
        XCTAssertEqual(t.replyTarget, .session)
        m = ThreadModel(ref: t.session.ref, thread: t, sender: sent.sender) { [t] _ in t }
        await m.send("Now tag the release")

        await sent.refuse("the folder does not exist")
        let refused = await m.send("And deploy")
        XCTAssertFalse(refused)
        XCTAssertEqual(m.refusal, "the folder does not exist")

        t.remote = true
        XCTAssertNotEqual(t.replyTarget, .session, "another computer's Session takes no prompt from here")
        t.takesText = nil
        t.session.state = "ended"
        XCTAssertEqual(t.replyTarget, .none("This Session has ended"))
        let pongs = await sent.pongs, prompts = await sent.prompts
        XCTAssertEqual(pongs, ["529a1973e6ec6c06 After review, please"])
        XCTAssertEqual(prompts, ["s-new Now tag the release"])
    }

    /// Where the Engine says the Thread takes text, it goes to the Session whatever its state and
    /// whoever started it; a waiting Ping still comes first, and another computer's Session takes none.
    func testTextGoesToAnySessionThatTakesIt() throws {
        var t = try Self.fixture("s-new")
        t.takesText = true
        XCTAssertEqual(t.replyTarget, .ping(t.pings[3]))
        t.pings[3].state = .answered
        XCTAssertEqual(t.replyTarget, .session, "the human started it")
        t.session.state = "ended"
        XCTAssertEqual(t.replyTarget, .session, "an ended Session can be opened again")
        t.remote = true
        XCTAssertEqual(t.replyTarget, .none("This Session has ended"))
    }

    /// Text written with no Ping waiting has no question: only its Pong shows.
    func testTextWrittenInTheThreadShowsAsAPongAlone() throws {
        var t = try Self.fixture("s-new")
        XCTAssertTrue(t.pings.allSatisfy(ThreadView.showsQuestion))
        t.pings[0].unasked = true
        XCTAssertFalse(ThreadView.showsQuestion(t.pings[0]))
    }

    /// A question that waits is a button that shows it in the Queue; nothing else is.
    func testAWaitingQuestionOpensInTheQueue() throws {
        let t = try Self.fixture("s-new")
        XCTAssertEqual(t.pings.filter(ThreadView.opensInQueue).map(\.id), ["529a1973e6ec6c06"])
    }

    /// A Thread opens with the prompt its Session began on, and shows the agent writing while it
    /// works with nothing waiting for a Pong, instead of only a Ping to wait for.
    func testThreadShowsTheFirstPromptAndTheAgentWriting() throws {
        let json = #"{"session": {"id": "s", "state": "working", "first_prompt": "Fix the checkout totals"}, "pings": []}"#
        var t = try SessionThread.decode(Data(json.utf8))
        XCTAssertEqual(ThreadView.firstPrompt(t), "Fix the checkout totals")
        XCTAssertTrue(t.isRunning)
        t.session.firstPrompt = "  \n"
        XCTAssertNil(ThreadView.firstPrompt(t), "a blank prompt is none")

        var waits = try Self.fixture("s-new")
        waits.session.state = "working"
        XCTAssertFalse(waits.isRunning, "a question that waits is answered, not shown as writing")
        waits.pings = waits.pings.map { var p = $0; if p.isWaiting { p.state = .answered }; return p }
        XCTAssertTrue(waits.isRunning)
        waits.session.state = "idle"
        XCTAssertFalse(waits.isRunning)
    }

    func testSnapshotFirstPromptAndWriting() throws {
        var t = try Self.fixture("s-new")
        t.session.state = "working"
        t.session.firstPrompt = "The checkout totals round wrong for baskets over 100 items. Find out why and fix it, with a test."
        t.pings = t.pings.map { var p = $0; if p.isWaiting { p.state = .answered }; return p }
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, ResourceStore()), named: "thread-first-prompt-writing")
        t.session.firstPrompt = (1...12).map { "Line \($0) of a long prompt the human pasted in." }.joined(separator: "\n")
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, ResourceStore()), named: "thread-first-prompt-long")
    }

    func testSnapshotReply() throws {
        var t = try Self.fixture("s-new")
        try assertSnapshot(ThreadReply(target: t.replyTarget, draft: "Run the e2e checks first"), named: "thread-reply")
        t.pings[3].state = .answered
        try assertSnapshot(ThreadReply(target: t.replyTarget, refusal: nil), named: "thread-reply-none")
    }

    func testContinuedFromLinksBack() throws {
        var went: [SessionThread.SessionRef] = []
        let t = try Self.fixture("s-new")
        ThreadActions(openSession: { went.append($0) }).openSession(try XCTUnwrap(t.session.continuedFrom))
        XCTAssertEqual(went, [.init(harness: "", id: "s-old")])
        XCTAssertNil(try Self.fixture("s-old").session.continuedFrom)
    }

    /// The window and its bar name the Project and the Session as the card's Project line does,
    /// with the folder's name while Pong knows no Project, and never the path.
    func testTitleNamesTheProjectAndTheSession() throws {
        let t = try Self.fixture("s-new")
        XCTAssertEqual(t.session.project, "8c3ef10015ce5501")
        XCTAssertEqual(ThreadView.title(t, project: Project(id: "8c3ef10015ce5501", name: "Shop", colour: "#30A46C")),
                       "Shop · Fix the checkout total")
        XCTAssertEqual(ThreadView.title(t, project: nil), "shop · Fix the checkout total")
        var untitled = t
        untitled.session.title = nil
        untitled.session.folder = nil
        XCTAssertEqual(ThreadView.title(untitled, project: nil), "Untitled Session")
    }

    /// Only the latest Delivered Pong says so; every Pong before it arrived too. One that was not
    /// delivered always says so, and shows as missed.
    func testOnlyTheLatestDeliveredPongSaysSo() throws {
        let json = #"{"session": {"id": "s"}, "pings": [{"id": "a", "session": "s", "question": "One?", "state": "answered", "pong": {"answer": "Yes", "delivery": "delivered"}}, {"id": "b", "session": "s", "question": "Two?", "state": "answered", "pong": {"answer": "No", "delivery": "undelivered"}}, {"id": "c", "session": "s", "question": "Three?", "state": "answered", "pong": {"answer": "Yes", "delivery": "delivered"}}, {"id": "d", "session": "s", "question": "Four?", "state": "answered", "pong": {"answer": "Yes", "delivery": "sent"}}]}"#
        let t = try SessionThread.decode(Data(json.utf8))
        XCTAssertEqual(t.pings.map { t.deliveryLine(of: $0) }, [nil, "Not delivered", "Delivered", "Sent, not yet delivered"])
        XCTAssertEqual(t.pings.map { $0.pong!.missedHarness }, [false, true, false, false])
    }

    /// Each Ping's context comes with the Thread, from the same Pings.
    func testContextsDecodeFromThePings() throws {
        let json = #"{"session": {"id": "s"}, "pings": [{"id": "p1", "session": "s", "question": "Cart or tax?", "state": "answered", "context": "The total is off by a cent", "why": "Both fix it", "answers": [{"label": "Cart", "leads_to": "Rounds per line"}, {"label": "Tax", "hard_to_undo": true}], "pong": {"answer": "Tax"}}]}"#
        let t = try SessionThread.decode(Data(json.utf8))
        XCTAssertEqual(t.contexts["p1"]?.context, "The total is off by a cent")
        let d = ThreadView.Details(t.pings[0], t.contexts["p1"])
        XCTAssertEqual(d.answers.map(\.label), ["Cart", "Tax"])
        XCTAssertEqual(d.answers.map(\.chosen), [false, true])
        XCTAssertEqual(d.answers.map(\.leadsTo), ["Rounds per line", nil])
        XCTAssertEqual(d.answers.map(\.hardToUndo), [false, true])
        XCTAssertEqual(d.sections.map(\.label), ["Context", "Why it is asking"], "the answers show once, above")
    }

    /// A Note with no context has nothing more to open; one with answers does.
    func testWhatAPastPingOpens() throws {
        let t = try Self.fixture("s-new")
        XCTAssertTrue(ThreadView.Details(t.pings[1], t.contexts[t.pings[1].id]).isEmpty)
        XCTAssertEqual(ThreadView.Details(t.pings[2], nil).answers.filter(\.chosen).map(\.label), ["lint", "unit"])
    }

    /// The button back to the latest Ping shows while the latest Ping is out of view.
    func testScrollingAwayOffersTheLatest() {
        XCTAssertFalse(ThreadScroll(offset: 0, visible: 500, content: 1200).atBottom)
        XCTAssertFalse(ThreadScroll(offset: 300, visible: 500, content: 1200).atBottom)
        XCTAssertTrue(ThreadScroll(offset: 695, visible: 500, content: 1200).atBottom)
        XCTAssertTrue(ThreadScroll(offset: 0, visible: 500, content: 400).atBottom, "a short Thread is all in view")
    }

    func testSnapshotBarAndOpenedPing() throws {
        var t = try Self.fixture("s-new")
        t.contexts[t.pings[0].id]?.context = "The checkout total is a cent off on carts with three or more items."
        t.contexts[t.pings[0].id]?.why = "Both places can round; only one should."
        try assertSnapshot(ThreadPanelTitle(thread: t, opensIn: "Claude", stop: nil, actions: .init(),
                                            moved: {}, putBack: {}, onClose: {}).frame(width: ThreadView.width), named: "thread-title")
        try assertSnapshot(ThreadView(thread: t, opened: [t.pings[0].id, t.pings[2].id])
            .environment(\.resourceStore, ResourceStore()), named: "thread-opened")
    }

    func testSnapshot() throws {
        let t = try Self.fixture("s-new")
        let notice = "Pong cannot tell which app this Session runs in. The Engine has no command to resume it."
        try assertSnapshot(ThreadView(thread: t, notice: notice).environment(\.resourceStore, ResourceStore()), named: "thread")
    }

    // Stopping a turn.

    /// A Running Session whose Harness can stop a turn, with nothing waiting for a Pong.
    static func running() throws -> SessionThread {
        var t = try fixture("s-new")
        for i in t.pings.indices where t.pings[i].isWaiting { t.pings[i].state = .answered }
        t.session.state = "working"
        t.stopsTurns = true
        return t
    }

    /// Stop is offered only while the Session is Running and its Harness can stop a turn; a
    /// stop that is sent says so until the turn has ended.
    func testStopIsOfferedOnlyWhileTheSessionIsRunning() throws {
        var t = try Self.running()
        XCTAssertEqual(t.stop, .offered)
        t.stopsTurns = nil
        XCTAssertNil(t.stop, "its Harness stops no turn")
        t.stopsTurns = true
        t.pings[3].state = .waiting
        XCTAssertNil(t.stop, "a Session waiting on a Ping is stopped by answering the Ping")
        t.pings[3].state = .answered
        t.session.stop = .init(state: "sent", device: "Jackson's iPhone")
        XCTAssertEqual(t.stop, .stopping)
        t.session.state = "idle"
        t.session.stop = .init(state: "stopped", device: "Jackson's iPhone")
        XCTAssertNil(t.stop)
        t.session.stop = .init(state: "missed")
        XCTAssertNil(t.stop, "a missed stop shows nothing more")
        XCTAssertNil(try Self.fixture("s-old").stop)
    }

    actor Stops {
        var asked: [String] = []
        var unreachable: String?
        func cut(_ computer: String?) { unreachable = computer }
        func stop(_ ref: SessionThread.SessionRef) throws {
            if let unreachable { throw CannotReach(computer: unreachable) }
            asked.append(ref.id)
        }
        nonisolated var sender: ThreadSending {
            ThreadSending(pong: { _, _ in }, followUp: { _, _ in }, stop: { try await self.stop($0) })
        }
    }

    final class Loaded: @unchecked Sendable {
        var thread: SessionThread
        init(_ t: SessionThread) { thread = t }
    }

    /// Pressing Stop asks at once. A computer that cannot be reached is said so under the bar
    /// until the Session next changes, and Stop is offered again.
    func testStopAsksAtOnceAndSaysWhenTheComputerCannotBeReached() async throws {
        let now = Loaded(try Self.running()), stops = Stops()
        let m = ThreadModel(ref: now.thread.session.ref, thread: now.thread, sender: stops.sender) { _ in now.thread }
        await stops.cut("the MacBook")
        await m.stopTurn()
        XCTAssertEqual(m.cannotReach, "Can't reach the MacBook.")
        XCTAssertEqual(m.stop, .offered)
        await m.refresh()
        XCTAssertEqual(m.cannotReach, "Can't reach the MacBook.", "nothing about the Session changed")

        await stops.cut(nil)
        now.thread.session.stop = .init(state: "sent", device: "This Mac")
        await m.stopTurn()
        let asked = await stops.asked
        XCTAssertEqual(asked, ["s-new"])
        XCTAssertNil(m.cannotReach)
        XCTAssertEqual(m.stop, .stopping)
        await m.stopTurn()
        let again = await stops.asked
        XCTAssertEqual(again, ["s-new"], "a stop that is sent is not asked for twice")
    }

    /// The title strip's ⋮ menu holds Stop while the Session is Running, "Stopping…" once it
    /// is pressed, and Open in the Harness, which another computer's Session has not.
    @MainActor
    func testThePanelMenuHoldsStopAndOpen() throws {
        var t = try Self.running()
        func titles(_ stop: ThreadStop?) -> [String] {
            ThreadPanelTitle.menu(t, opensIn: "Claude", stop: stop, actions: .init()).map(\.title)
        }
        XCTAssertEqual(titles(.offered), ["Stop", "Open in Claude"])
        XCTAssertEqual(titles(.stopping), ["Stopping…", "Open in Claude"])
        XCTAssertEqual(ThreadPanelTitle.menu(t, opensIn: "Claude", stop: .stopping, actions: .init()).map(\.enabled), [false, true])
        XCTAssertEqual(titles(nil), ["Open in Claude"])
        t.remote = true
        XCTAssertEqual(titles(nil), [])
    }

    func testStopDecodesFromTheThread() throws {
        let json = #"{"session": {"id": "s", "state": "working", "stop": {"state": "sent", "asked_at": "2026-11-20T10:00:00Z", "device": "Jackson's iPhone"}}, "stops_turns": true, "pings": [{"id": "l1", "session": "s", "question": "You stopped this turn from Jackson's iPhone.", "state": "dismissed", "is_note": true, "line": true, "answers": []}]}"#
        let t = try SessionThread.decode(Data(json.utf8))
        XCTAssertEqual(t.session.stop, .init(state: "sent", device: "Jackson's iPhone"))
        XCTAssertEqual(t.stopsTurns, true)
        XCTAssertEqual(t.pings[0].line, true)
    }

    func testSnapshotStop() throws {
        var t = try Self.running()
        let line = #"{"session": {"id": "s"}, "pings": [{"id": "l1", "session": "s-new", "question": "You stopped this turn from Jackson's iPhone.", "state": "dismissed", "is_note": true, "line": true, "answers": []}]}"#
        t.pings.append(try SessionThread.decode(Data(line.utf8)).pings[0])
        t.session.state = "idle"
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, ResourceStore()), named: "thread-stopped")
    }

    /// Thoughts are kept in the Thread in order among the Pings, wait for nothing and leave the
    /// Thread's status to the Pings.
    func testThoughtsDecodeInOrderAndWaitForNothing() throws {
        let t = try Self.fixture("s-thoughts")
        XCTAssertEqual(t.pings.map(\.isThought), [false, true, true, false])
        XCTAssertEqual(t.pings[1].question, "The duplicate Pongs come from Inbox dropping the Session id on retry.")
        XCTAssertFalse(ThreadView.opensInQueue(t.pings[1]))
        XCTAssertNil(ThreadView.stateLine(t.pings[1]))
    }

    func testSnapshotThoughts() throws {
        var t = try Self.fixture("s-thoughts")
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, ResourceStore()), named: "thread-thoughts")
        // A Thought beside one of Pong's own lines: the turn was stopped after it.
        t.pings.removeLast()
        let line = #"{"session": {"id": "s"}, "pings": [{"id": "l1", "session": "s-thoughts", "question": "You stopped this turn from Jackson's iPhone.", "state": "dismissed", "is_note": true, "line": true, "answers": []}]}"#
        t.pings.append(try SessionThread.decode(Data(line.utf8)).pings[0])
        t.session.state = "idle"
        try assertSnapshot(ThreadView(thread: t).environment(\.resourceStore, ResourceStore()), named: "thread-thought-line")
    }
}
