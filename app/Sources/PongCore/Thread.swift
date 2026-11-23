import Foundation
import Observation

/// A Session's Thread as `GET /thread` reports it: the Session and its Pings, each with its Pong,
/// oldest first.
public struct SessionThread: Decodable, Equatable, Sendable {
    public struct SessionRef: Codable, Equatable, Hashable, Sendable {
        public var harness: String
        public var id: String

        public init(harness: String, id: String) {
            self.harness = harness
            self.id = id
        }
    }

    /// The parts of the Session the Thread shows.
    public struct Session: Decodable, Equatable, Sendable {
        public var harness: String?
        public var id: String
        public var folder: String?
        /// The Project's id.
        public var project: String?
        public var title: String?
        /// The prompt the Session began on, from Pong or typed in the Harness.
        public var firstPrompt: String?
        /// "human" | "engine"
        public var startedBy: String?
        public var state: String?
        public var continuedFrom: SessionRef?
        /// Set only on Sessions the Engine started.
        public var resumeCommand: String?
        /// Opens it in the Harness's own app; only on Claude Code Sessions the Engine started or
        /// that run in the Claude desktop app.
        public var appCommand: String?
        /// What the app `appCommand` opens is called.
        public var appName: String?
        /// The stop asked for the turn it is in or was last in.
        public var stop: Stop?

        public struct Stop: Decodable, Equatable, Sendable {
            /// "sent" | "stopped" | "missed"
            public var state: String
            /// The name of the Device it was asked from.
            public var device: String?
            public init(state: String, device: String? = nil) { self.state = state; self.device = device }
        }

        public var ref: SessionRef { SessionRef(harness: harness ?? "", id: id) }
    }

    public var session: Session
    /// The Session runs on another computer: it can be opened in its Harness only there.
    public var remote: Bool?
    /// Text written here has somewhere to go: the Harness's hooks take it while the Session
    /// works or sits idle, and an ended Session can be opened again with it.
    public var takesText: Bool?
    /// The Session's Harness can stop a turn, so Stop is offered while the Session is Running.
    public var stopsTurns: Bool?
    public var pings: [Ping]
    /// Each Ping's background, why it asked and what its answers lead to, by Ping id: what the
    /// card showed beside it, read from the same Pings.
    public var contexts: [String: PingContext]

    private enum CodingKeys: String, CodingKey { case session, remote, takesText, stopsTurns, pings }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        session = try c.decode(Session.self, forKey: .session)
        remote = try c.decodeIfPresent(Bool.self, forKey: .remote)
        takesText = try c.decodeIfPresent(Bool.self, forKey: .takesText)
        stopsTurns = try c.decodeIfPresent(Bool.self, forKey: .stopsTurns)
        pings = try c.decode([Ping].self, forKey: .pings)
        let explained = (try? c.decode([PingContext].self, forKey: .pings)) ?? []
        contexts = Dictionary(explained.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
    }

    /// What a Ping's Pong says of its delivery in the Thread. Only the latest Delivered Pong says
    /// so, since every Pong before it arrived too; one on its way or not delivered always says so.
    public func deliveryLine(of ping: Ping) -> String? {
        guard let pong = ping.pong, let line = pong.deliveryLine else { return nil }
        guard pong.delivery == "delivered" else { return line }
        return pings.last(where: { $0.pong?.delivery == "delivered" })?.id == ping.id ? line : nil
    }

    public static func decode(_ data: Data) throws -> SessionThread {
        try JSON.decoder.decode(SessionThread.self, from: data)
    }
}

extension EngineClient {
    /// `GET /thread?harness=H&id=ID`.
    public func thread(_ ref: SessionThread.SessionRef) async throws -> SessionThread {
        var c = URLComponents()
        c.path = "/thread"
        c.queryItems = [URLQueryItem(name: "harness", value: ref.harness), URLQueryItem(name: "id", value: ref.id)]
        return try SessionThread.decode(await send(request(c.string!)))
    }

    /// `POST /session/follow-up`: text for the Session. The Engine keeps it until the Session's
    /// Harness takes it, or opens an ended Session again with it where the human works.
    public func followUp(_ ref: SessionThread.SessionRef, prompt: String, files: [PongFile] = []) async throws {
        struct Body: Encodable { var prompt: String; var files: [PongFile]? }
        var c = URLComponents()
        c.path = "/session/follow-up"
        c.queryItems = [URLQueryItem(name: "harness", value: ref.harness), URLQueryItem(name: "id", value: ref.id)]
        let body = Body(prompt: prompt, files: files.isEmpty ? nil : files)
        _ = try await send(request(c.string!, method: "POST", body: JSONEncoder().encode(body)), timeout: 60)
    }

