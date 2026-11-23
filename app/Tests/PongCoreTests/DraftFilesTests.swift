import XCTest
@testable import PongCore

/// Files added to a reply are part of its draft: kept on disk, and gone only once it is sent.
@MainActor
final class DraftFilesTests: XCTestCase {
    func testFilesSurviveANewStoreAndGoOnlyWhenSent() throws {
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("draft-files-\(UUID()).json")
        defer { try? FileManager.default.removeItem(at: file) }
        let ref = SessionThread.SessionRef(harness: "claude-code", id: "s1")
        Drafts(file: file).setFiles(["/a/shot.png", "/b/notes.txt"], .ping("p1"))
        Drafts(file: file).setFiles(["/c/log.txt"], .session(ref))
        XCTAssertEqual(Drafts(file: file).files(.ping("p1")), ["/a/shot.png", "/b/notes.txt"])
        XCTAssertEqual(Drafts(file: file).files(.session(ref)), ["/c/log.txt"])
        let d = Drafts(file: file)
        d.set("see these", .ping("p1"))
        d.sent(.ping("p1"))
        XCTAssertEqual(Drafts(file: file).files(.ping("p1")), [])
        XCTAssertEqual(Drafts(file: file).text(.ping("p1")), "")
        XCTAssertEqual(Drafts(file: file).files(.session(ref)), ["/c/log.txt"], "another draft keeps its Files")
    }

    func testAPastedImageIsKeptAtOnceAndGoesWithThePong() throws {
        let path = try PongFile.keep(Data("png".utf8), ext: "png")
        defer { try? FileManager.default.removeItem(atPath: path) }
        let f = try XCTUnwrap(PongFile.at(path))
        XCTAssertEqual(f.size, 3)
        XCTAssertEqual(f.sha256?.count, 64)
        XCTAssertTrue(path.hasPrefix(PongFile.draftsFolder.path))
        let body = try JSONSerialization.jsonObject(with: JSONEncoder().encode(Ping.Pong(text: "this", files: [f]))) as? [String: Any]
        let files = try XCTUnwrap(body?["files"] as? [[String: Any]])
        XCTAssertEqual(files.first?["path"] as? String, path)
        XCTAssertEqual(files.first?["size"] as? Int, 3)
        let plain = try JSONSerialization.jsonObject(with: JSONEncoder().encode(Ping.Pong(text: "no files"))) as? [String: Any]
        XCTAssertNil(plain?["files"], "a Pong without Files says nothing of them")
    }

    func testFilesPastTheLimitAreNotAdded() {
        XCTAssertNil(PongFile.problem(with: []))
        XCTAssertNotNil(PongFile.sending(["/nowhere/x"]).problem)
    }
}
