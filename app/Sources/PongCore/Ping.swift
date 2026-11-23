import Foundation

/// A Ping as the Engine reports it. Only the parts the app shows so far.
public struct Ping: Decodable, Identifiable, Equatable, Sendable {
    /// The human's answer, once sent.
    public struct Pong: Codable, Equatable, Sendable {
        public var answer: String?
        public var answers: [String]?
        public var text: String?
        /// The Pong should start a new Session, continued from this one.
        public var newSession: Bool?
        /// Where the Pong stands on its way to the Harness: sent,
        /// delivering, delivered, undelivered, unconfirmed or left. Absent on a Pong recorded
        /// before delivery was tracked, which counts as delivered. Read only: never sent.
        public var delivery: String?
        /// Why an undelivered or unconfirmed Pong is back with the human.
        public var notDelivered: String?

        /// Where the Pong stands, in words, or nil for a Pong recorded before delivery was tracked.
        public var deliveryLine: String? {
            switch delivery {
            case "sent": "Sent, not yet delivered"
            case "delivering": "Sent, being delivered"
            case "delivered": "Delivered"
            case "undelivered": "Not delivered"
            case "unconfirmed": "Sent, but it may not have arrived"
            case "left": "Not delivered, and not sent again"
            default: nil
            }
        }

        /// The Pong could not be delivered, or may not have been, and is back with the human.
        public var needsHuman: Bool { delivery == "undelivered" || delivery == "unconfirmed" }

        /// The Pong did not reach the Harness, or may not have: shown in the error colour.
        public var missedHarness: Bool { needsHuman || delivery == "left" }

        /// Files sent with it, for the Engine to give the Session as paths. Sent only.
        public var files: [PongFile]?

        public init(answer: String? = nil, answers: [String]? = nil, text: String? = nil, newSession: Bool? = nil,
                    files: [PongFile]? = nil) {
            self.answer = answer
            self.answers = answers
            self.text = text
            self.newSession = newSession
            self.files = files
        }

        private enum Out: String, CodingKey { case answer, answers, text, new_session, files }

        // Written in the API's snake case; read with the decoder's snake case conversion.
        public func encode(to encoder: Encoder) throws {
            var c = encoder.container(keyedBy: Out.self)
            try c.encodeIfPresent(answer, forKey: .answer)
            try c.encodeIfPresent(answers, forKey: .answers)
            try c.encodeIfPresent(text, forKey: .text)
            try c.encodeIfPresent(newSession, forKey: .new_session)
            if let files, !files.isEmpty { try c.encode(files, forKey: .files) }
        }
    }

    public struct Answer: Decodable, Equatable, Sendable {
        public var label: String
        public init(label: String) { self.label = label }
    }

    public init(id: String, session: String, folder: String? = nil, question: String, answers: [Answer]? = nil,
                allowSeveral: Bool? = nil, approves: String? = nil, isNote: Bool? = nil, blocksAgent: Bool? = nil,
                moreComing: Bool? = nil, first: Bool? = nil, createdAt: String? = nil, state: PingState,
                pong: Pong? = nil, resources: [Resource]? = nil, tags: [Tag]? = nil, computer: Computer? = nil,
                finished: Bool? = nil) {
        self.id = id; self.session = session; self.folder = folder; self.question = question
        self.answers = answers; self.allowSeveral = allowSeveral; self.approves = approves; self.isNote = isNote
        self.blocksAgent = blocksAgent; self.moreComing = moreComing; self.first = first; self.createdAt = createdAt
        self.state = state; self.pong = pong; self.resources = resources; self.tags = tags
        self.computer = computer
        self.finished = finished
    }

    public var id: String
    public var session: String
    public var folder: String?
    public var question: String
    /// In the Engine's order: the recommended answer is first, nothing else marks it.
    public var answers: [Answer]?
    public var allowSeveral: Bool?
    /// "plan" or "action" when the Ping asks for approval.
    public var approves: String?
    public var isNote: Bool?
    /// Text the human wrote in the Thread while no Ping waited: it has no question, only its Pong.
    public var unasked: Bool?
    /// One of Pong's own lines in the Thread, such as "You stopped this turn from …": the words
    /// are `question`, and it has no answers and never waits.
    public var line: Bool?
    /// A Thought: one sentence the agent wrote as it worked, stating something it found, decided
    /// or changed. Kept like one of Pong's own lines: the words are `question`, it has
    /// no answers, never waits and never notifies.
    public var thought: Bool?
    /// The agent cannot continue until the Pong arrives; such a Pong cannot start a new Session.
    public var blocksAgent: Bool?
    /// The agent hands over finished work: the apps draw the Finished mark on it.
    /// Absent counts as false; Pong never decides on its own that a Ping is finished.
    public var finished: Bool?
    /// The agent said another Ping from this Session will follow.
    public var moreComing: Bool?
    /// The Engine found it its Session's first Ping, for the Ping sound.
    public var first: Bool?
    public var createdAt: String?
    public var state: PingState
    public var pong: Pong?

