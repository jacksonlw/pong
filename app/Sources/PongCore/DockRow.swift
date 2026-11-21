import CoreGraphics

/// The panels Pong opens beside the card's window: Threads, Settings and Resource previews. The
/// docked ones sit in one row, nearest the card first; a detached one stands where the human put
/// it. Each panel is known by a key its owner chooses.
public struct DockRow: Equatable, Sendable {
    /// The docked panels, nearest the card first.
    public private(set) var docked: [String] = []
    /// The panels the human moved away from the row.
    public private(set) var detached: Set<String> = []

    public init() {}

    public func contains(_ key: String) -> Bool { docked.contains(key) || detached.contains(key) }

    /// Opens a panel: a new one goes in nearest the card, or stands apart when `apart`; one
    /// already open keeps its place. The docked panels `replacing` picks are taken out first, and
    /// returned so their owners can close them.
    @discardableResult
    public mutating func open(_ key: String, apart: Bool = false, replacing: (String) -> Bool = { _ in false }) -> [String] {
        let gone = docked.filter { $0 != key && replacing($0) }
        docked.removeAll { gone.contains($0) }
        guard !contains(key) else { return gone }
        if apart { detached.insert(key) } else { docked.insert(key, at: 0) }
        return gone
    }

    public mutating func close(_ key: String) {
        docked.removeAll { $0 == key }
        detached.remove(key)
    }

    /// The human moved the panel away from the row.
    public mutating func detach(_ key: String) {
        guard docked.contains(key) else { return }
        docked.removeAll { $0 == key }
        detached.insert(key)
    }

    /// The human put the panel back, at the far end of the row.
    public mutating func dockAtEnd(_ key: String) {
        guard contains(key) else { return }
        detached.remove(key)
        docked.removeAll { $0 == key }
        docked.append(key)
    }

    /// Where the docked panels of these sizes sit, nearest the card first: in a row away from the
    /// card, to its left, or to its right when the left has no room for the first; top edges
    /// level with the card's, and kept on the screen. Past the screen's edge, later panels lie
    /// over earlier ones.
    public static func frames(_ sizes: [CGSize], beside card: CGRect, on screen: CGRect, gap: CGFloat) -> [CGRect] {
        let left = card.minX - gap - (sizes.first?.width ?? 0) >= screen.minX
        var edge = left ? card.minX - gap : card.maxX + gap
        return sizes.map { s in
            var x = left ? edge - s.width : edge
            edge = left ? x - gap : x + s.width + gap
            x = max(screen.minX, min(x, screen.maxX - s.width))
            let y = max(screen.minY, card.maxY - s.height)
            return CGRect(origin: CGPoint(x: x, y: y), size: s)
        }
    }
}
