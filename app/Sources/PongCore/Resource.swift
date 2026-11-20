import Foundation
import Observation

/// Something a Ping carries for the human to look at.
public struct Resource: Decodable, Identifiable, Equatable, Sendable {
    public var id: String
    public var title: String
    /// image | document | sheet | slides | link | change | error | output | file | text
    public var kind: String
    public var file: String?
    public var contentType: String

    public init(id: String, title: String, kind: String, file: String? = nil, contentType: String) {
        self.id = id; self.title = title; self.kind = kind; self.file = file; self.contentType = contentType
    }
}

extension Resource {
    /// A link opens like a Tag link instead of showing a preview.
    public var isLink: Bool { kind == "link" }

    /// The address a link Resource's content holds: its first line that is not a comment
    /// (the content is `text/uri-list`), or nil when there is none.
    public static func linkAddress(_ data: Data) -> String? {
        String(decoding: data, as: UTF8.self)
            .split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .first { !$0.isEmpty && !$0.hasPrefix("#") }
    }

    /// The host shown beneath a link's title, or the address itself when it has none.
    public static func linkHost(_ data: Data) -> String? {
        guard let address = linkAddress(data) else { return nil }
        return URL(string: address)?.host() ?? address
    }
}

/// A sheet Resource's content as the Engine stores it (`application/vnd.pong.sheet+json`): one
/// or more named sheets, each a list of rows of cell text, the first row being the header.
public struct SheetContent: Decodable, Equatable, Sendable {
    public struct Sheet: Decodable, Equatable, Sendable {
        public var name: String
        public var rows: [[String]]

        public init(name: String, rows: [[String]]) { self.name = name; self.rows = rows }

        /// As many columns as its longest row.
        public var columns: Int { rows.map(\.count).max() ?? 0 }

        /// Row `i` padded with empty cells to the sheet's width, so every column lines up.
        public func row(_ i: Int) -> [String] { rows[i] + Array(repeating: "", count: columns - rows[i].count) }
    }

    public var sheets: [Sheet]

    public init(sheets: [Sheet]) { self.sheets = sheets }

    /// Nil when the content is not a sheet.
    public static func decode(_ data: Data) -> SheetContent? { try? JSONDecoder().decode(SheetContent.self, from: data) }
}

extension EngineClient {
    /// A Resource's content as the Engine stored it.
    public func resource(_ resourceID: String, of pingID: String) async throws -> Data {
        var r = URLRequest(url: URL(string: "http://\(address)/pings/\(pingID)/resources/\(resourceID)")!)
        r.timeoutInterval = 10
        r.setValue("Bearer \(secret)", forHTTPHeaderField: "Authorization")
        let (data, resp) = try await URLSession(configuration: .ephemeral).data(for: r)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else { throw Failure(description: "GET resource \(resourceID): \(status)") }
        return data
    }
}

/// The Resource contents fetched so far, the Resources open in previews, and what opening one
/// does. The app gives it a way to fetch and to show previews; tests fill `contents` from
/// recorded files.
@MainActor @Observable
public final class ResourceStore {
    nonisolated public static let shared = ResourceStore()

    /// How a Resource is opened: a plain click, a Command click, a Shift click, or its own window.
    public enum Opening: Sendable {
        /// In place of the other docked previews.
        case alone
        /// Beside the previews already open; closes it when it is open.
        case added
        /// With every Resource between the last one opened and this one.
        case through
        /// In a preview of its own, apart from the card.
        case detached
    }

    public var contents: [String: Data] = [:]
    /// The Resources shown in previews, in the order they were opened.
    public private(set) var opened: [Resource] = []
    /// The opened Resources whose previews the human moved away from the card. They stay until
    /// the human closes them; the rest are docked beside the card.
    public private(set) var detached: Set<String> = []
    @ObservationIgnored public var fetch: (@Sendable (_ resourceID: String, _ pingID: String) async throws -> Data)?
    @ObservationIgnored public var onOpen: ((Ping, Resource) -> Void)?

    nonisolated public init() {}

    /// Fetches the content once; failures leave it missing and are retried on the next call.
    public func load(_ r: Resource, of ping: Ping) async {
        guard contents[r.id] == nil, let fetch else { return }
        if let data = try? await fetch(r.id, ping.id) { contents[r.id] = data }
    }

    public func isOpen(_ r: Resource) -> Bool { opened.contains { $0.id == r.id } }
    public func isDetached(_ r: Resource) -> Bool { detached.contains(r.id) }
    /// The docked previews' Resources, in the order they sit beside the card.
    public var docked: [Resource] { opened.filter { !detached.contains($0.id) } }

    /// Opens `r`, one of the Ping's Resources `all`, the way the click asked for.
    public func open(_ r: Resource, of ping: Ping, _ how: Opening = .alone, among all: [Resource] = []) {
        let before = Set(opened.map(\.id))
        switch how {
        case .alone:
            if !isDetached(r) { opened = opened.filter(isDetached) + [r] }
        case .added:
            if isOpen(r) { return close(r) }
            opened.append(r)
        case .through:
            let last = docked.last { d in all.contains { $0.id == d.id } }
            if let a = all.firstIndex(where: { $0.id == last?.id }), let b = all.firstIndex(where: { $0.id == r.id }) {
                opened += all[min(a, b)...max(a, b)].filter { !isOpen($0) }
            } else if !isOpen(r) {
                opened.append(r)
            }
        case .detached:
            if !isOpen(r) { opened.append(r) }
            detached.insert(r.id)
        }
        onOpen?(ping, r)
        for new in opened where !before.contains(new.id) { Task { await load(new, of: ping) } }
    }

    public func close(_ r: Resource) {
        opened.removeAll { $0.id == r.id }
        detached.remove(r.id)
    }

    /// Closes every preview, detached ones too: the human closed the card.
    public func closeAll() {
        opened = []
        detached = []
    }

    /// Closes the docked previews; the detached ones stay.
    public func closeDocked() { opened = opened.filter(isDetached) }

    /// The human moved the preview away from the card.
    public func detach(_ r: Resource) { if isOpen(r) { detached.insert(r.id) } }

    /// The human put the preview back beside the card, after the ones already docked.
    public func dock(_ r: Resource) {
        guard isOpen(r) else { return }
        detached.remove(r.id)
        opened = opened.filter { $0.id != r.id } + [r]
    }
}
