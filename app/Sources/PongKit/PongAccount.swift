import Foundation

/// An account a build's hosted voice model belongs to: its session token, whether it may use
/// the model, and the page to subscribe on.
public protocol PongAccount: Sendable {
    func token() async -> String?
    /// Whether the Account has Pong Cloud; false when signed out or the server can't be reached.
    func entitled() async -> Bool
    @MainActor func openUpgrade()
}
