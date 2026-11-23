import CryptoKit
import Foundation

/// One File the human sends with a Pong, a Thread's text or a first prompt: a path on this
/// computer, which the Engine gives the Session's Harness to read.
public struct PongFile: Codable, Equatable, Sendable {
    public var name: String
    public var size: Int64
    public var sha256: String?
    public var path: String?

    public init(name: String, size: Int64, sha256: String? = nil, path: String? = nil) {
        self.name = name; self.size = size; self.sha256 = sha256; self.path = path
    }

    /// The most Files one Pong or first prompt carries.
    public static let limit: Int64 = 25 << 20

    /// The File at a path, with its size and checksum, or nil when it cannot be read.
    public static func at(_ path: String) -> PongFile? {
        let url = URL(fileURLWithPath: path)
        guard let data = try? Data(contentsOf: url, options: .mappedIfSafe) else { return nil }
        let sum = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
        return PongFile(name: url.lastPathComponent, size: Int64(data.count), sha256: sum, path: path)
    }

    /// The size of the Files at these paths, together.
    public static func size(of paths: [String]) -> Int64 {
        paths.reduce(0) { $0 + (((try? FileManager.default.attributesOfItem(atPath: $1))?[.size] as? NSNumber)?.int64Value ?? 0) }
    }

    /// Why these Files cannot all go with one Pong, or nil when they can.
    public static func problem(with paths: [String]) -> String? {
        let total = size(of: paths)
        guard total > limit else { return nil }
        return "At most \(limit >> 20) megabytes of Files go with one Pong; these come to \((total + (1 << 20) - 1) >> 20) megabytes."
    }

    /// The Files at these paths as they go, or why one cannot.
    public static func sending(_ paths: [String]) -> (files: [PongFile], problem: String?) {
        var out: [PongFile] = []
        for p in paths {
            guard let f = at(p) else {
                return ([], "The File \(URL(fileURLWithPath: p).lastPathComponent) cannot be read, so nothing was sent. Remove it or add it again.")
            }
            out.append(f)
        }
        return (out, problem(with: paths))
    }

    /// Where pasted images are kept until their draft is sent: the Engine's Files folder, which a
    /// Harness may read. Under tests, a temporary folder.
    public static var draftsFolder: URL {
        if NSClassFromString("XCTestCase") != nil {
            return FileManager.default.temporaryDirectory.appendingPathComponent("pong-test-files/drafts", isDirectory: true)
        }
        return (FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first ?? FileManager.default.temporaryDirectory)
            .appendingPathComponent("Pong/files/drafts", isDirectory: true)
    }

    /// Writes pasted bytes to the drafts folder at once, so the draft keeps them; gives the path.
    public static func keep(_ data: Data, ext: String) throws -> String {
        try FileManager.default.createDirectory(at: draftsFolder, withIntermediateDirectories: true)
        let url = draftsFolder.appendingPathComponent("Pasted \(UUID().uuidString.prefix(8)).\(ext)")
        try data.write(to: url, options: .atomic)
        return url.path
    }
}
