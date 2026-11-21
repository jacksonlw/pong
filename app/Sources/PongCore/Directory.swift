import Foundation
import Observation

/// A Project as the Engine reports it.
public struct Project: Decodable, Equatable, Sendable {
    public var id: String
    public var name: String
    public var colour: String
    /// The path to fetch the logo from; absent when there is none.
    public var logo: String?
    /// This computer takes Sessions started from other Devices in it.
    public var enabled: Bool?
    /// The computers that take Sessions started from other Devices in it.
    public var enabledOn: [String]?
    /// Only another computer has it.
    public var remote: Bool?

    public init(id: String, name: String, colour: String, logo: String? = nil) {
        self.id = id; self.name = name; self.colour = colour; self.logo = logo
    }
}

/// How full a Session's context window is, as far as its Harness says. Every part is optional.
/// JSON: `{"used_fraction": 0.42, "tokens": 420000, "window": 1000000, "cost_usd": 3.18, "model": "…"}`.
public struct ContextFigures: Decodable, Equatable, Sendable {
    /// 0…1. The ring shows only when this is known.
    public var usedFraction: Double?
    public var tokens: Int?
    public var window: Int?
    public var costUsd: Double?
    public var model: String?

    public init(usedFraction: Double? = nil, tokens: Int? = nil, window: Int? = nil, costUsd: Double? = nil, model: String? = nil) {
        self.usedFraction = usedFraction; self.tokens = tokens; self.window = window; self.costUsd = costUsd; self.model = model
    }
}

/// A Session as the Engine reports it. Only the parts the card shows or opens it with.
public struct Session: Decodable, Equatable, Sendable {
    public var harness: String?
    public var id: String
    public var project: String?
    public var title: String?
    public var context: ContextFigures?
    /// "human" | "engine"
    public var startedBy: String?
    public var resumeCommand: String?
    /// Opens it in the Harness's own app, when the Engine knows a way.
    public var appCommand: String?
    /// What the app `appCommand` opens is called.
    public var appName: String?
    /// "working" | "waiting" | "idle" | "ended" | "failed"
    public var state: String?
    /// UTC, in one format, so the text sorts as the time does.
    public var updatedAt: String?
    /// The agent's latest line on where it stands, from its last Ping or Note that gave one.
    public var summary: String?
    public var createdAt: String?
    /// When its current or last turn started.
    public var turnStartedAt: String?

    /// What its Thread's details show.
    public var details: ThreadDetails {
        ThreadDetails(summary: summary, createdAt: createdAt, turnStartedAt: turnStartedAt, state: state, context: context)
    }
}

/// The parts of a Ping that explain it: where it is from, its background, why it asks and what
/// each answer leads to.
public struct PingContext: Decodable, Equatable, Sendable {
    public struct Answer: Decodable, Equatable, Sendable {
        public var label: String
        public var leadsTo: String?
        public var hardToUndo: Bool?
    }

    public var id: String
    public var session: String
    public var harness: String?
    public var project: String?
    public var context: String?
    public var why: String?
    /// On finished work, what the agent says it did not check.
    public var notVerified: String?
    public var answers: [Answer]?
    public var state: PingState
}

/// Projects, Sessions and waiting Pings' context by id, kept from the Engine's event stream so
/// each card can be rendered from them.
@MainActor @Observable
public final class Directory {
    public private(set) var projects: [String: Project] = [:]
    public private(set) var sessions: [String: Session] = [:]
    public private(set) var pings: [String: PingContext] = [:]
    /// Logo image bytes by Project id.
    public private(set) var logos: [String: Data] = [:]
    /// Where the card sits, from the Engine's settings.
    public private(set) var corner: CardCorner = .topRight
    /// When a Ping makes a sound, from the Engine's settings.
    public private(set) var pingSound: PingSoundMode = .first
    /// Voice mode and auto-listen, from the Engine's settings.
    public private(set) var voice = false
    public private(set) var voiceAutoListen = false
    /// The model voice understands speech with, from the Engine's settings.
    public private(set) var voiceModel: VoiceModelChoice = .none
    public private(set) var voiceEndpoint: String?
    public private(set) var voiceEndpointModel: String?
    /// Whether the card moves to the display with the focused window, from the Engine's settings.
    public private(set) var followsActiveDisplay = false

    /// What a card needs to say where its Ping is from and to explain it.
    public struct Card: Equatable {
        public var project: Project?
        public var session: Session?
        public var ping: PingContext?
        public var logo: Data?

        public init(project: Project? = nil, session: Session? = nil, ping: PingContext? = nil, logo: Data? = nil) {
            self.project = project; self.session = session; self.ping = ping; self.logo = logo
        }
    }

    nonisolated public init() {}

    /// A Project and its logo by id, for views that know the Project but not a Ping of it.
    public func project(_ id: String?) -> (Project, Data?)? {
        guard let id, let p = projects[id] else { return nil }
        return (p, logos[id])
    }

    public func card(for ping: Ping) -> Card {
        let ctx = pings[ping.id]
        let session = ctx.flatMap { sessions[Self.key($0.harness, $0.session)] }
            ?? sessions.values.first { $0.id == ping.session }
        let projectID = ctx?.project ?? session?.project
        return Card(project: projectID.flatMap { projects[$0] }, session: session, ping: ctx,
                    logo: projectID.flatMap { logos[$0] })
    }

