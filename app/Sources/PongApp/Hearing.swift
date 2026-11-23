import PongKit

/// The Mac's one microphone and speech recogniser, shared by Voice mode and Speak so only one
/// of them listens at a time.
@MainActor
enum Hearing {
    static let io = SystemVoiceIO()
    static let speak = Speak(io: io, permissions: SystemVoicePermissions())
}
