import Foundation
import Observation

/// What the feed reports, for the card to render. It keeps the Pings the card can move
/// between in the Queue's order, refetching them whenever the event stream says something
/// changed; it decides nothing about the Pings themselves.
///
/// The deck is each Ping the card has already shown, so that going back to an answered Ping
/// shows what was chosen, followed by every waiting Ping.
@MainActor @Observable
public final class Inbox {
    public private(set) var deck: [Ping] = []
    public private(set) var currentID: String?
    public private(set) var lastError: String?
    /// Every Ping the feed reports, to find each Session's latest.
    private var all: [Ping] = []
    /// An answered Ping that said more are coming, while the card waits for its Session's next.
    private var writingAfter: Ping?

    /// Ids of Pings the card has shown; they stay in the deck after they stop waiting.
    @ObservationIgnored private var shown: Set<String> = []
    /// True after the human moved with the arrows: the card then stays where they put it.
    @ObservationIgnored private var browsing = false
    @ObservationIgnored private var client: (any PingFeed)?
    /// The human tucked the card away. Interface state only: the feed is told nothing.
    public private(set) var hidden = false
    /// Ids of every waiting Ping seen so far, to tell a new Ping from one already waiting.
    @ObservationIgnored private var seenWaiting: Set<String> = []
    /// The human's unsent replies, kept on disk until what they hold is sent and taken.
    @ObservationIgnored public var drafts = Drafts.shared

    public init(pings: [Ping] = []) {
        apply(pings)
    }

    public var current: Ping? { deck.first { $0.id == currentID } }
    /// The current Ping's place in the deck, from 0.
    public var position: Int { deck.firstIndex { $0.id == currentID } ?? 0 }
    /// The card is shown while any Ping waits, or any Pong came back undelivered.
    public var hasWaiting: Bool { deck.contains(where: \.needsHuman) }

    /// The card's Ping is answered and its Session's next Ping is still being written.
    public var isWriting: Bool { writingAfter != nil && writingAfter?.id == currentID }

    /// The agent is still asking: the latest Ping of the card's Session says more are coming
    /// (so the next has not arrived). Shown as the typing badge, except while the card itself
    /// says the next question is being written.
    public var moreComing: Bool {
        guard let c = current, !isWriting else { return false }
        return latest(of: c.session)?.moreComing == true
    }

    /// What the card shows, or nil when it is hidden.
    public var scene: CardScene? {
        guard !hidden, hasWaiting || isWriting, let c = current else { return nil }
        return CardScene(ping: c, writing: isWriting, position: position)
    }

    /// The Session's latest Ping or Note; a Thought between two Pings is not one.
    private func latest(of session: String) -> Ping? {
        all.filter { $0.session == session && !$0.isThought }.max { ($0.createdAt ?? "") < ($1.createdAt ?? "") }
    }

    /// Follows a feed until the task is cancelled, reconnecting with catch-up when the
    /// stream drops. `clientProvider` is asked again after each drop (the Mac's Engine may
    /// have restarted on a new port).
    public func follow(clientProvider: @escaping @Sendable () async -> (any PingFeed)?, retry: Duration = .milliseconds(500)) async {
        while !Task.isCancelled {
            if let c = await clientProvider() {
                client = c
                do {
                    // The list, then every change after it. An events-only catch-up from the log's
                    // start would mean one fetch for each event ever kept.
                    var since = 0
                    if let s = c as? any PingFeedAtSeq {
                        let (all, seq) = try await s.allPingsAtSeq()
                        apply(all)
                        since = seq
                    } else {
                        try await refresh()
                    }
                    for try await _ in c.events(since: since) { refreshSoon() }
                } catch {
                    lastError = "\(error)"
                }
            }
            try? await Task.sleep(for: retry)
        }
    }

    @ObservationIgnored private var refreshing = false
    @ObservationIgnored private var refreshAgain = false

    /// Fetches the list once for each burst of events: an event that arrives while a fetch is
    /// on its way asks for one more fetch after it, not one each.
    private func refreshSoon() {
        refreshAgain = true
        guard !refreshing else { return }
        refreshing = true
        Task {
            defer { refreshing = false }
            while refreshAgain {
                refreshAgain = false
                do { try await refresh() } catch { lastError = "\(error)" }
            }
        }
    }

    public func previous() { move(by: -1) }
    public func next() { move(by: 1) }

    /// Why the feed last refused a Pong for each Ping, in its words; cleared on the next send.
    public private(set) var refusals: [String: String] = [:]

    /// Sends a Pong for a Ping. If the feed refuses it, the Ping still waits and its
    /// refusal is kept for the card to show.
    public func send(_ pong: Ping.Pong, for ping: Ping) {
        refusals[ping.id] = nil
        // The draft goes only once the Pong is taken: refused or unsent, it stays on the card.
        act(refused: { [weak self] in self?.refusals[ping.id] = $0 },
            taken: { [weak self] in self?.drafts.sent(.ping(ping.id)) }) { try await $0.pong(ping.id, pong) }
    }

    public func handBack(_ ping: Ping) {
        act { try await $0.handBack(ping.id) }
    }

    public func dismiss(_ ping: Ping) {
        act { try await $0.dismiss(ping.id) }
    }

    @ObservationIgnored private var drafting: [String: Task<Void, Never>] = [:]

    /// Keeps what the human is typing for a Ping on this Device at once, and shares it a moment
    /// after they pause.
    public func draft(_ text: String, for ping: Ping) {
        drafts.set(text, .ping(ping.id))
        guard let client else { return }
        drafting[ping.id]?.cancel()
        drafting[ping.id] = Task {
            try? await Task.sleep(for: .milliseconds(400))
            guard !Task.isCancelled else { return }
            try? await client.draft(ping.id, text: text)
        }
    }

