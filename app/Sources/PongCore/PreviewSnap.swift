import CoreGraphics

/// Where a preview the human dragged comes to rest: beside or level with the nearest of Pong's
/// other windows when it was let go close to one, and where it was let go otherwise.
public enum PreviewSnap {
    /// How close an edge must be to another window's to be pulled to it.
    public static let reach: CGFloat = 20

    public static func rest(_ f: CGRect, beside others: [CGRect], gap: CGFloat) -> CGRect {
        var dx: CGFloat?, dy: CGFloat?
        func nearer(_ d: CGFloat, than best: CGFloat?) -> CGFloat? {
            abs(d) <= reach && abs(d) < abs(best ?? .infinity) ? d : best
        }
        for o in others where f.insetBy(dx: -reach - gap, dy: -reach - gap).intersects(o) {
            // Beside it with the gap between, or with the same edges.
            for d in [o.maxX + gap - f.minX, o.minX - gap - f.maxX, o.minX - f.minX, o.maxX - f.maxX] { dx = nearer(d, than: dx) }
            for d in [o.maxY + gap - f.minY, o.minY - gap - f.maxY, o.minY - f.minY, o.maxY - f.maxY] { dy = nearer(d, than: dy) }
        }
        return f.offsetBy(dx: dx ?? 0, dy: dy ?? 0)
    }
}
