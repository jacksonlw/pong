import Foundation
import PongCore

/// What a build of the Mac app adds beyond Local. Public pong adds nothing: no account and no
/// hosted voice model, so Settings does not offer one.
public struct PongExtras: Sendable {
    /// The account behind the hosted voice model.
    public var account: (any PongAccount)?
    /// Makes the hosted voice model from the account's session token.
    public var hostedVoice: (@Sendable (String) -> any VoiceModel)?
    /// What Settings says about the hosted voice model, and when its account may not use it.
    public var hostedVoiceAbout: String
    public var hostedVoiceUpgrade: String

    public init(account: (any PongAccount)? = nil, hostedVoice: (@Sendable (String) -> any VoiceModel)? = nil,
                hostedVoiceAbout: String = "", hostedVoiceUpgrade: String = "") {
        self.account = account
        self.hostedVoice = hostedVoice
        self.hostedVoiceAbout = hostedVoiceAbout
        self.hostedVoiceUpgrade = hostedVoiceUpgrade
    }
}

public enum Extras {
    /// What Settings says when the account may not use the hosted voice model.
    public static var hostedVoiceUpgrade: String { current.hostedVoiceUpgrade }

    /// The build's extras, set once by runPong before the app starts.
    nonisolated(unsafe) public static var current = PongExtras()
}
