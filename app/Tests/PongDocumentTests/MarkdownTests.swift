import Foundation
import XCTest
@testable import PongDocument

/// A document Resource read as GitHub-flavoured markdown into the blocks both apps draw.
final class MarkdownTests: XCTestCase {
    func plain(_ s: AttributedString) -> String { String(s.characters) }

    /// The ticket list that showed its table as pipes: a table is a table.
    func testTableIsATable() throws {
        let src = """
        # Tickets for stopping a turn

        | # | Ticket | Blocked by |
        |---|:---|---:|
        | [01](01.md) | Record how a hook halts a turn | None |
        | 03 | Claude Code ends the turn | 01, 02 |
        """
        let blocks = MarkdownReader.blocks(src)
        XCTAssertEqual(blocks.count, 2)
        guard case let .table(t) = blocks[1] else { return XCTFail("not a table: \(blocks[1])") }
        XCTAssertEqual(t.head.map(plain), ["#", "Ticket", "Blocked by"])
        XCTAssertEqual(t.alignments, [nil, .leading, .trailing])
        XCTAssertEqual(t.rows.map { $0.map(plain) }, [["01", "Record how a hook halts a turn", "None"],
                                                     ["03", "Claude Code ends the turn", "01, 02"]])
        XCTAssertEqual(t.rows[0][0].runs.first?.link, URL(string: "01.md"))
    }

    func testInlineStyles() throws {
        let blocks = MarkdownReader.blocks("Some **bold**, *slanted*, `code`, ~~gone~~ and [a link](https://example.com).\nSame paragraph.")
        guard case let .paragraph(p) = blocks.first else { return XCTFail() }
        XCTAssertEqual(plain(p), "Some bold, slanted, code, gone and a link. Same paragraph.")
        func intent(_ word: String) -> InlinePresentationIntent? {
            let r = p.range(of: word)!
            return p[r].runs.first?.inlinePresentationIntent
        }
        XCTAssertEqual(intent("bold"), .stronglyEmphasized)
        XCTAssertEqual(intent("slanted"), .emphasized)
        XCTAssertEqual(intent("code"), .code)
        XCTAssertEqual(intent("gone"), .strikethrough)
        XCTAssertEqual(p[p.range(of: "a link")!].runs.first?.link, URL(string: "https://example.com"))
    }

    /// A click on a link in text goes straight to the system, so a document may link only to the
    /// web: a link or picture that names a file, a script or another app is shown as its words.
    func testOnlyWebLinksAreLinks() throws {
        let src = "[web](https://example.com) [file](file:///tmp/x.command) [share](smb://host/share) [app](shortcuts://run-shortcut?name=x) [js](javascript:alert(1)) ![pic](file:///etc/passwd) [page](01.md)"
        guard case let .paragraph(p) = MarkdownReader.blocks(src).first else { return XCTFail() }
        XCTAssertEqual(plain(p), "web file share app js pic page")
        func link(_ word: String) -> URL? { p[p.range(of: word)!].runs.first?.link }
        XCTAssertEqual(link("web"), URL(string: "https://example.com"))
        XCTAssertEqual(link("page"), URL(string: "01.md"))
        for word in ["file", "share", "app", "js", "pic"] { XCTAssertNil(link(word), word) }
    }

    /// A web address written bare, as in a setup guide's steps, is a link; one in code and a name
    /// without its scheme stay as they are.
    func testBareAddressIsALink() throws {
        let src = "- Open https://developer.apple.com/account/resources/certificates/add, then `https://not.a/link` and example.com."
        guard case let .list(_, items) = MarkdownReader.blocks(src).first,
              case let .paragraph(p) = items.first?.blocks.first else { return XCTFail() }
        let url = "https://developer.apple.com/account/resources/certificates/add"
        XCTAssertEqual(p[p.range(of: url)!].runs.first?.link, URL(string: url))
        XCTAssertEqual(p.runs.filter { $0.link != nil }.count, 1)
        XCTAssertEqual(plain(p), "Open \(url), then https://not.a/link and example.com.")
    }

    /// Nested lists keep their nesting, ordered lists their start, and task items their box.
    func testLists() throws {
        let src = """
        3. Third
        4. Fourth
           - inner

        - [x] done
        - [ ] not yet
        """
        let blocks = MarkdownReader.blocks(src)
        guard case let .list(start, items) = blocks[0], case let .list(nil, tasks) = blocks[1] else { return XCTFail("\(blocks)") }
        XCTAssertEqual(start, 3)
        XCTAssertEqual(items.count, 2)
        guard case let .list(nil, inner) = items[1].blocks.last else { return XCTFail("\(items[1])") }
        XCTAssertEqual(inner.count, 1)
        XCTAssertEqual(tasks.map(\.checked), [true, false])
    }

    func testCodeQuoteRuleAndMermaid() throws {
        let src = """
        ```swift
        let x = 1
        ```

        > Quoted **text**

        ---

        ```mermaid
        graph TD; A-->B
        ```
        """
        XCTAssertEqual(MarkdownReader.blocks(src).map(\.kindName), ["code", "quote", "rule", "mermaid"])
        guard case let .code(lang, text) = MarkdownReader.blocks(src)[0] else { return XCTFail() }
        XCTAssertEqual(lang, "swift")
        XCTAssertEqual(text, "let x = 1")
        guard case let .mermaid(m) = MarkdownReader.blocks(src)[3] else { return XCTFail() }
        XCTAssertEqual(m, "graph TD; A-->B")
    }

    /// The recorded migration plan reads as before.
    func testRecordedPlan() throws {
        let src = """
        # Migration plan

        Rewrite customer emails in **two tables**.

        1. Back up `users` and `orders`.
        """
        let blocks = MarkdownReader.blocks(src)
        guard case let .heading(1, h) = blocks[0], case let .list(1, items) = blocks[2],
              case let .paragraph(first) = items[0].blocks[0] else { return XCTFail("\(blocks)") }
        XCTAssertEqual(plain(h), "Migration plan")
        XCTAssertEqual(plain(first), "Back up users and orders.")
    }
}

extension MarkdownBlock {
    var kindName: String {
        switch self {
        case .heading: "heading"
        case .paragraph: "paragraph"
        case .list: "list"
        case .quote: "quote"
        case .code: "code"
        case .mermaid: "mermaid"
        case .table: "table"
        case .rule: "rule"
        }
    }
}
