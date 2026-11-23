import Foundation
import Observation

/// Every reply the human has started and not yet sent, on this Device: a Pong typed on a card and
/// text written in a Thread. A draft is never lost. It is written to disk at every change, so it
/// survives closing the card or Thread, moving to another Ping, quitting, a crash and a restart,
/// and it goes only when what it holds has been sent and taken.
@MainActor @Observable
public final class Drafts {
    public enum Key: Hashable, Sendable {
        /// The Pong being written to a Ping.
        case ping(String)
        /// Text written in a Session's Thread.
        case session(SessionThread.SessionRef)

        var raw: String {
            switch self {
            case .ping(let id): "ping:\(id)"
            case .session(let s): "session:\(s.harness)/\(s.id)"
            }
        }
    }

    /// The app's drafts, in its Application Support folder. Under tests it keeps them in memory, so
    /// no test reads or writes the human's.
    public static let shared = Drafts(file: NSClassFromString("XCTestCase") == nil ? defaultFile : nil)

    private static var defaultFile: URL? {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?
            .appendingPathComponent("Pong", isDirectory: true)
            .appendingPathComponent("drafts.json")
    }

    private var texts: [String: String]
    @ObservationIgnored private let file: URL?

    /// Drafts kept in `file`, or only in memory when it is nil.
    public init(file: URL?) {
        self.file = file
        texts = file.flatMap { try? Data(contentsOf: $0) }.flatMap { try? JSONDecoder().decode([String: String].self, from: $0) } ?? [:]
    }

    public func text(_ key: Key) -> String { texts[key.raw] ?? "" }

    /// Keeps the text; empty text removes the draft.
    public func set(_ text: String, _ key: Key) {
        guard texts[key.raw, default: ""] != text else { return }
        texts[key.raw] = text.isEmpty ? nil : text
        save()
    }

    /// The Files added to a draft, by path.
    public func files(_ key: Key) -> [String] {
        (texts["files:" + key.raw] ?? "").split(separator: "\n").map(String.init)
    }

    /// Keeps the draft's Files; none removes them.
    public func setFiles(_ paths: [String], _ key: Key) {
        let joined = paths.joined(separator: "\n")
        guard texts["files:" + key.raw, default: ""] != joined else { return }
        texts["files:" + key.raw] = joined.isEmpty ? nil : joined
        save()
    }

    /// What was sent has been taken: the draft and its Files go.
    public func sent(_ key: Key) {
        set("", key)
        setFiles([], key)
    }

    private func save() {
        guard let file else { return }
        do {
            try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
            try JSONEncoder().encode(texts).write(to: file, options: .atomic)
        } catch {
            // Kept in memory still; the next change tries the disk again.
        }
    }
}
