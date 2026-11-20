import XCTest
@testable import PongDocument

final class HighlightTests: XCTestCase {
    func kinds(_ code: String, _ lang: String?) -> [String: CodeRun.Kind] {
        var out: [String: CodeRun.Kind] = [:]
        for r in Highlight.runs(code, language: lang) { if let k = r.kind { out[r.text] = k } }
        return out
    }

    func testSwift() {
        let k = kinds("let n = 42 // the answer\nfunc f() -> String { \"hi\" }", "swift")
        XCTAssertEqual(k["let"], .keyword)
        XCTAssertEqual(k["func"], .keyword)
        XCTAssertEqual(k["42"], .number)
        XCTAssertEqual(k["// the answer"], .comment)
        XCTAssertEqual(k["\"hi\""], .string)
        XCTAssertNil(k["f"])
    }

    func testHashCommentsAndEscapedQuotes() {
        let k = kinds("if [ -z \"$a\\\"b\" ]; then # check\n  echo 'x'\nfi", "bash")
        XCTAssertEqual(k["if"], .keyword)
        XCTAssertEqual(k["\"$a\\\"b\""], .string)
        XCTAssertEqual(k["# check"], .comment)
        XCTAssertEqual(k["'x'"], .string)
    }

    func testBlockCommentSpansLines() {
        let k = kinds("/* one\ntwo */ return", "go")
        XCTAssertEqual(k["/* one\ntwo */"], .comment)
        XCTAssertEqual(k["return"], .keyword)
    }

    func testPythonDocstringSpansLines() {
        let k = kinds("def f():\n    \"\"\"Says hi.\n    Twice.\"\"\"\n    return 1", "python")
        XCTAssertEqual(k["\"\"\"Says hi.\n    Twice.\"\"\""], .string)
        XCTAssertEqual(k["return"], .keyword)
    }

    /// A quote left open ends at its line, so the next line is read as code.
    func testAnOpenQuoteEndsAtItsLine() {
        let runs = Highlight.runs("echo don't\nif x", language: "bash")
        XCTAssertEqual(runs.first { $0.kind == .string }?.text, "'t")
        XCTAssertEqual(kinds("echo don't\nif x", "bash")["if"], .keyword)
    }

    /// The text is kept whole, and an unknown language is left plain.
    func testTextIsKeptAndUnknownIsPlain() {
        let code = "SELECT * FROM t -- rows\nwhere x = 'a'"
        XCTAssertEqual(Highlight.runs(code, language: "sql").map(\.text).joined(), code)
        XCTAssertEqual(kinds(code, "sql")["where"], .keyword, "SQL keywords are any case")
        XCTAssertEqual(Highlight.runs(code, language: "klingon").compactMap(\.kind), [])
        XCTAssertEqual(Highlight.runs(code, language: nil).compactMap(\.kind), [])
    }
}
