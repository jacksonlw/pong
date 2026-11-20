import Foundation

/// What the human meant by what they said about the card's Ping.
public enum VoiceIntent: Equatable, Sendable {
    /// Chosen answers (labels, in the Ping's order), typed text, or both: the shapes a Pong takes.
    case answer(choices: [String], text: String?)
    case explain
    case showContext
    case hideContext
    /// Open the Resource the human named, by title or type.
    case showResource(String)
    case wait, go, cancel, repeatPing, stop, dismiss
    case notUnderstood
}

/// The only parts of a Ping a model is given, with the transcript.
public struct VoicePing: Codable, Equatable, Sendable {
    public var question: String
    public var answers: [String]
    /// The recommended answer: the first, when there is a choice (the Engine puts it first).
    public var recommended: String?
    public var allowSeveral: Bool
    public var quickContext: String?
    public var resources: [String]

    public init(_ ping: Ping, quickContext: String? = nil) {
        question = ping.question
        answers = (ping.answers ?? []).map(\.label)
        let choosable = answers.filter { $0 != "Nothing more" }
        recommended = choosable.count > 1 ? choosable.first : nil
        allowSeveral = ping.allowSeveral == true
        self.quickContext = quickContext
        resources = (ping.resources ?? []).map(\.title)
    }

    private enum CodingKeys: String, CodingKey {
        case question, answers, recommended, allowSeveral = "allow_several", quickContext = "quick_context", resources
    }
}

/// A language model that understands speech: given instructions, a request and the JSON Schema
/// its reply must follow, it returns the reply's text. Endpoint and Anthropic implementations
/// are in VoiceModels.swift; tests use a fake.
public protocol VoiceModel: Sendable {
    func reply(system: String, user: String, schema: String) async throws -> String
}

extension VoiceUnderstanding {
    /// What the human meant: simple phrases are matched here, anything else goes to `model`.
    /// Nil `model` (none chosen yet) leaves anything else not understood. Throws only when the
    /// model cannot be reached or refuses; a reply that does not fit the structure is not understood.
    /// `pending` is true while an understood answer fills in or counts down, when "no" cancels it.
    public static func understand(_ words: String, _ ping: VoicePing, pending: Bool = false,
                                  model: VoiceModel?) async throws -> VoiceIntent {
        if let i = local(words, ping, pending: pending) { return i }
        guard let model else { return .notUnderstood }
        let reply = try await model.reply(system: system, user: request(words, ping), schema: schema)
        return parse(reply, ping)
    }

