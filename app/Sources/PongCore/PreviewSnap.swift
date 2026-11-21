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

    /// Whether two windows rest against each other: their facing edges no more than the gap
    /// apart (and a little), and overlapping along those edges.
    public static func touching(_ a: CGRect, _ b: CGRect, gap: CGFloat) -> Bool {
        let slack = gap + 2
        let alongX = a.minX < b.maxX && b.minX < a.maxX
        let alongY = a.minY < b.maxY && b.minY < a.maxY
        let besideX = abs(a.minX - b.maxX) <= slack || abs(b.minX - a.maxX) <= slack
        let besideY = abs(a.minY - b.maxY) <= slack || abs(b.minY - a.maxY) <= slack
        return (alongY && (besideX || a.intersects(b))) || (alongX && (besideY || a.intersects(b)))
    }

    /// The windows among `loose` that rest against the `body`, directly or through each other.
    public static func attached(_ loose: [String: CGRect], to body: [CGRect], gap: CGFloat) -> Set<String> {
        var joined = Set<String>(), reached = body, grew = true
        while grew {
            grew = false
            for (k, f) in loose where !joined.contains(k) && reached.contains(where: { touching(f, $0, gap: gap) }) {
                joined.insert(k)
                reached.append(f)
                grew = true
            }
        }
        return joined
    }
}