    /// `POST /session/stop`: asks for the turn the Session is in to be stopped. The Session
    /// stays open. Throws `CannotReach` when its computer cannot be reached now.
    public func stop(_ ref: SessionThread.SessionRef) async throws {
        var c = URLComponents()
        c.path = "/session/stop"
        c.queryItems = [URLQueryItem(name: "harness", value: ref.harness), URLQueryItem(name: "id", value: ref.id)]
        do {
            _ = try await send(request(c.string!, method: "POST"), timeout: 15)
        } catch let f as Failure where f.status == 503 {
            throw CannotReach(line: f.message ?? "Can't reach the computer.")
        }
    }

    /// Only events from now on (no `since`), for windows that fetch their state first.
    public func newEvents() -> AsyncThrowingStream<EngineEvent, Error> {
        let r = request("/events")
        return AsyncThrowingStream { cont in
            let task = Task {
                do {
                    let (bytes, _) = try await URLSession(configuration: .ephemeral).bytes(for: r)
                    for try await line in bytes.lines {
                        if let e = EventLine.parse(line) { cont.yield(e) }
                    }
                    cont.finish()
                } catch {
                    cont.finish(throwing: error)
                }
            }
            cont.onTermination = { _ in task.cancel() }
        }
    }
}

/// What "Open in harness" can do; the app gives the real one, tests a fake.
@MainActor
public protocol HarnessOpener {
    /// Runs `command`, which opens the Session in its Harness's own app; throws when it fails.
    func openApp(running command: String) throws
    /// Opens a terminal running `command`.
    func openTerminal(running command: String) throws
    func copy(_ text: String)
}

extension HarnessOpener {
    /// Opens a Session in its Harness: in the Harness's own app when the Engine gives a command
    /// for that, else in a terminal when the Session has ended or the Engine started it (resuming
    /// one the human has open elsewhere would start a second copy). False when Pong could do neither.
    public func open(app: String?, resume: String?, startedBy: String?, ended: Bool = false) -> Bool {
        if let app, (try? openApp(running: app)) != nil { return true }
        if startedBy == "engine" || ended, let resume, (try? openTerminal(running: resume)) != nil { return true }
        return false
    }
}

/// Where opening a Session takes the human first.
public enum OpenTarget: Equatable, Sendable {
    /// The Harness's own app, by name.
    case app(String)
    case terminal

    /// What `HarnessOpener.open` tries first, or nil when Pong cannot name it: the Engine names
    /// no app for the app command, or there is nothing to open and the resume command is copied.
    public init?(app: String?, name: String?, resume: String?, startedBy: String?, ended: Bool = false) {
        if app != nil {
            guard let name else { return nil }
            self = .app(name)
        } else if startedBy == "engine" || ended, resume != nil {
            self = .terminal
        } else {
            return nil
        }
    }
}

extension SessionThread.Session {
    public var openTarget: OpenTarget? {
        OpenTarget(app: appCommand, name: appName, resume: resumeCommand, startedBy: startedBy, ended: state == "ended")
    }
}

/// Where text written in a Thread goes.
public enum ThreadReplyTarget: Equatable, Sendable {
    /// The Session's latest Ping that waits for a Pong: the text is its Pong.
    case ping(Ping)
    /// The Session itself, on this computer, where its Thread takes text.
    case session
    /// Nowhere, and why.
    case none(String)
}

extension SessionThread {
    public var replyTarget: ThreadReplyTarget {
        if let p = pings.last(where: { $0.isWaiting && $0.isNote != true }) { return .ping(p) }
        if takesText == true, remote != true { return .session }
        if session.state == "ended" { return .none("This Session has ended") }
        return .none("Nothing waits for a Pong here")
    }
}

extension SessionThread {
    /// The agent is at work and nothing of its own waits for a Pong, as the Threads list's Running
    /// says: the Thread shows it writing.
    public var isRunning: Bool {
        session.state == "working" && !pings.contains(where: { $0.isWaiting && $0.isNote != true })
    }
}

/// What a Thread's bar shows for stopping the Session's turn.
public enum ThreadStop: Equatable, Sendable {
    /// The Stop button: the Session is Running and its Harness can stop a turn.
    case offered
    /// A stop was asked for and the turn still runs.
    case stopping
}

extension SessionThread {
    /// Stop is offered only while the Session is Running, as the Threads list says it: working,
    /// with no Ping of its own waiting for a Pong. Such a Ping is answered instead.
    public var stop: ThreadStop? {
        guard session.state == "working" else { return nil }
        if session.stop?.state == "sent" { return .stopping }
        guard stopsTurns == true, session.stop == nil, isRunning else { return nil }
        return .offered
    }
}

/// A stop could not be delivered: the computer running the Session cannot be reached now, and
/// nothing is kept for later. `line` is what the Thread shows, "Can't reach the MacBook."
public struct CannotReach: Error, Equatable, Sendable {
    public var line: String
    public init(line: String) { self.line = line }
    public init(computer: String) { line = "Can't reach \(computer)." }
}