    public var isWaiting: Bool { state == .waiting }
    /// The Ping carries the Finished mark.
    public var isFinished: Bool { finished == true }
    /// The Ping is a Thought, shown only in its Session's Thread.
    public var isThought: Bool { thought == true }
    /// The Ping waits, or its Pong came back undelivered: either way it is the human's to act on.
    public var needsHuman: Bool { isWaiting || pong?.needsHuman == true }
    public var resources: [Resource]?
    /// In the order the agent sent them; absent when there are none.
    public var tags: [Tag]?
    /// The computer holding the Ping, named once the set has more than one.
    public var computer: Computer?
    /// Another computer's Ping: handing back and the Harness happen only there.
    public var remote: Bool?
    /// The Pong being written on any Device.
    public var draft: Draft?

    public struct Computer: Decodable, Equatable, Sendable {
        public var id: String
        public var name: String
        public var reachable: Bool
        public init(id: String, name: String, reachable: Bool) { self.id = id; self.name = name; self.reachable = reachable }
    }

    public struct Draft: Codable, Equatable, Sendable {
        public var text: String?
        public init(text: String?) { self.text = text }
    }

    /// A Pong sent from this Device that arrived after another one won, and why.
    public var lost: Lost?

    public struct Lost: Decodable, Equatable, Sendable {
        public var reason: String
    }

    public var isRemote: Bool { remote == true }
}

/// Short text with an icon, and optionally a colour, that says what kind of Ping it is, where
/// its change lands or what it concerns. It is only read, never clicked.
/// The icon and colour are the agent's names as given, which may be ones the app does not know.
public struct Tag: Decodable, Equatable, Hashable, Sendable {
    public var text: String
    public var icon: String?
    public var colour: String?

    public init(text: String, icon: String? = nil, colour: String? = nil) {
        self.text = text
        self.icon = icon
        self.colour = colour
    }
}

/// Where a Ping stands, as the Engine names it. A state this app does not know decodes as
/// `.other` rather than failing, so a newer Engine does not break the card.
public enum PingState: Decodable, Equatable, Sendable, ExpressibleByStringLiteral {
    case waiting, answered, handedBack, answeredInHarness, withdrawn, dismissed
    case other(String)

    public init(_ raw: String) {
        switch raw {
        case "waiting": self = .waiting
        case "answered": self = .answered
        case "handed_back": self = .handedBack
        case "answered_in_harness": self = .answeredInHarness
        case "withdrawn": self = .withdrawn
        case "dismissed": self = .dismissed
        default: self = .other(raw)
        }
    }

    /// The Engine's name for the state.
    public var name: String {
        switch self {
        case .waiting: "waiting"
        case .answered: "answered"
        case .handedBack: "handed_back"
        case .answeredInHarness: "answered_in_harness"
        case .withdrawn: "withdrawn"
        case .dismissed: "dismissed"
        case .other(let raw): raw
        }
    }

    public init(stringLiteral raw: String) { self.init(raw) }
    public init(from decoder: Decoder) throws { self.init(try decoder.singleValueContainer().decode(String.self)) }
}

/// One event from the Engine's event stream. `ping` is set for the `ping.*` kinds.
public struct EngineEvent: Sendable {
    public var seq: Int
    public var kind: String
    public var ping: Ping?

    public init(seq: Int, kind: String, ping: Ping? = nil) {
        self.seq = seq; self.kind = kind; self.ping = ping
    }
}

public enum JSON {
    public static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        return d
    }()
}

/// Reads Server-Sent Events line by line. The Engine puts the whole envelope on one
/// `data:` line, so each such line is one event; `id:` and `event:` repeat what it holds.
public enum EventLine {
    private struct Envelope: Decodable {
        var seq: Int
        var kind: String
    }
    private struct PingEnvelope: Decodable {
        var data: Ping
    }

    /// The event on this line, or nil for lines that carry none.
    public static func parse(_ line: String) -> EngineEvent? {
        guard line.hasPrefix("data:") else { return nil }
        let body = Data(line.dropFirst(5).drop(while: { $0 == " " }).utf8)
        guard let env = try? JSON.decoder.decode(Envelope.self, from: body) else { return nil }
        var ping: Ping?
        if env.kind.hasPrefix("ping.") {
            ping = try? JSON.decoder.decode(PingEnvelope.self, from: body).data
        }
        return EngineEvent(seq: env.seq, kind: env.kind, ping: ping)
    }

    /// Every event in a recorded stream, in order.
    public static func parseAll(_ text: String) -> [EngineEvent] {
        text.split(whereSeparator: \.isNewline).compactMap { parse(String($0)) }
    }
}
