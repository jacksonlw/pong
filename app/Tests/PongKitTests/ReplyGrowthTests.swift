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
        let field = try XCTUnwrap(Self.find(NSTextField.self, in: host))
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

    static func find<T: NSView>(_ type: T.Type, in v: NSView) -> T? {
        if let t = v as? T { return t }
        for s in v.subviews { if let t = find(type, in: s) { return t } }
        return nil
    }
}
