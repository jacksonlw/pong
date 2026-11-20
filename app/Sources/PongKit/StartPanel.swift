import Foundation
import Observation

// The start panel, opened from the Bar: pick a Project or a folder, type what you want,
// and the Engine starts the Session.

/// A Project as `GET /projects` reports it, with the Harness last used in it, which the Engine
/// decides.
public struct StartProject: Decodable, Equatable, Sendable {
    public var project: Project
    public var lastHarness: String?

    public init(from decoder: Decoder) throws {
        project = try Project(from: decoder)
        lastHarness = try JSONKeys.decode(decoder, "lastHarness")
    }
}

/// A Harness as `GET /harnesses` reports it, with whether the Engine can start Sessions in it.
public struct StartHarness: Decodable, Equatable, Sendable {
    public var status: HarnessStatus
    public var canStart: Bool

    public init(from decoder: Decoder) throws {
        status = try HarnessStatus(from: decoder)
        canStart = try JSONKeys.decode(decoder, "canStart") ?? false
    }
}

/// Reads one extra key next to a type the panel shares with the rest of the app. Keys are as
/// `JSON.decoder` sees them, after converting from snake case.
private struct JSONKeys: CodingKey {
    var stringValue: String
    var intValue: Int? { nil }
    init(stringValue: String) { self.stringValue = stringValue }
    init?(intValue: Int) { nil }

    static func decode<T: Decodable>(_ decoder: Decoder, _ key: String) throws -> T? {
        try decoder.container(keyedBy: JSONKeys.self).decodeIfPresent(T.self, forKey: JSONKeys(stringValue: key))
    }
}

/// Where a new Session would go across the set (`GET /sessions/placement`).
public struct Placement: Decodable, Equatable, Sendable {
    public struct Computer: Decodable, Equatable, Sendable, Identifiable {
        public var id: String
        public var name: String
        public var eligible: Bool
        public var reasons: [String]?
        public var warning: String?
        public var running: Int
    }
    public var choice: String?
    public var computers: [Computer]
}

/// What a Thread's Session is doing, as the Threads list says it.
public enum ThreadStatus: Equatable, Sendable {
    case running, waiting, done, error

    public var label: String {
        switch self {
        case .running: "Running"
        case .waiting: "Waiting"
        case .done: "Done"
        case .error: "Error"
        }
    }

    /// A waiting Ping comes first: the Session may still be working while it asks.
    init(state: String?, hasWaitingPing: Bool) {
        switch state {
        case "failed": self = .error
        case _ where hasWaitingPing, "waiting": self = .waiting
        case "working": self = .running
        default: self = .done
        }
    }
}

/// One row of the Threads list.
public struct ThreadRow: Equatable, Identifiable, Sendable {
    public var ref: SessionThread.SessionRef
    public var title: String
    public var status: ThreadStatus
    /// The Session's Project and its logo, shown at the row's right; none for a plain folder.
    public var project: Project? = nil
    public var logo: Data? = nil
    /// What the row shows on hover.
    public var details: ThreadDetails? = nil
    public var id: SessionThread.SessionRef { ref }
}

/// Where the new Session runs: a recognised Project, or a folder picked by the human.
public enum StartPlace: Equatable, Sendable {
    case project(String)
    case folder(String)
}

/// The Engine refused to start the Session; `message` is its `error`, to show as it is.
public struct StartRefused: Error, Equatable {
    public var message: String
}

