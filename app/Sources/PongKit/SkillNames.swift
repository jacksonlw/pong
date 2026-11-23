import Foundation
import Observation
import PongCore

/// The names of the Skills a Harness offers in a Project or folder, as the Engine on the
/// Session's computer read them. Only names; each box loads them as it shows.
@MainActor @Observable
public final class SkillNames {
    /// Where the Skills are meant: a Harness in a Project or a folder, on a computer.
    public struct Place: Hashable, Sendable {
        public var harness: String
        public var project: String?
        public var folder: String?
        public var computer: String?
        public init(harness: String, project: String? = nil, folder: String? = nil, computer: String? = nil) {
            self.harness = harness; self.project = project; self.folder = folder; self.computer = computer
        }
    }

    private var found: [Place: [String]] = [:]
    @ObservationIgnored private let fetch: @Sendable (Place) async -> [String]?

    public init(fetch: @escaping @Sendable (Place) async -> [String]?) { self.fetch = fetch }

    /// Asks the Engine again; the names offered change once it answers.
    public func load(_ place: Place?) async {
        guard let place, SkillCompletion.trigger(for: place.harness) != nil else { return }
        if let names = await fetch(place), names != found[place] { found[place] = names }
    }

    /// What a box at this place offers: none for a Harness without a trigger.
    public func offer(_ place: Place?) -> SkillOffer {
        guard let place, let t = SkillCompletion.trigger(for: place.harness) else { return .none }
        return SkillOffer(trigger: t, names: found[place] ?? [])
    }
}

public extension EngineClient {
    /// The Skill names the Engine reads for a Harness in a Project or folder.
    func skills(_ p: SkillNames.Place) async throws -> [String] {
        struct Body: Decodable { var skills: [String] }
        var c = URLComponents()
        c.path = "/skills"
        c.queryItems = [URLQueryItem(name: "harness", value: p.harness)]
        if let v = p.project { c.queryItems?.append(URLQueryItem(name: "project", value: v)) }
        if let v = p.folder { c.queryItems?.append(URLQueryItem(name: "folder", value: v)) }
        if let v = p.computer { c.queryItems?.append(URLQueryItem(name: "computer", value: v)) }
        let b: Body = try await json(c.string ?? "")
        return b.skills
    }
}