/// How a Thread sends what the human writes in it; the app gives the Engine's, tests a fake.
public struct ThreadSending: Sendable {
    public var pong: @Sendable (_ pingID: String, Ping.Pong) async throws -> Void
    public var followUp: @Sendable (SessionThread.SessionRef, _ prompt: String) async throws -> Void
    /// Asks for the Session's turn to be stopped; throws `CannotReach` when it cannot be delivered.
    public var stop: @Sendable (SessionThread.SessionRef) async throws -> Void
    /// Text with Files for the Session, where the Device can send Files; nil where it cannot.
    public var followUpWithFiles: (@Sendable (SessionThread.SessionRef, _ prompt: String, [PongFile]) async throws -> Void)?

    public init(pong: @escaping @Sendable (String, Ping.Pong) async throws -> Void,
                followUp: @escaping @Sendable (SessionThread.SessionRef, String) async throws -> Void,
                stop: @escaping @Sendable (SessionThread.SessionRef) async throws -> Void = { _ in }) {
        self.pong = pong
        self.followUp = followUp
        self.stop = stop
    }
}

/// One open Thread: fetched from the Engine and fetched again whenever an event touches it.
@MainActor @Observable
public final class ThreadModel {
    public let ref: SessionThread.SessionRef
    public private(set) var thread: SessionThread?
    /// What the last "Open in harness" did, when the human should be told.
    public private(set) var notice: String?
    /// Text the human wrote here is on its way.
    public private(set) var sending = false
    /// Why the last text was not sent, in the Engine's words.
    public private(set) var refusal: String?
    /// A stop was pressed and its answer is not back yet.
    public private(set) var askingStop = false
    /// The last stop could not be delivered: "Can't reach the MacBook.", until the Session next changes.
    public private(set) var cannotReach: String?
    @ObservationIgnored private let load: @Sendable (SessionThread.SessionRef) async throws -> SessionThread
    @ObservationIgnored private let sender: ThreadSending?

    public init(ref: SessionThread.SessionRef, thread: SessionThread? = nil, sender: ThreadSending? = nil,
                load: @escaping @Sendable (SessionThread.SessionRef) async throws -> SessionThread) {
        self.ref = ref
        self.thread = thread
        self.sender = sender
        self.load = load
    }

    /// Sends text written in the Thread: as the Pong of the Ping that waits, else as the
    /// Session's next prompt. True when it went; a refusal is kept to show, with the text.
    @discardableResult
    public func send(_ text: String, files: [PongFile] = []) async -> Bool {
        guard let sender, let target = thread?.replyTarget, !sending else { return false }
        sending = true
        defer { sending = false }
        do {
            switch target {
            case .ping(let p): try await sender.pong(p.id, Ping.Pong(text: text, files: files.isEmpty ? nil : files))
            case .session where !files.isEmpty:
                guard let withFiles = sender.followUpWithFiles else {
                    refusal = "Files cannot be sent to this Session from here."
                    return false
                }
                try await withFiles(ref, text, files)
            case .session: try await sender.followUp(ref, text)
            case .none: return false
            }
        } catch {
            refusal = (error as? FeedRefusal)?.message ?? "Pong could not reach the Engine."
            return false
        }
        refusal = nil
        await refresh()
        return true
    }

    /// What the bar shows for stopping: "Stopping…" from the press until the turn has ended.
    public var stop: ThreadStop? { askingStop ? .stopping : thread?.stop }

    /// Asks at once for the Session's turn to be stopped, with no confirmation: nothing is lost,
    /// the Session stays open. A refusal other than an unreachable computer means the turn is
    /// already over, which the Thread fetched again shows.
    public func stopTurn() async {
        guard let sender, stop == .offered else { return }
        askingStop = true
        var unreachable: String?
        do { try await sender.stop(ref) } catch let e as CannotReach { unreachable = e.line } catch {}
        await refresh()
        cannotReach = unreachable
        askingStop = false
    }

    public func refresh() async {
        guard let t = try? await load(ref) else { return }
        if t.session != thread?.session { cannotReach = nil }
        thread = t
    }

    /// Fetches the Thread again when the event is about this Session; nothing is worked out here.
    public func handle(_ e: EngineEvent) async {
        if e.kind == "session.changed" || e.ping?.session == ref.id { await refresh() }
    }

    public func openInHarness(with opener: HarnessOpener) {
        guard let s = thread?.session else { return }
        if opener.open(app: s.appCommand, resume: s.resumeCommand, startedBy: s.startedBy, ended: s.state == "ended") {
            notice = nil
            return
        }
        guard let command = s.resumeCommand else {
            notice = "Pong cannot tell which app this Session runs in. The Engine has no command to resume it."
            return
        }
        opener.copy(command)
        notice = s.startedBy == "engine" || s.state == "ended"
            ? "Pong could not open a terminal. The resume command is copied: \(command)"
            : "Pong cannot tell which app this Session runs in. The resume command is copied: \(command)"
    }
}

/// Where clicking a Ping's question goes; the app sets it.
@MainActor
public enum ThreadOpening {
    public static var open: ((Ping) -> Void)?
}

/// Where clicking a Ping's Session, in the line above the question, goes: the Session itself in
/// its Harness. The app sets it.
@MainActor
public enum SessionOpening {
    public static var open: ((Ping) -> Void)?
}