/// The parts of the Engine's API the panel uses, with the Project menu's edits.
public protocol StartAPI: Sendable {
    func startProjects() async throws -> [StartProject]
    func logo(_ path: String) async throws -> Data
    /// The palette a Project's colour is chosen from comes with the settings.
    func settings() async throws -> EngineSettings
    func changeProject(_ id: String, name: String?, colour: String?) async throws -> Project
    func setLogo(_ id: String, image: Data) async throws -> Project
    func removeLogo(_ id: String) async throws -> Project
    /// Whether this computer takes Sessions started from other Devices in the Project.
    func setEnabled(_ id: String, _ on: Bool) async throws -> Project
    func startHarnesses() async throws -> [StartHarness]
    /// Throws `StartRefused` with the Engine's words when it cannot start it.
    func startSession(harness: String, at place: StartPlace, prompt: String) async throws
    /// Starts it on the computer chosen, by Device identifier; nil lets Pong choose.
    func startSession(harness: String, at place: StartPlace, prompt: String, computer: String?) async throws
    /// Where a Session in the Project would go; nil when this Mac is on its own.
    func placement(project: String, harness: String) async throws -> Placement?
}

public extension StartAPI {
    func startSession(harness: String, at place: StartPlace, prompt: String, computer: String?) async throws {
        try await startSession(harness: harness, at: place, prompt: prompt)
    }
    func placement(project: String, harness: String) async throws -> Placement? { nil }
    func setEnabled(_ id: String, _ on: Bool) async throws -> Project { throw EngineClient.Failure(description: "not supported") }
}

extension EngineClient: StartAPI {
    public func setEnabled(_ id: String, _ on: Bool) async throws -> Project {
        try await json(project(id), "PATCH", JSONEncoder().encode(["enabled": on]))
    }
    private func project(_ id: String) -> String {
        "/projects/" + (id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id)
    }

    public func changeProject(_ id: String, name: String?, colour: String?) async throws -> Project {
        struct Change: Encodable { var name: String?; var colour: String? }
        return try await json(project(id), "PATCH", JSONEncoder().encode(Change(name: name, colour: colour)))
    }
    public func setLogo(_ id: String, image: Data) async throws -> Project {
        var r = request(project(id) + "/logo", method: "PUT")
        r.httpBody = image
        return try JSON.decoder.decode(Project.self, from: await send(r))
    }
    public func removeLogo(_ id: String) async throws -> Project { try await json(project(id) + "/logo", "DELETE") }
    public func logo(_ path: String) async throws -> Data { try await send(request(path)) }

    public func startProjects() async throws -> [StartProject] {
        try JSON.decoder.decode([StartProject].self, from: await send(request("/projects")))
    }

    public func startHarnesses() async throws -> [StartHarness] {
        try JSON.decoder.decode([StartHarness].self, from: await send(request("/harnesses"), timeout: 120))
    }

    public func placement(project: String, harness: String) async throws -> Placement? {
        var c = URLComponents()
        c.path = "/sessions/placement"
        c.queryItems = [URLQueryItem(name: "project", value: project), URLQueryItem(name: "harness", value: harness)]
        do {
            let p: Placement = try await json(c.string ?? "")
            return p.computers.count > 1 ? p : nil
        } catch let f as Failure where f.status == 404 {
            return nil
        }
    }

    public func startSession(harness: String, at place: StartPlace, prompt: String) async throws {
        try await startSession(harness: harness, at: place, prompt: prompt, computer: nil)
    }

    public func startSession(harness: String, at place: StartPlace, prompt: String, computer: String?) async throws {
        var body = ["harness": harness, "prompt": prompt]
        if let computer { body["computer"] = computer }
        switch place {
        case .project(let id): body["project"] = id
        case .folder(let path): body["folder"] = path
        }
        var r = request("/sessions/start", method: "POST", body: try JSONEncoder().encode(body))
        r.timeoutInterval = 30
        let (data, resp) = try await URLSession(configuration: .ephemeral).data(for: r)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            struct Body: Decodable { var error: String }
            let text = (try? JSONDecoder().decode(Body.self, from: data))?.error ?? String(decoding: data, as: UTF8.self)
            throw StartRefused(message: text.isEmpty ? "The Engine answered \(status)." : text)
        }
    }
}

