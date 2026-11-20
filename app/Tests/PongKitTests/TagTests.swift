import XCTest
@testable import PongKit

/// A Ping's Tags decode from the event stream in the order the agent sent them.
final class TagTests: XCTestCase {
    func event(_ tags: String) -> String {
        #"data: {"seq":3,"kind":"ping.created","at":"2026-11-19T10:00:00Z","data":{"id":"p1","session":"s1","folder":"/tmp/p","question":"Run the migration?","answers":[],"state":"waiting","created_at":"2026-11-19T10:00:00Z","updated_at":"2026-11-19T10:00:00Z""# + tags + "}}"
    }

    func testTagsDecodeInOrder() throws {
        let ping = try XCTUnwrap(EventLine.parse(event(#","tags":[{"text":"prod","icon":"server.rack","colour":"rose"},{"text":"LIN-123","icon":"no.such.icon","link":"vscode://file/tmp/p"},{"text":"api"}]"#))?.ping)
        XCTAssertEqual(ping.tags, [
            Tag(text: "prod", icon: "server.rack", colour: "rose"),
            Tag(text: "LIN-123", icon: "no.such.icon"),
            Tag(text: "api"),
        ])
    }

    func testPingWithoutTagsHasNone() throws {
        let ping = try XCTUnwrap(EventLine.parse(event(""))?.ping)
        XCTAssertNil(ping.tags)
    }
}
