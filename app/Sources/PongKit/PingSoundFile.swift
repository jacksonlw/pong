import Foundation

extension PingSound {
    /// The sound's file in PongKit's resources.
    public var url: URL? { Bundle.module.url(forResource: rawValue, withExtension: "wav") }
}
