import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The reply box writes lists as the start panel's text box does, typed into a real window.
@MainActor
final class ReplyListTests: XCTestCase {
    func testAListIsShownAsBulletsAndSentAsMarkdown() async throws {
        XCTAssertTrue(registerBrandFont())
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true && $0.allowSeveral != true })
        final class Got { var pongs: [Ping.Pong] = []; var drafts: [String] = [] }
        let got = Got()
        var actions = CardActions()
        actions.send = { got.pongs.append($0) }
        actions.draft = { got.drafts.append($0) }
        let host = NSHostingView(rootView: CardView(ping: ping, actions: actions)
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
            .environment(\.typingAnimates, false))
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: cardWidth, height: 700), styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = host
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(100))
        let field = try XCTUnwrap(ReplyGrowthTests.reply(in: host))
        XCTAssertTrue(w.makeFirstResponder(field))
        try await Task.sleep(for: .milliseconds(100))
        let editor = try XCTUnwrap(w.firstResponder as? NSTextView)
        func type(_ s: String) async throws {
            for ch in s {
                editor.insertText(String(ch), replacementRange: editor.selectedRange())
                try await Task.sleep(for: .milliseconds(30))
            }
        }
        func pressReturn(shift: Bool) async throws {
            let e = try XCTUnwrap(NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: shift ? .shift : [], timestamp: 0,
                                                   windowNumber: w.windowNumber, context: nil, characters: "\r",
                                                   charactersIgnoringModifiers: "\r", isARepeat: false, keyCode: 36))
            w.sendEvent(e)
            try await Task.sleep(for: .milliseconds(100))
        }

        try await type("- one")
        XCTAssertEqual(editor.string, "• one", "a typed dash shows a bullet")
        try await pressReturn(shift: true)
        XCTAssertEqual(editor.string, "• one\n• ", "Shift-Return starts the next item")
        XCTAssertEqual(editor.selectedRange().location, 8)
        try await type("two")
        XCTAssertEqual(got.drafts.last, "- one\n- two", "the draft other Devices see is Markdown")
        try await pressReturn(shift: false)
        XCTAssertEqual(got.pongs.map(\.text), ["- one\n- two"], "Return sends the reply as Markdown")
    }
}
