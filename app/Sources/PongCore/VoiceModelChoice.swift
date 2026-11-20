import Foundation

/// The model voice understands speech with, as the Engine keeps it.
public enum VoiceModelChoice: String, Codable, CaseIterable, Sendable {
    /// None yet: only the phrases matched on the Mac work.
    case none
    /// Pong's own model through Pong's server, for Pong Cloud subscribers.
    case hosted
    /// An OpenAI-compatible chat completions endpoint, such as a local Ollama or LM Studio.
    case endpoint
    /// Anthropic's API with the human's own key, kept in the Keychain by the Engine.
    case anthropic

    public var title: String {
        switch self {
        case .none: "None"
        case .hosted: "Hosted"
        case .endpoint: "Endpoint"
        case .anthropic: "Anthropic API key"
        }
    }
}
