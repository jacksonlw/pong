import AppKit
import SwiftUI
import XCTest
@testable import PongKit

/// The reply box is as tall as the lines its text takes, at every keystroke, typed into a
/// real window through the field's own editor.
@MainActor
final class ReplyGrowthTests: XCTestCase {
    func testTheBoxIsNeverTallerThanItsText() async throws {
        XCTAssertTrue(registerBrandFont())
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true })
        let host = NSHostingView(rootView: CardView(ping: ping)
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
            .environment(\.typingAnimates, false))
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: cardWidth, height: 700), styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = host
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(100))
        let field = try XCTUnwrap(Self.reply(in: host))
        XCTAssertTrue(w.makeFirstResponder(field))
        try await Task.sleep(for: .milliseconds(100))
        let editor = try XCTUnwrap(w.firstResponder as? NSTextView)
        let layout = try XCTUnwrap(editor.layoutManager), container = try XCTUnwrap(editor.textContainer)
        let line = layout.defaultLineHeight(for: try XCTUnwrap(editor.font))
        let empty = host.fittingSize.height
        var wrong: [String] = []
        func check() async throws {
            try await Task.sleep(for: .milliseconds(30))
            host.layoutSubtreeIfNeeded()
            layout.ensureLayout(for: container)
            let lines = min(5, max(1, Int((layout.usedRect(for: container).height / line).rounded())))
            // The box is its text and 10 pt above and below, and never under 40 pt.
            let box = max(40, CGFloat(lines) * line + 20)
            if host.fittingSize.height != empty - 40 + box {
                wrong.append("\(host.fittingSize.height - empty + 40) pt for \(lines) lines after \(editor.string.suffix(16).debugDescription)")
            }
        }
        func type(_ s: String) async throws {
            for ch in s {
                if ch == "\n" { editor.insertNewlineIgnoringFieldEditor(nil) } else { editor.insertText(String(ch), replacementRange: editor.selectedRange()) }
                try await check()
            }
        }
        // Words that wrap, a run with no place to break, spaces at a line's end, and new lines.
        try await type("the quick brown fox jumps over the lazy dog and keeps going the quick brown fox \n")
        try await type(String(repeating: "a", count: 70) + "    bb\n\nc")
        while !editor.string.isEmpty {
            editor.deleteBackward(nil)
            try await check()
        }
        XCTAssertEqual(wrong, [])
    }

    /// A reply longer than the box keeps its place when the box loses focus, and still scrolls.
    func testALongReplyKeepsItsPlaceAndScrollsWithoutFocus() async throws {
        XCTAssertTrue(registerBrandFont())
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true })
        let host = NSHostingView(rootView: CardView(ping: ping)
            .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
            .environment(\.typingAnimates, false))
        let other = NSTextField(frame: NSRect(x: 0, y: 0, width: 50, height: 20))
        let root = NSView(frame: NSRect(x: 0, y: 0, width: cardWidth, height: 700))
        host.frame = root.bounds
        root.addSubview(host)
        root.addSubview(other)
        let w = NSWindow(contentRect: root.frame, styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = root
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(100))
        let editor = try XCTUnwrap(Self.reply(in: host))
        XCTAssertTrue(w.makeFirstResponder(editor))
        for i in 1...12 {
            editor.insertText("line \(i)", replacementRange: editor.selectedRange())
            editor.insertNewlineIgnoringFieldEditor(nil)
        }
        try await Task.sleep(for: .milliseconds(100))
        let clip = try XCTUnwrap(editor.enclosingScrollView?.contentView)
        let bottom = clip.bounds.origin.y
        XCTAssertGreaterThan(bottom, 0, "typing past five lines scrolls to the caret")

        XCTAssertTrue(w.makeFirstResponder(other))
        try await Task.sleep(for: .milliseconds(100))
        host.layoutSubtreeIfNeeded()
        XCTAssertEqual(clip.bounds.origin.y, bottom, "losing focus keeps the place")

        let wheel = try XCTUnwrap(CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 1, wheel1: 2000, wheel2: 0, wheel3: 0))
        editor.enclosingScrollView?.scrollWheel(with: try XCTUnwrap(NSEvent(cgEvent: wheel)))
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(clip.bounds.origin.y, 0, "the wheel still scrolls it without focus")
        XCTAssertTrue(w.firstResponder === other.currentEditor() || w.firstResponder === other)
        XCTAssertEqual(editor.string.components(separatedBy: "\n").count, 13, "nothing typed is lost")
    }

    /// A draft from another Device does not replace what is being typed here; once nobody types
    /// here, a newer one does.
    func testADraftFromElsewhereWaitsWhileTyping() async throws {
        XCTAssertTrue(registerBrandFont())
        var ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.isNote != true })
        Drafts.shared.set("", .ping(ping.id))
        defer { Drafts.shared.set("", .ping(ping.id)) }
        func card(_ p: Ping) -> some View {
            CardView(ping: p)
                .environment(\.cardContext, CardContext(directory: Directory(), hover: ContextHover()))
                .environment(\.typingAnimates, false)
        }
        let host = NSHostingView(rootView: card(ping))
        let other = NSTextField(frame: NSRect(x: 0, y: 0, width: 50, height: 20))
        let root = NSView(frame: NSRect(x: 0, y: 0, width: cardWidth, height: 700))
        host.frame = root.bounds
        root.addSubview(host)
        root.addSubview(other)
        let w = NSWindow(contentRect: root.frame, styleMask: [.titled], backing: .buffered, defer: false)
        w.contentView = root
        w.makeKeyAndOrderFront(nil)
        defer { w.orderOut(nil) }
        try await Task.sleep(for: .milliseconds(100))
        let editor = try XCTUnwrap(Self.reply(in: host))
        XCTAssertTrue(w.makeFirstResponder(editor))
        editor.insertText("mine", replacementRange: editor.selectedRange())
        try await Task.sleep(for: .milliseconds(100))

        ping.draft = Ping.Draft(text: "theirs")
        host.rootView = card(ping)
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(editor.string, "mine", "typing here wins")

        XCTAssertTrue(w.makeFirstResponder(other))
        try await Task.sleep(for: .milliseconds(100))
        ping.draft = Ping.Draft(text: "theirs, later")
        host.rootView = card(ping)
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(editor.string, "theirs, later", "a newer one shows once nobody types here")
    }

    /// The reply box's text view.
    static func reply(in v: NSView) -> NSTextView? {
        if let t = v as? NSTextView, t.isEditable, !t.isFieldEditor { return t }
        for s in v.subviews { if let t = reply(in: s) { return t } }
        return nil
    }

    static func find<T: NSView>(_ type: T.Type, in v: NSView) -> T? {
        if let t = v as? T { return t }
        for s in v.subviews { if let t = find(type, in: s) { return t } }
        return nil
    }
}
