import Foundation


/// Why a model could not answer, in words Settings and the card can show.
public struct VoiceModelError: LocalizedError, Equatable, Sendable {
    public var errorDescription: String?
    public init(_ d: String) { errorDescription = d }
}

/// An OpenAI-compatible chat completions endpoint, asked for a JSON object.
public struct EndpointVoiceModel: VoiceModel {
    public var url: URL
    public var model: String

    public init(url: URL, model: String) {
        self.url = url
        self.model = model
    }

    /// `<url>/chat/completions`, unless the URL already names it.
    var completions: URL {
        url.path.hasSuffix("/chat/completions") ? url : url.appendingPathComponent("chat/completions")
    }

    func request(system: String, user: String) throws -> URLRequest {
        var r = URLRequest(url: completions)
        r.httpMethod = "POST"
        r.timeoutInterval = 20
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let body: [String: Any] = [
            "model": model,
            "messages": [["role": "system", "content": system], ["role": "user", "content": user]],
            "response_format": ["type": "json_object"],
        ]
        r.httpBody = try JSONSerialization.data(withJSONObject: body)
        return r
    }

    static func text(_ data: Data, status: Int) throws -> String {
        struct Reply: Decodable {
            struct Choice: Decodable { struct Message: Decodable { var content: String? }; var message: Message }
            var choices: [Choice]?
            struct Failure: Decodable { var message: String }
            var error: Failure?
        }
        let r = try? JSONDecoder().decode(Reply.self, from: data)
        guard (200..<300).contains(status) else {
            throw VoiceModelError("the endpoint answered \(status)\(r?.error.map { ": \($0.message)" } ?? "")")
        }
        guard let content = r?.choices?.first?.message.content else { throw VoiceModelError("the endpoint's reply had no message") }
        return content
    }

    public func reply(system: String, user: String, schema: String) async throws -> String {
        let (data, status) = try await VoiceModels.send(request(system: system, user: user), to: url.host ?? url.absoluteString)
        return try Self.text(data, status: status)
    }
}

/// Anthropic's Messages API with the human's key, constrained to the reply's JSON Schema.
public struct AnthropicVoiceModel: VoiceModel {
    public var key: String
    /// A small, fast model.
    public var model = "claude-haiku-5-5"
    static let url = URL(string: "https://api.anthropic.com/v1/messages")!

    public init(key: String) { self.key = key }

    func request(system: String, user: String, schema: String) throws -> URLRequest {
        var r = URLRequest(url: Self.url)
        r.httpMethod = "POST"
        r.timeoutInterval = 20
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        r.setValue(key, forHTTPHeaderField: "x-api-key")
        r.setValue("2026-10-21", forHTTPHeaderField: "anthropic-version")
        let body: [String: Any] = [
            "model": model,
            "max_tokens": 1024,
            "system": system,
            "messages": [["role": "user", "content": user]],
            "output_config": ["effort": "low",
                              "format": ["type": "json_schema", "schema": try JSONSerialization.jsonObject(with: Data(schema.utf8))]],
        ]
        r.httpBody = try JSONSerialization.data(withJSONObject: body)
        return r
    }

    static func text(_ data: Data, status: Int) throws -> String {
        struct Reply: Decodable {
            struct Block: Decodable { var type: String; var text: String? }
            var content: [Block]?
            var stopReason: String?
            struct Failure: Decodable { var type: String; var message: String }
            var error: Failure?
            enum CodingKeys: String, CodingKey { case content, stopReason = "stop_reason", error }
        }
        let r = try? JSONDecoder().decode(Reply.self, from: data)
        guard (200..<300).contains(status) else {
            if status == 401 { throw VoiceModelError("Anthropic refused the API key") }
            throw VoiceModelError("Anthropic answered \(status)\(r?.error.map { ": \($0.message)" } ?? "")")
        }
        if r?.stopReason == "refusal" { throw VoiceModelError("Anthropic declined the request") }
        guard let text = r?.content?.first(where: { $0.type == "text" })?.text else {
            throw VoiceModelError("Anthropic's reply had no text")
        }
        return text
    }

    public func reply(system: String, user: String, schema: String) async throws -> String {
        let (data, status) = try await VoiceModels.send(request(system: system, user: user, schema: schema), to: "api.anthropic.com")
        return try Self.text(data, status: status)
    }
}

/// The hosted voice model refused because its account may no longer use it.
public struct HostedVoiceLapsed: LocalizedError, Equatable, Sendable {
    public init() {}
    public var errorDescription: String? { HostedVoiceLapsed.said }
    /// Said once when hosted voice stops working; voice carries on with the phrases matched on the Mac.
    /// A build with a hosted voice model sets what is said.
    nonisolated(unsafe) public static var said = "Hosted voice stopped working. Until it works again, say a number, like \"the first one\"."
}

public enum VoiceModels {
    /// The model the settings choose, or nil when none is set up yet. `key` is the Anthropic key,
    /// or for hosted the account's session token.
    public static func make(_ choice: VoiceModelChoice, endpoint: String?, endpointModel: String?, key: String?) -> VoiceModel? {
        switch choice {
        case .none: return nil
        case .hosted:
            guard let key, !key.isEmpty, let make = Extras.current.hostedVoice else { return nil }
            return make(key)
        case .endpoint:
            guard let e = endpoint, let url = URL(string: e), url.scheme?.hasPrefix("http") == true,
                  let m = endpointModel, !m.isEmpty else { return nil }
            return EndpointVoiceModel(url: url, model: m)
        case .anthropic:
            guard let key, !key.isEmpty else { return nil }
            return AnthropicVoiceModel(key: key)
        }
    }

    public static func send(_ r: URLRequest, to host: String) async throws -> (Data, Int) {
        do {
            let (data, resp) = try await URLSession(configuration: .ephemeral).data(for: r)
            return (data, (resp as? HTTPURLResponse)?.statusCode ?? 0)
        } catch {
            throw VoiceModelError("\(host) could not be reached (\(error.localizedDescription))")
        }
    }
}

/// The sample Ping Settings' "Test" sends.
public enum VoiceSample {
    public static let transcript = "let's do just the users table, and add a test for it"
    public static let ping: VoicePing = {
        var p = Ping(id: "sample", session: "sample", question: "Which tables should the migration touch?", state: "waiting")
        p.answers = [Ping.Answer(label: "Users and orders"), Ping.Answer(label: "Users only"), Ping.Answer(label: "Nothing more")]
        return VoicePing(p, quickContext: "Orders and users both have the old date column.")
    }()

    public static func describe(_ i: VoiceIntent) -> String {
        switch i {
        case .answer(let choices, let text):
            let parts = choices + (text.map { ["“\($0)”"] } ?? [])
            return "Works. It understood: \(parts.joined(separator: ", with "))."
        case .notUnderstood: return "The model answered, but not in the shape Pong needs."
        default: return "Works, though it heard a request rather than an answer."
        }
    }
}