    static let ordinals = ["first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "1st": 1, "2nd": 2, "3rd": 3, "4th": 4, "5th": 5]
    static let cardinals = ["one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "1": 1, "2": 2, "3": 3, "4": 4, "5": 5]
    static let filler: Set<String> = ["the", "one", "ones", "number", "option", "answer", "and", "please"]

    static let phrases: [String: VoiceIntent] = {
        var p: [String: VoiceIntent] = [:]
        for s in ["stop", "stop listening"] { p[s] = .stop }
        for s in ["wait", "hold on", "hang on", "pause", "wait a moment", "wait a second"] { p[s] = .wait }
        for s in ["go", "send", "send it", "go ahead", "ok go", "okay go"] { p[s] = .go }
        for s in ["cancel", "cancel that", "no wait"] { p[s] = .cancel }
        for s in ["repeat", "repeat that", "say that again", "again", "read it again"] { p[s] = .repeatPing }
        for s in ["dismiss", "dismiss it"] { p[s] = .dismiss }
        for s in ["explain", "explain this", "explain it", "what is this about", "what's this about", "what does this mean"] { p[s] = .explain }
        for s in ["show the context", "show me the context", "show context", "open the context"] { p[s] = .showContext }
        for s in ["hide it", "hide the context", "hide context", "close the context", "close it"] { p[s] = .hideContext }
        return p
    }()

    /// The phrases matched without a model: ordinals and numbers ("the second one", "number
    /// three", "two and three"), "recommended", "yes" and "no" on a two-answer Ping, wait, go,
    /// stop, cancel, repeat, dismiss, explain, and showing or hiding the context or a Resource.
    public static func local(_ words: String, _ ping: VoicePing, pending: Bool = false) -> VoiceIntent? {
        let w = normalized(words)
        if let p = phrases[w] { return p }
        let choosable = ping.answers.filter { $0 != "Nothing more" }
        if w == "no" {
            return pending || choosable.count != 2 ? .cancel : .answer(choices: [choosable[1]], text: nil)
        }
        if w == "yes", choosable.count == 2 { return .answer(choices: [choosable[0]], text: nil) }
        if ["recommended", "the recommended one", "the recommended", "recommended one"].contains(w), let r = ping.recommended {
            return .answer(choices: [r], text: nil)
        }
        for prefix in ["show me the ", "show the ", "open the ", "show ", "open "] where w.hasPrefix(prefix) {
            return .showResource(String(w.dropFirst(prefix.count)))
        }
        return numbered(w, ping)
    }

    private static func numbered(_ w: String, _ ping: VoicePing) -> VoiceIntent? {
        let tokens = w.split(separator: " ").map(String.init)
        let hasOrdinal = tokens.contains { ordinals[$0] != nil }
        var picked: [Int] = []
        for t in tokens {
            if let n = ordinals[t] ?? (hasOrdinal && t == "one" ? nil : cardinals[t]) {
                if !picked.contains(n) { picked.append(n) }
            } else if !filler.contains(t) {
                return nil
            }
        }
        guard !picked.isEmpty, picked.allSatisfy({ $0 <= ping.answers.count }) else { return nil }
        return .answer(choices: picked.sorted().map { ping.answers[$0 - 1] }, text: nil)
    }

    // MARK: The model

    static let system = """
    You turn what a developer said aloud into their answer to one question from their coding agent. \
    You are given the transcript and the question: its answers, which one is recommended, whether several \
    may be chosen, its quick context and the titles of its resources. Reply with JSON only, matching the schema:
    - "answer" with "choices" (exact answer labels they picked, possibly none) and/or "text" (anything they want \
    to say to the agent in their own words, written as they would type it);
    - "explain" when they ask what the question is about;
    - "show_context" or "hide_context" for the context panel;
    - "show_resource" with "resource" set to the exact title of the resource they named;
    - "wait", "go", "cancel", "repeat" or "stop" for those requests;
    - "not_understood" when unsure. Never guess a choice.
    """

    static let schema = """
    {"type":"object","properties":{"intent":{"type":"string","enum":["answer","explain","show_context","hide_context",\
    "show_resource","wait","go","cancel","repeat","stop","not_understood"]},"choices":{"type":"array","items":{"type":"string"}},\
    "text":{"type":"string"},"resource":{"type":"string"}},"required":["intent"],"additionalProperties":false}
    """

    static func request(_ words: String, _ ping: VoicePing) -> String {
        struct Request: Encodable { var transcript: String; var ping: VoicePing }
        let e = JSONEncoder()
        e.outputFormatting = [.sortedKeys]
        return String(decoding: (try? e.encode(Request(transcript: words, ping: ping))) ?? Data(), as: UTF8.self)
    }

    /// The model's reply as an intent; anything that does not match the structure, or names an
    /// answer or Resource the Ping does not have, is not understood.
    public static func parse(_ reply: String, _ ping: VoicePing) -> VoiceIntent {
        struct Reply: Decodable {
            var intent: String
            var choices: [String]?
            var text: String?
            var resource: String?
        }
        guard let r = try? JSONDecoder().decode(Reply.self, from: Data(jsonText(reply).utf8)) else { return .notUnderstood }
        switch r.intent {
        case "answer":
            let choices = r.choices ?? []
            let text = r.text?.trimmingCharacters(in: .whitespacesAndNewlines)
            guard choices.allSatisfy(ping.answers.contains) else { return .notUnderstood }
            let t = text?.isEmpty == false ? text : nil
            if choices.isEmpty && t == nil { return .notUnderstood }
            return .answer(choices: ping.answers.filter(choices.contains), text: t)
        case "explain": return .explain
        case "show_context": return .showContext
        case "hide_context": return .hideContext
        case "show_resource":
            guard let res = r.resource, ping.resources.contains(res) else { return .notUnderstood }
            return .showResource(res)
        case "wait": return .wait
        case "go": return .go
        case "cancel": return .cancel
        case "repeat": return .repeatPing
        case "stop": return .stop
        default: return .notUnderstood
        }
    }

    // MARK: Explain and show

    static let explainSystem = """
    Explain to a developer, in two or three short spoken sentences, what their coding agent is asking and \
    what the answers mean. Use only the question, its answers and its quick context; add nothing else. \
    Reply with JSON only: {"explanation": "..."}.
    """

    static let explainSchema = """
    {"type":"object","properties":{"explanation":{"type":"string"}},"required":["explanation"],"additionalProperties":false}
    """

    /// Two or three sentences drawn only from the Ping's question, answers and quick context:
    /// the model's, or without one (or with a reply that does not fit) the quick context itself.
    public static func explain(_ ping: VoicePing, model: VoiceModel?) async throws -> String {
        if let model {
            struct Reply: Decodable { var explanation: String }
            let reply = try await model.reply(system: explainSystem, user: request("explain", ping), schema: explainSchema)
            if let r = try? JSONDecoder().decode(Reply.self, from: Data(jsonText(reply).utf8)), !r.explanation.isEmpty {
                return r.explanation
            }
        }
        guard let c = ping.quickContext, !c.isEmpty else { return "The agent asks: \(ping.question)" }
        return firstSentences(c, 3)
    }

    static func firstSentences(_ s: String, _ n: Int) -> String {
        var out = "", count = 0
        for ch in s {
            out.append(ch)
            if ".!?".contains(ch) { count += 1; if count == n { break } }
        }
        return out.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Resource kinds as people name them.
    static let kindNames: [String: String] = [
        "diff": "change", "change": "change", "changes": "change", "code change": "change",
        "screenshot": "image", "image": "image", "picture": "image",
        "error": "error", "output": "output", "log": "output", "logs": "output",
        "document": "document", "doc": "document", "file": "file", "slides": "slides", "text": "text",
    ]

    /// The Resources a spoken name fits: by title, or else by kind ("the diff", "the screenshot").
    public static func resources(named name: String, in all: [Resource]) -> [Resource] {
        let n = normalized(name)
        let byTitle = all.filter { normalized($0.title) == n }
        if !byTitle.isEmpty { return byTitle }
        let partial = all.filter { normalized($0.title).contains(n) }
        if !partial.isEmpty { return partial }
        guard let kind = kindNames[n] else { return [] }
        return all.filter { $0.kind == kind }
    }

    /// The JSON object in a reply, without a code fence a model may have put round it.
    static func jsonText(_ s: String) -> String {
        guard let a = s.firstIndex(of: "{"), let b = s.lastIndex(of: "}"), a < b else { return s }
        return String(s[a...b])
    }
}