    /// Takes a Project's logo from a feed that carries it, as the phone's does.
    public func setLogo(_ data: Data, for projectID: String) { logos[projectID] = data }

    private struct Envelope<T: Decodable>: Decodable { var seq: Int; var kind: String; var data: T }
    private struct Head: Decodable { var seq: Int; var kind: String }

    /// Applies one line of the event stream; returns its seq when it carried an event.
    @discardableResult
    public func apply(_ line: String) -> Int? {
        guard line.hasPrefix("data:") else { return nil }
        return apply(event: Data(line.dropFirst(5).drop(while: { $0 == " " }).utf8))
    }

    /// Takes the Engine's current state (`GET /directory`) in place of what it held: what the
    /// snapshot leaves out is gone. Returns the seq to follow the event stream from.
    @discardableResult
    public func apply(snapshot data: Data) -> Int? {
        guard let s = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let seq = s["seq"] as? Int, let events = s["events"] as? [Any] else { return nil }
        let oldProjects = Set(projects.keys)
        sessions = [:]
        pings = [:]
        var seen = Set<String>()
        for e in events {
            guard let body = try? JSONSerialization.data(withJSONObject: e) else { continue }
            apply(event: body)
            if let p = try? JSON.decoder.decode(Envelope<Project>.self, from: body), p.kind == "project.changed" { seen.insert(p.data.id) }
        }
        for gone in oldProjects.subtracting(seen) { projects[gone] = nil; logos[gone] = nil }
        return seq
    }

    @discardableResult
    private func apply(event body: Data) -> Int? {
        guard let head = try? JSON.decoder.decode(Head.self, from: body) else { return nil }
        switch head.kind {
        case "project.changed":
            if let p = try? JSON.decoder.decode(Envelope<Project>.self, from: body).data {
                if projects[p.id]?.logo != p.logo { logos[p.id] = nil; fetchLogo(p) }
                projects[p.id] = p
            }
        case "session.changed":
            if let s = try? JSON.decoder.decode(Envelope<Session>.self, from: body).data {
                sessions[Self.key(s.harness, s.id)] = s
            }
        case "settings.changed":
            struct Changed: Decodable { var cardCorner: CardCorner; var pingSound: PingSoundMode?; var voice: Bool?; var voiceAutoListen: Bool?
                           var voiceModel: VoiceModelChoice?; var voiceEndpoint: String?; var voiceEndpointModel: String?
                           var cardFollowsActiveDisplay: Bool? }
            if let c = try? JSON.decoder.decode(Envelope<Changed>.self, from: body).data {
                corner = c.cardCorner
                pingSound = c.pingSound ?? .first
                voice = c.voice ?? false
                voiceAutoListen = c.voiceAutoListen ?? false
                voiceModel = c.voiceModel ?? .none
                voiceEndpoint = c.voiceEndpoint
                voiceEndpointModel = c.voiceEndpointModel
                // Events from before the display setting existed lack it: the card stays on the main display.
                followsActiveDisplay = c.cardFollowsActiveDisplay ?? false
            }
        case let k where k.hasPrefix("ping."):
            if let p = try? JSON.decoder.decode(Envelope<PingContext>.self, from: body).data {
                pings[p.id] = p.state == .waiting ? p : nil
            }
        default: break
        }
        return head.seq
    }

    private static func key(_ harness: String?, _ id: String) -> String { "\(harness ?? "")\u{0}\(id)" }

    @ObservationIgnored private var client: EngineClient?

    /// Follows the Engine until cancelled: at each connection it takes the current state from
    /// `/directory`, then the events after it. The log keeps old events about Sessions and
    /// Projects for a week only, so it is never replayed from its start. An Engine without
    /// `/directory` is replayed from the start, as before.
    public func follow(clientProvider: @escaping @Sendable () async -> EngineClient?, retry: Duration = .milliseconds(500)) async {
        var since = 0
        while !Task.isCancelled {
            if let c = await clientProvider() {
                client = c
                if let (data, resp) = try? await URLSession.shared.data(for: Self.request(c, "/directory")),
                   (resp as? HTTPURLResponse)?.statusCode == 200, let seq = apply(snapshot: data) {
                    since = seq
                }
                var r = Self.request(c, "/events?since=\(since)")
                r.timeoutInterval = 3600
                do {
                    let (bytes, _) = try await URLSession.shared.bytes(for: r)
                    for try await line in bytes.lines {
                        if let seq = apply(line) { since = seq }
                    }
                } catch {}
            }
            try? await Task.sleep(for: retry)
        }
    }

    private func fetchLogo(_ p: Project) {
        guard let path = p.logo, let c = client else { return }
        Task {
            guard let (data, resp) = try? await URLSession.shared.data(for: Self.request(c, path)),
                  (resp as? HTTPURLResponse)?.statusCode == 200, projects[p.id]?.logo == path else { return }
            logos[p.id] = data
        }
    }

    private static func request(_ c: EngineClient, _ path: String) -> URLRequest {
        var r = URLRequest(url: URL(string: "http://\(c.address)\(path)")!)
        r.setValue("Bearer \(c.secret)", forHTTPHeaderField: "Authorization")
        return r
    }
}