/// What the panel shows and the Session it is about to start.
@MainActor @Observable
public final class StartPanelModel {
    public private(set) var projects: [Project] = []
    public private(set) var logos: [String: Data] = [:]
    /// The colours a Project can take, from the Engine.
    public private(set) var palette: [String] = []
    /// The Project whose menu is open, by id.
    public var menuOpen: String?
    /// The Harnesses offered: the installed ones the Engine can start Sessions in.
    public private(set) var harnesses: [HarnessStatus] = []
    /// Kept with the draft, so the panel opens on the Project or folder it was closed on.
    public private(set) var place: StartPlace? { didSet { if place != oldValue { keepPlace() } } }
    public var harness: String? { didSet { if harness != oldValue { placementChanged() } } }
    @ObservationIgnored private var placing: Task<Void, Never>?

    /// Asks again where the Session would go; only the latest answer is kept.
    private func placementChanged() {
        placing?.cancel()
        placing = Task { await refreshPlacement() }
    }
    /// Where the Session would go, once this Mac shares a set with other computers.
    public private(set) var placement: Placement?
    /// The computer the human chose instead of Pong's choice.
    public var computer: String?

    /// The computer the Session will start on: the human's choice, else Pong's.
    public var chosenComputer: String? { computer ?? placement?.choice }

    public func refreshPlacement() async {
        guard case .project(let id)? = place, let harness else { placement = nil; return }
        let p = try? await api.placement(project: id, harness: harness)
        guard !Task.isCancelled, place == .project(id), self.harness == harness else { return }
        placement = p
        if let c = computer, placement?.computers.first(where: { $0.id == c })?.eligible != true { computer = nil }
    }
    /// What the human has typed. It is kept as a draft with the files, so closing the panel or
    /// quitting Pong loses nothing; a Session that starts clears it.
    public var text = "" { didSet { if text != oldValue { drafts?.set(text, forKey: Self.textKey) } } }
    /// Files the human added for the agent to read, by path.
    public private(set) var files: [String] = [] { didSet { drafts?.set(files, forKey: Self.filesKey) } }
    public private(set) var starting = false
    /// Why the last start failed, or why loading failed, in the Engine's words.
    public private(set) var problem: String?

    @ObservationIgnored private let api: StartAPI
    /// The Harness last used in each Project, by Project id, as the Engine reports it.
    @ObservationIgnored private var lastHarness: [String: String] = [:]

    /// Where the draft is kept between openings of the panel and runs of the app.
    @ObservationIgnored private let drafts: UserDefaults?
    private static let textKey = "startDraftText", filesKey = "startDraftFiles"
    private static let projectKey = "startPlaceProject", folderKey = "startPlaceFolder"

    private func keepPlace() {
        var project: String?, folder: String?
        switch place {
        case .project(let id): project = id
        case .folder(let path): folder = path
        case nil: break
        }
        drafts?.set(project, forKey: Self.projectKey)
        drafts?.set(folder, forKey: Self.folderKey)
    }

    public init(api: StartAPI, drafts: UserDefaults? = nil) {
        self.api = api
        self.drafts = drafts
        text = drafts?.string(forKey: Self.textKey) ?? ""
        files = drafts?.stringArray(forKey: Self.filesKey) ?? []
        place = drafts?.string(forKey: Self.projectKey).map(StartPlace.project) ?? drafts?.string(forKey: Self.folderKey).map(StartPlace.folder)
    }

    public func add(files paths: [String]) {
        for p in paths where !files.contains(p) { files.append(p) }
    }

    public func remove(file path: String) {
        files.removeAll { $0 == path }
    }

    /// The prompt the Session starts with: the text as Markdown, then the files by path.
    static func prompt(text: String, files: [String]) -> String {
        let text = ListText.markdown(text).trimmingCharacters(in: .whitespacesAndNewlines)
        return files.isEmpty ? text : text + "\n\nFiles:\n" + files.map { "- " + $0 }.joined(separator: "\n")
    }

