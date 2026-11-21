import Foundation
import Observation

/// What the human has done on each card that the feed does not yet hold: typed text, ticked
/// answers, and the Pong they sent, so the card can say where it stands. Kept per Ping, so
/// text typed on one card stays there when the human moves to another. Typed text is kept in
/// `Drafts`, on disk, and goes only once the Pong carrying it is taken (`Inbox.send`).
@MainActor @Observable
public final class CardState {
    private let drafts: Drafts
    private var ticked: [String: [String]] = [:]
    private var mine: [String: Ping.Pong] = [:]
    private var refusals: [String: String] = [:]

    public init(drafts: Drafts? = nil) { self.drafts = drafts ?? .shared }

    public func draft(for ping: Ping) -> String { drafts.text(.ping(ping.id)) }
    public func setDraft(_ text: String, for ping: Ping) { drafts.set(text, .ping(ping.id)) }

    public func isTicked(_ label: String, on ping: Ping) -> Bool { ticked[ping.id]?.contains(label) == true }

    /// Ticks or unticks an answer of a Ping that allows several.
    public func toggle(_ label: String, on ping: Ping) {
        var t = ticked[ping.id] ?? []
        if let i = t.firstIndex(of: label) { t.remove(at: i) } else { t.append(label) }
        ticked[ping.id] = t
    }

    /// The Pong the reply row would send: the ticked answers in the Ping's order and the typed
    /// text, or nil when there is nothing to send.
    public func pong(for ping: Ping, newSession: Bool? = nil) -> Ping.Pong? {
        let text = draft(for: ping).trimmingCharacters(in: .whitespacesAndNewlines)
        let order = (ping.answers ?? []).map(\.label)
        let answers = order.filter { isTicked($0, on: ping) }
        guard !text.isEmpty || !answers.isEmpty else { return nil }
        return Ping.Pong(answers: answers.isEmpty ? nil : answers, text: text.isEmpty ? nil : text, newSession: newSession)
    }

    /// The human sent this Pong; the card's ticks are cleared. Its text stays until the Pong is
    /// taken, so a refused one keeps it.
    public func sent(_ pong: Ping.Pong, for ping: Ping) {
        mine[ping.id] = pong
        refusals[ping.id] = nil
        ticked[ping.id] = nil
    }

    public func refused(_ why: String, for ping: Ping) { refusals[ping.id] = why }

    /// The human withdrew their Pong: the Ping is theirs to answer again.
    public func withdrawn(_ ping: Ping) { mine[ping.id] = nil }

    public enum Outcome: Equatable {
        /// Sent from here, and the feed has not reported it yet.
        case sending
        /// Where the Pong stands; a Pong not yet delivered can be withdrawn.
        case delivery(String, withdrawable: Bool)
        /// Another Device answered first; this Pong was not used.
        case lost
        /// The feed refused it, or could not be reached: in words to show.
        case refused(String)
    }

    public func outcome(of ping: Ping) -> Outcome? {
        if let r = refusals[ping.id] { return .refused(r) }
        guard let sent = mine[ping.id] else {
            return ping.pong?.deliveryLine.map { .delivery($0, withdrawable: false) }
        }
        guard let recorded = ping.pong else { return .sending }
        if !Self.same(recorded, sent) { return .lost }
        guard let line = recorded.deliveryLine else { return nil }
        return .delivery(line, withdrawable: recorded.delivery == "sent")
    }

    private static func same(_ a: Ping.Pong, _ b: Ping.Pong) -> Bool {
        a.answer == b.answer && a.answers == b.answers && a.text == b.text
    }
}
