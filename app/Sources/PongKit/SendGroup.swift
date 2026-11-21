import SwiftUI

/// Send and the caret beside it, as one button group (prototype: `.sendgrp`). The caret opens
/// the ways to send and hand back; it opens whenever the Ping waits, and looks ready to send
/// once text is typed.
struct SendGroup: View {
    var canSend: Bool
    /// The Ping waits: hand back is always there behind the caret.
    var canOpen: Bool
    /// Text is typed, so the send choices behind the caret can be used.
    var typed: Bool
    var send: () -> Void
    /// Nil where there is one way to send, as in a Thread: Send then stands alone.
    var caret: (() -> Void)?

    var body: some View {
        HStack(spacing: 2) {
            HoverButton(action: send) { hover in
                Image(systemName: "arrow.up").fontWeight(Brand.iconWeight).imageScale(.large)
                    .font(Brand.Style.answer.font)
                    .foregroundStyle(canSend ? Brand.onSelected : Brand.secondary)
                    .frame(width: 40, height: 40)
                    .background(canSend ? (hover ? Brand.selectedHover : Brand.selected) : Brand.row)
                    .contentShape(Rectangle())
            }
            .disabled(!canSend)
            .accessibilityLabel("Send")
            if let caret {
                HoverButton(action: caret) { hover in
                    Image(systemName: "chevron.down").fontWeight(Brand.iconWeight).imageScale(.large)
                        .font(Brand.Style.label.font)
                        .foregroundStyle(typed ? Brand.onSelected : canOpen ? Brand.text : Brand.secondary)
                        .frame(width: 26, height: 40)
                        .background(typed ? (hover ? Brand.selectedHover : Brand.selected) : hover && canOpen ? Brand.rowHover : Brand.row)
                        .contentShape(Rectangle())
                }
                .disabled(!canOpen)
                .help("More ways to send, or answer in the Harness")
                .accessibilityLabel("More ways to send")
            }
        }
    }
}

/// The caret's choices: the two ways to send, and hand back, drawn inside the card above the reply row as in the prototype
/// (`.menu`), so no window is needed beside the non-key card and clicks reach it as they
/// reach the card's other buttons.
struct SendMenu: View {
    /// How full this Session's context is, 0…1, when known.
    var used: Double?
    /// Text is typed: the two ways to send can be used.
    var canSend: Bool
    /// False on a Ping that blocks the agent: its Pong cannot start a new Session.
    var offersNewSession: Bool
    var newSession: () -> Void
    var thisSession: () -> Void
    /// Nil on another computer's Ping: handing back happens only at the computer running the Session.
    var handBack: (() -> Void)?

    var body: some View {
        VStack(spacing: 0) {
            if offersNewSession {
                Choice(title: "New Session with this Pong", enabled: canSend, action: newSession)
            }
            Choice(title: "Send to this Session", detail: Self.thisSessionDetail(used), enabled: canSend, action: thisSession)
            if let handBack {
                Choice(title: "Answer in the Harness instead", action: handBack)
            }
        }
        .padding(6)
        .frame(width: 290)
        .background(Brand.rowHover, in: Rectangle())
        .brandShadow()
    }

    /// The prototype's wording; the context's fullness comes from the Engine's figures.
    static func thisSessionDetail(_ used: Double?) -> String {
        guard let used else { return "Continues where it left off" }
        let pct = Int((used * 100).rounded())
        return ContextRing.isFull(used) ? "Its memory is \(pct)% full" : "\(pct)% of its memory is used"
    }

    struct Choice: View {
        var title: String
        var detail: String? = nil
        var enabled = true
        var action: () -> Void
        @State private var hover = false

        var body: some View {
            Button(action: action) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).brandStyle(enabled ? .answer : .body)
                        .foregroundStyle(enabled ? Brand.text : Brand.secondary)
                    if let detail {
                        Text(detail).brandStyle(.secondary).foregroundStyle(Brand.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 9)
                .padding(.horizontal, 10)
                .background(hover && enabled ? Brand.row : .clear, in: Rectangle())
                .contentShape(Rectangle())
            }
            .buttonStyle(FocusedPlain())
            .disabled(!enabled)
            .hoverHighlight($hover)
            .accessibilityElement(children: .combine)
        }
    }
}
