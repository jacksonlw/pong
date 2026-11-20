import Foundation

/// Where the card sits on the screen.
public enum CardCorner: String, Codable, CaseIterable, Sendable {
    case topRight = "top-right", topLeft = "top-left", bottomRight = "bottom-right", bottomLeft = "bottom-left"

    public var isTop: Bool { self == .topRight || self == .topLeft }
    public var isRight: Bool { self == .topRight || self == .bottomRight }

    public var title: String {
        switch self {
        case .topLeft: "Top left"
        case .topRight: "Top right"
        case .bottomLeft: "Bottom left"
        case .bottomRight: "Bottom right"
        }
    }
}