    public var canStart: Bool {
        place != nil && harness != nil && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !starting
    }

    public func load() async {
        do {
            palette = try await api.settings().palette
            let found = try await api.startProjects()
            projects = found.map(\.project)
            lastHarness = Dictionary(found.compactMap { p in p.lastHarness.map { (p.project.id, $0) } }, uniquingKeysWith: { a, _ in a })
            harnesses = try await api.startHarnesses().filter { $0.canStart && $0.status.installed }.map(\.status)
            problem = nil
            // A Project kept from last time may be gone since.
            if case .project(let id)? = place, !projects.contains(where: { $0.id == id }) { place = nil }
        } catch {
            problem = "\(error)"
        }
        if harness == nil || !harnesses.contains(where: { $0.id == harness }) { harness = harnesses.first?.id }
        if case .project(let id)? = place { harness = offeredLastHarness(in: id) ?? harness }
        for p in projects where logos[p.id] == nil {
            if let path = p.logo { logos[p.id] = try? await api.logo(path) }
        }
    }

    /// Picks a Project; the Harness becomes the one last used in it.
    public func choose(project id: String) {
        place = .project(id)
        computer = nil
        harness = offeredLastHarness(in: id) ?? harness
        placementChanged()
    }

    public func choose(folder path: String) {
        place = .folder(path)
        placing?.cancel()
        placement = nil
        computer = nil
    }

    /// The folder picked, by its last component, when it is not a recognised Project.
    public var folderName: String? {
        if case .folder(let path)? = place { return URL(fileURLWithPath: path).lastPathComponent }
        return nil
    }

    /// Asks the Engine to start the Session; true when it did. A refusal keeps the text.
    @discardableResult
    public func start() async -> Bool {
        guard canStart, let place, let harness else { return false }
        starting = true
        defer { starting = false }
        do {
            try await api.startSession(harness: harness, at: place, prompt: Self.prompt(text: text, files: files), computer: computer)
            problem = nil
            text = ""
            files = []
            self.place = nil
            return true
        } catch let r as StartRefused {
            problem = r.message
        } catch {
            problem = "\(error)"
        }
        return false
    }

    // The Project menu's edits. The Engine checks and keeps each one, and tells every Ping from
    // the Project (`projects.changed`).

    public func rename(_ id: String, to name: String) async {
        guard let p = projects.first(where: { $0.id == id }), p.name != name else { return }
        await attempt { put(try await api.changeProject(id, name: name, colour: nil)) }
    }

    public func setEnabled(_ id: String, _ on: Bool) async {
        await attempt { put(try await api.setEnabled(id, on)) }
    }

    public func setColour(_ id: String, _ colour: String) async {
        await attempt { put(try await api.changeProject(id, name: nil, colour: colour)) }
    }

    /// Any image the Mac can read, at any size: it is sent as a small PNG (`ProjectLogo`).
    public func setLogo(_ id: String, image: Data) async {
        guard let image = ProjectLogo.prepared(from: image) else {
            problem = "Pong could not read that image. Choose another file."
            return
        }
        await attempt {
            let p = try await api.setLogo(id, image: image)
            put(p)
            if let path = p.logo { logos[id] = try await api.logo(path) }
        }
    }

    public func removeLogo(_ id: String) async {
        await attempt { put(try await api.removeLogo(id)) }
        if projects.first(where: { $0.id == id })?.logo == nil { logos[id] = nil }
    }

    private func put(_ p: Project) {
        if let i = projects.firstIndex(where: { $0.id == p.id }) { projects[i] = p }
    }

    private func attempt(_ work: () async throws -> Void) async {
        do {
            try await work()
            problem = nil
        } catch {
            problem = "\(error)"
        }
    }

    private func offeredLastHarness(in project: String) -> String? {
        lastHarness[project].flatMap { h in harnesses.contains { $0.id == h } ? h : nil }
    }
}
