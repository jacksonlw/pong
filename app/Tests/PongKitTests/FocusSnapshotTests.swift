import SwiftUI
import XCTest
@testable import PongKit

/// The shared keyboard focus outline, drawn on a button-shaped row block and on a chosen
/// answer, on the card's surface.
@MainActor
final class FocusSnapshotTests: XCTestCase {
    func testFocusOutline() throws {
        let view = HStack(spacing: 16) {
            Text("Repair").brandStyle(.label).foregroundStyle(Brand.text)
                .padding(.horizontal, 12).frame(minHeight: 28)
                .background(Brand.row, in: Rectangle())
                .brandFocus(true)
            Text("Yes, ship it").brandStyle(.answer).foregroundStyle(Brand.onSelected)
                .padding(.vertical, 11).padding(.horizontal, 14)
                .background(Brand.selected, in: Rectangle())
                .brandFocus(true)
        }
        .padding(16)
        .background(Brand.surface)
        try assertSnapshot(view, named: "focus-outline")
    }

    /// Every control on a real card drawn as focused at once: the send and caret buttons, the
    /// answers, the tabs and the field. No outline may be cut by the card's layout.
    func testFocusOutlineInCard() throws {
        let ping = try XCTUnwrap(CardSnapshotTests.pings().first { $0.isWaiting && $0.answers?.count == 2 && $0.approves == nil && $0.allowSeveral == nil })
        let card = CardView(ping: ping, count: 1, draft: "Ship it")
            .environment(\.showsFocusEverywhere, true)
        try assertSnapshot(card, named: "focus-outline-card")
    }
}
