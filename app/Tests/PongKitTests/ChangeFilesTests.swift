import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// A change to several files shows one file at a time, with a picker of them.
@MainActor
final class ChangeFilesTests: XCTestCase {
    static let threeFiles = """
    diff --git a/internal/engine/engine.go b/internal/engine/engine.go
    index 1111111..2222222 100644
    --- a/internal/engine/engine.go
    +++ b/internal/engine/engine.go
    @@ -10,3 +10,4 @@ func Start() {
     	open()
    -	wait()
    +	waitFor(ctx)
    +	ready()
    diff --git a/app/Sources/PongKit/Bar.swift b/app/Sources/PongKit/Bar.swift
    new file mode 100644
    --- /dev/null
    +++ b/app/Sources/PongKit/Bar.swift
    @@ -0,0 +1,2 @@
    +import SwiftUI
    +struct Bar {}
    diff --git a/old.txt b/old.txt
    deleted file mode 100644
    --- a/old.txt
    +++ /dev/null
    @@ -1 +0,0 @@
    -gone
    """

    func testADiffSplitsIntoItsFiles() {
        let files = DiffFile.split(Self.threeFiles)
        XCTAssertEqual(files.map(\.path), ["internal/engine/engine.go", "app/Sources/PongKit/Bar.swift", "old.txt"])
        XCTAssertEqual(files.map(\.added), [2, 2, 0])
        XCTAssertEqual(files.map(\.removed), [1, 0, 1])
        XCTAssertEqual(files[1].lines.first, "diff --git a/app/Sources/PongKit/Bar.swift b/app/Sources/PongKit/Bar.swift")
        XCTAssertEqual(files.map(\.lines.count).reduce(0, +), splitLines(Self.threeFiles).count, "no line is lost")
    }

    func testAPlainDiffWithoutGitHeadersSplitsToo() {
        let plain = "--- a.txt\n+++ a.txt\n@@ -1 +1 @@\n-a\n+b\n--- b.txt\n+++ b.txt\n@@ -1 +1 @@\n-c\n+d"
        XCTAssertEqual(DiffFile.split(plain).map(\.path), ["a.txt", "b.txt"])
        XCTAssertEqual(DiffFile.split("just text").count, 0)
    }

    func testAChangeToSeveralFilesPreviewsOneAtATime() throws {
        XCTAssertTrue(registerBrandFont())
        let r = Resource(id: "r-change", title: "Uncommitted change", kind: "change", contentType: "text/plain; charset=utf-8")
        try assertSnapshot(ResourcePreview(resource: r, data: Data(Self.threeFiles.utf8), onClose: {}), named: "change-files")
    }
}