    /// Sends an undelivered or unconfirmed Pong again. A refusal is kept for the card to show.
    public func sendAgain(_ ping: Ping, newSession: Bool) {
        refusals[ping.id] = nil
        act(refused: { [weak self] in self?.refusals[ping.id] = $0 }) { try await $0.sendAgain(ping.id, newSession: newSession) }
    }

    /// Withdraws a Pong still waiting to be delivered. A refusal is kept for the card to show.
    public func withdraw(_ ping: Ping) {
        refusals[ping.id] = nil
        // The withdrawn Pong's text comes back as the draft, unless something new was typed since.
        let text = ping.pong?.text ?? ""
        act(refused: { [weak self] in self?.refusals[ping.id] = $0 },
            taken: { [weak self] in
                guard let self, !text.isEmpty, self.drafts.text(.ping(ping.id)).isEmpty else { return }
                self.drafts.set(text, .ping(ping.id))
            }) { try await $0.withdraw(ping.id) }
    }

    /// Keeps an undelivered or unconfirmed Pong without sending it again.
    public func leave(_ ping: Ping) {
        act { try await $0.leave(ping.id) }
    }

    private func act(refused: ((String) -> Void)? = nil, taken: (() -> Void)? = nil, _ call: @escaping @Sendable (any PingFeed) async throws -> Void) {
        guard let client else { return }
        browsing = false
        let unreachable = client.unreachable
        Task {
            do {
                try await call(client)
            } catch {
                lastError = "\(error)"
                refused?((error as? FeedRefusal)?.message ?? unreachable)
                return
            }
            taken?()
            do { try await refresh() } catch { lastError = "\(error)" }
        }
    }

    private func move(by step: Int) {
        let i = position + step
        guard deck.indices.contains(i) else { return }
        browsing = true
        writingAfter = nil
        setCurrent(deck[i].id)
    }

    /// How many Pings wait in the feed (Notes included: they wait until dismissed), for
    /// the Bar while the card is hidden.
    public var waitingCount: Int { all.filter(\.needsHuman).count }

    /// Tucks the card away. Sends nothing: the Ping keeps waiting and its agent stays held.
    public func hide() { hidden = true }

    /// The Bar's waiting button was clicked: a hidden card comes back on the first waiting Ping.
    /// Says whether it did.
    @discardableResult public func unhide() -> Bool {
        guard hidden, let first = waiting.first else { return false }
        hidden = false
        browsing = false
        writingAfter = nil
        setCurrent(first.id)
        return true
    }

    /// Brings the card to this Ping, e.g. when the human opens its notification.
    public func show(_ pingID: String) {
        guard deck.contains(where: { $0.id == pingID }) else { return }
        hidden = false
        browsing = true
        writingAfter = nil
        setCurrent(pingID)
    }

    /// Waiting Pings, and those whose Pong came back undelivered, in the Queue's order.
    public var waiting: [Ping] { deck.filter(\.needsHuman) }

    private func setCurrent(_ id: String?) {
        currentID = id
        if let id { shown.insert(id) }
    }

    private func refresh() async throws {
        guard let client else { return }
        apply(try await client.allPings())
    }

    /// Takes every Ping the feed reports, in its order. When the current Ping stops
    /// waiting and the human did not go to it with the arrows, the card moves on to the next
    /// waiting Ping, or the first one. A card that was hidden opens on a waiting Ping.
    /// When the card's Ping is answered, said more are coming and is still its Session's
    /// latest, the card stays on it, saying the next question is being written, until the
    /// Session's next Ping arrives.
    package func apply(_ all: [Ping]) {
        let wasShown = hasWaiting || isWriting
        self.all = all
        let queue = all.filter { $0.needsHuman || shown.contains($0.id) }
        // Pings already dealt with come first, so the count never reads as if Pings were
        // still waiting after the last one that is.
        deck = queue.filter { !$0.needsHuman } + queue.filter(\.needsHuman)
        // An undelivered Pong comes back like a new Ping, under its own key.
        let key = { (p: Ping) in p.isWaiting ? p.id : "\(p.id) \(p.pong?.delivery ?? "")" }
        let fresh = deck.first { $0.needsHuman && !seenWaiting.contains(key($0)) }
        seenWaiting.formUnion(deck.filter(\.needsHuman).map(key))
        if hidden {
            // Only a Ping that arrived while hidden brings the card back, on that Ping.
            guard let fresh else { return }
            hidden = false
            browsing = false
            writingAfter = nil
            setCurrent(fresh.id)
            return
        }
        if writingAfter == nil, let c = current, c.state == .answered, c.moreComing == true,
           latest(of: c.session)?.id == c.id, !browsing {
            writingAfter = c
        }
        if let w = writingAfter {
            if let next = deck.first(where: { $0.isWaiting && $0.session == w.session && ($0.createdAt ?? "") > (w.createdAt ?? "") }) {
                writingAfter = nil
                setCurrent(next.id)
                return
            }
            if latest(of: w.session)?.moreComing == true, deck.contains(where: { $0.id == w.id }) { return }
            writingAfter = nil
        }
        if let c = current, c.needsHuman || (browsing && wasShown) { return }
        // The next one after it in the Queue's order.
        let after = queue.firstIndex { $0.id == currentID }.map { $0 + 1 } ?? 0
        let next = queue[after...].first(where: \.needsHuman) ?? queue.first(where: \.needsHuman)
        setCurrent(next?.id)
    }
}
